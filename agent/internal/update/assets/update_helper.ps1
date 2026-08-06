# USBridge Windows self-update helper.
#
# Run detached by apply_windows.go right before it exits the old process
# (which is why this script, not the app itself, does the actual file
# swap: Windows won't let a running process overwrite its own loaded
# image/DLLs). Everything this script installs was already downloaded and
# SHA-256-verified against an Ed25519-signed manifest by the Go code that
# launched it — this script's only job is the mechanical "wait, copy,
# relaunch, clean up" dance.
param(
    [Parameter(Mandatory = $true)][int]$ParentPid,
    [Parameter(Mandatory = $true)][string]$StagingDir,
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [Parameter(Mandatory = $true)][string]$ExeName,
    [switch]$Elevated
)

$ErrorActionPreference = 'Stop'

function Wait-ForParentExit {
    try {
        Wait-Process -Id $ParentPid -Timeout 30 -ErrorAction SilentlyContinue
    } catch {
        # Already gone, or never existed — either way, proceed.
    }
    # Give the OS a moment to release file locks on the just-exited
    # process's DLLs/exe even after Wait-Process returns.
    Start-Sleep -Milliseconds 750
}

function Copy-Update {
    Copy-Item -Path (Join-Path $StagingDir '*') -Destination $InstallDir -Recurse -Force
}

Wait-ForParentExit

try {
    Copy-Update
} catch {
    if ($Elevated) {
        # Already retried elevated and it still failed — nothing more we
        # can do automatically.
        exit 1
    }
    # Most likely cause: InstallDir needs administrator rights (e.g. it's
    # under Program Files). Re-launch this same script elevated, which
    # triggers exactly one UAC prompt — an unavoidable OS security control,
    # not an extra confirmation step this updater is choosing to add.
    $selfArgs = @(
        '-NoProfile', '-ExecutionPolicy', 'Bypass', '-WindowStyle', 'Hidden', '-File', $PSCommandPath,
        '-ParentPid', $ParentPid, '-StagingDir', $StagingDir, '-InstallDir', $InstallDir, '-ExeName', $ExeName, '-Elevated'
    )
    try {
        Start-Process -FilePath 'powershell.exe' -ArgumentList $selfArgs -Verb RunAs -Wait
    } catch {
        # UAC prompt declined, or elevation otherwise failed — leave the
        # current (pre-update) install as-is rather than half-applying it.
        exit 1
    }
    Remove-Item -Path $StagingDir -Recurse -Force -ErrorAction SilentlyContinue
    exit 0
}

$newExe = Join-Path $InstallDir $ExeName
if (Test-Path $newExe) {
    Start-Process -FilePath $newExe
}

Remove-Item -Path $StagingDir -Recurse -Force -ErrorAction SilentlyContinue
