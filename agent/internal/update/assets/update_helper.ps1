# USBridge Windows self-update helper.
#
# Run detached by apply_windows.go right before it exits the old process
# (which is why this script, not the app itself, does the actual file
# swap: Windows won't let a running process overwrite its own loaded
# image/DLLs). Everything this script installs was already downloaded and
# SHA-256-verified against an Ed25519-signed manifest by the Go code that
# launched it - this script's only job is the mechanical "wait, copy,
# relaunch, clean up" dance.
#
# Every step is logged to %TEMP%\usbridge-update.log.
param(
    [Parameter(Mandatory = $true)][int]$ParentPid,
    [Parameter(Mandatory = $true)][string]$StagingDir,
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [Parameter(Mandatory = $true)][string]$ExeName,
    # Set on the elevated re-run: copy only and report through the exit
    # code; the non-elevated caller relaunches, so the app never ends up
    # running as administrator.
    [switch]$CopyOnly
)

$ErrorActionPreference = 'Stop'
$LogPath = Join-Path $env:TEMP 'usbridge-update.log'
$BackupDir = "$StagingDir.backup"

function Write-Log([string]$Message) {
    try {
        Add-Content -Path $LogPath -Value ("{0:u} [{1}] {2}" -f (Get-Date), $PID, $Message)
    } catch {}
}

function Wait-ForParentExit {
    try {
        Wait-Process -Id $ParentPid -Timeout 30 -ErrorAction SilentlyContinue
    } catch {
        # Already gone, or never existed - either way, proceed.
    }
    # Other copies of the same exe keep it locked too: the agent window of a
    # user session attached to this service closes itself for the update
    # (and reopens afterwards); give it a moment, then close what is left.
    $target = Join-Path $InstallDir $ExeName
    $deadline = (Get-Date).AddSeconds(20)
    while ((Get-Date) -lt $deadline) {
        $others = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Id -ne $PID -and $_.Path -eq $target })
        if ($others.Count -eq 0) { break }
        Start-Sleep -Milliseconds 500
    }
    $others = @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.Id -ne $PID -and $_.Path -eq $target })
    foreach ($p in $others) {
        Write-Log "stopping pid $($p.Id), it still runs $target"
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    }
    # Give the OS a moment to release file locks on the just-exited
    # process's DLLs/exe even after Wait-Process returns.
    Start-Sleep -Milliseconds 750
}

# Copy-Update overwrites InstallDir with the staged files, first saving
# every file it replaces to $BackupDir so a failure halfway (a file still
# locked, the disk full) can be rolled back instead of leaving a mix of
# two versions.
function Copy-Update {
    Remove-Item -Path $BackupDir -Recurse -Force -ErrorAction SilentlyContinue
    $root = (Resolve-Path $StagingDir).Path.TrimEnd('\')
    $files = Get-ChildItem -Path $root -Recurse -File -Force |
        Where-Object { $_.FullName -ne (Join-Path $root '.update_helper.ps1') }
    try {
        foreach ($f in $files) {
            $rel = $f.FullName.Substring($root.Length + 1)
            $dest = Join-Path $InstallDir $rel
            if (Test-Path -LiteralPath $dest) {
                $saved = Join-Path $BackupDir $rel
                New-Item -ItemType Directory -Path (Split-Path $saved) -Force | Out-Null
                Copy-Item -LiteralPath $dest -Destination $saved -Force
            }
            New-Item -ItemType Directory -Path (Split-Path $dest) -Force | Out-Null
            Copy-Item -LiteralPath $f.FullName -Destination $dest -Force
        }
    } catch {
        $err = $_
        Write-Log "copy failed, rolling back: $err"
        if (Test-Path $BackupDir) {
            Copy-Item -Path (Join-Path $BackupDir '*') -Destination $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
        }
        Remove-Item -Path $BackupDir -Recurse -Force -ErrorAction SilentlyContinue
        throw $err
    }
    Remove-Item -Path $BackupDir -Recurse -Force -ErrorAction SilentlyContinue
}

function Test-PermissionError($ErrorRecord) {
    $e = $ErrorRecord.Exception
    while ($e) {
        if ($e -is [System.UnauthorizedAccessException]) { return $true }
        $e = $e.InnerException
    }
    return $false
}

function Start-App {
    $newExe = Join-Path $InstallDir $ExeName
    # Installed by the setup .exe the agent runs as the USBridgeAgent service
    # (LocalSystem): bring the service back rather than starting a bare copy
    # in session 0, which would be invisible and not managed by the SCM.
    $service = Get-CimInstance Win32_Service -Filter "Name='USBridgeAgent'" -ErrorAction SilentlyContinue
    if ($service -and [Security.Principal.WindowsIdentity]::GetCurrent().IsSystem -and $service.PathName -like "*$newExe*") {
        Start-Service -Name 'USBridgeAgent'
    } elseif (Test-Path $newExe) {
        Start-Process -FilePath $newExe
    }
}

if ($CopyOnly) {
    try {
        Copy-Update
        exit 0
    } catch {
        Write-Log "elevated copy failed: $_"
        exit 1
    }
}

Wait-ForParentExit
Write-Log "installing update from $StagingDir into $InstallDir"

$updated = $false
try {
    Copy-Update
    $updated = $true
} catch {
    if (Test-PermissionError $_) {
        # InstallDir needs administrator rights (e.g. it's under Program
        # Files). Re-run just the copy elevated, which triggers exactly one
        # UAC prompt - an unavoidable OS security control, not an extra
        # confirmation step this updater is choosing to add.
        Write-Log "no write access, asking for elevation: $_"
        $selfArgs = @(
            '-NoProfile', '-ExecutionPolicy', 'Bypass', '-WindowStyle', 'Hidden', '-File', "`"$PSCommandPath`"",
            '-ParentPid', $ParentPid, '-StagingDir', "`"$StagingDir`"", '-InstallDir', "`"$InstallDir`"", '-ExeName', "`"$ExeName`"", '-CopyOnly'
        )
        try {
            $p = Start-Process -FilePath 'powershell.exe' -ArgumentList $selfArgs -Verb RunAs -Wait -PassThru
            $updated = ($p.ExitCode -eq 0)
            if (-not $updated) { Write-Log "elevated copy exited with $($p.ExitCode)" }
        } catch {
            Write-Log "elevation declined or failed: $_"
        }
    } else {
        Write-Log "update failed: $_"
    }
}

# Relaunch either way: on failure the previous version is still (or again)
# in place, and the user must not be left with the app simply gone.
if ($updated) { Write-Log "update installed" } else { Write-Log "update not installed, restarting the current version" }
try {
    Start-App
} catch {
    Write-Log "relaunch failed: $_"
}

Remove-Item -Path $StagingDir -Recurse -Force -ErrorAction SilentlyContinue
if (-not $updated) { exit 1 }
