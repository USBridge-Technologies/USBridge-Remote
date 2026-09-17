#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Builds usbridge_agent for Windows, driving MSYS2 UCRT64 entirely from PowerShell.

.DESCRIPTION
    build_windows.sh (the actual build logic: Go build, DLL bundling, Sunshine
    staging) still runs inside the UCRT64 toolchain -- Go's cgo (needed for
    Fyne/OpenGL) requires a GCC-compatible compiler, which on Windows means mingw-w64.
    This script just removes the manual steps around that: no more opening an "MSYS2
    UCRT64" terminal by hand and running pacman yourself. It installs MSYS2 itself (via
    winget) if missing, installs whichever required UCRT64 packages aren't already
    present, then invokes build_windows.sh non-interactively inside UCRT64 and streams
    its output back here.

.PARAMETER Msys2Root
    Path to the MSYS2 install. Defaults to $env:MSYS2_ROOT or C:\msys64.

.EXAMPLE
    ./agent/scripts/build_windows.ps1
#>
param(
    [string]$Msys2Root = $(if ($env:MSYS2_ROOT) { $env:MSYS2_ROOT } else { "C:\msys64" })
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor Cyan
}

# Native stderr becomes ErrorRecords under $ErrorActionPreference=Stop and aborts
# the script. MSYS2 often emits harmless noise (e.g. cygpath blocked by Smart App
# Control), so wrap bash/msys2_shell and only fail on non-zero exit codes.
function Invoke-Native {
    param(
        [Parameter(Mandatory)][scriptblock]$ScriptBlock,
        [switch]$CaptureStdout
    )
    $prev = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    try {
        $lines = & $ScriptBlock 2>&1
        $code = $LASTEXITCODE
        $stdout = New-Object System.Collections.Generic.List[string]
        foreach ($line in $lines) {
            if ($line -is [System.Management.Automation.ErrorRecord]) {
                Write-Host $line.ToString() -ForegroundColor DarkYellow
            } elseif ($CaptureStdout) {
                [void]$stdout.Add([string]$line)
            } else {
                Write-Host $line
            }
        }
        return [pscustomobject]@{
            ExitCode = $code
            Stdout   = $stdout.ToArray()
        }
    } finally {
        $ErrorActionPreference = $prev
    }
}

function Restore-MsysCaBundle {
    $bundle = Join-Path $Msys2Root "usr\ssl\certs\ca-bundle.crt"
    if ((Test-Path $bundle) -and ((Get-Item $bundle).Length -gt 50KB)) {
        return
    }
    Write-Step "MSYS2 CA bundle is missing/empty -- restoring from Git for Windows"
    $src = @(
        "C:\Program Files\Git\usr\ssl\certs\ca-bundle.crt",
        "C:\Program Files\Git\mingw64\ssl\certs\ca-bundle.crt"
    ) | Where-Object { (Test-Path $_) -and ((Get-Item $_).Length -gt 50KB) } | Select-Object -First 1
    if (-not $src) {
        throw "MSYS2 CA certificates are empty and Git for Windows CA bundle was not found."
    }
    foreach ($dest in @(
        $bundle,
        (Join-Path $Msys2Root "usr\ssl\cert.pem"),
        (Join-Path $Msys2Root "etc\pki\ca-trust\extracted\pem\tls-ca-bundle.pem")
    )) {
        $dir = Split-Path $dest -Parent
        if (-not (Test-Path $dir)) {
            New-Item -ItemType Directory -Force -Path $dir | Out-Null
        }
        Copy-Item -LiteralPath $src -Destination $dest -Force
    }
}

$bashExe = Join-Path $Msys2Root "usr\bin\bash.exe"
$msys2Shell = Join-Path $Msys2Root "msys2_shell.cmd"

if (-not (Test-Path $bashExe)) {
    Write-Step "MSYS2 not found at $Msys2Root -- installing via winget"
    winget install --id MSYS2.MSYS2 -e --source winget --accept-package-agreements --accept-source-agreements
    if (-not (Test-Path $bashExe)) {
        throw "MSYS2 still not found at $Msys2Root after install. If it installed elsewhere, rerun with -Msys2Root <path>."
    }
}

Restore-MsysCaBundle

# mingw-w64-ucrt-x86_64-toolchain pulls in gcc/g++/windres/objdump/binutils together.
# `zip` is the plain MSYS package (used only to zip the finished dist/ folder, no
# Windows-target linking involved, so it doesn't need a ucrt64 variant).
$requiredPkgs = @(
    "mingw-w64-ucrt-x86_64-toolchain",
    "mingw-w64-ucrt-x86_64-go",
    "mingw-w64-ucrt-x86_64-rust",
    "mingw-w64-ucrt-x86_64-openssl",
    "mingw-w64-ucrt-x86_64-opus",
    "mingw-w64-ucrt-x86_64-pkgconf",
    "mingw-w64-ucrt-x86_64-python",
    "zip"
)

Write-Step "Checking installed MSYS2/UCRT64 packages"
$pkgList = Invoke-Native -CaptureStdout -ScriptBlock { & $bashExe -lc "pacman -Qq" }
$installed = @($pkgList.Stdout | ForEach-Object { $_.Trim() } | Where-Object { $_ })
$missing = $requiredPkgs | Where-Object { $installed -notcontains $_ }

if ($missing.Count -gt 0) {
    Write-Step "Installing missing packages: $($missing -join ', ')"
    # `-Sy` (sync db without upgrading installed packages) risks a "partial upgrade":
    # newly resolved deps for the missing packages can conflict with older versions of
    # already-installed packages (e.g. gcc-libs vs. gcc-libgfortran). `-Syu` upgrades
    # everything together, which pacman/MSYS2 requires. A core-package update (e.g.
    # msys2-runtime) can terminate this shell mid-upgrade, so run it twice.
    $null = Invoke-Native -ScriptBlock { & $bashExe -lc "pacman -Syu --noconfirm" }
    Restore-MsysCaBundle
    $upgrade = Invoke-Native -ScriptBlock { & $bashExe -lc "pacman -Syu --noconfirm" }
    Restore-MsysCaBundle
    if ($upgrade.ExitCode -ne 0) { throw "pacman -Syu failed (exit $($upgrade.ExitCode))" }
    $install = Invoke-Native -ScriptBlock { & $bashExe -lc "pacman -S --needed --noconfirm $($missing -join ' ')" }
    Restore-MsysCaBundle
    if ($install.ExitCode -ne 0) { throw "pacman -S failed installing: $($missing -join ' ')" }
} else {
    Write-Step "All required UCRT64 packages already installed"
}

# $PSScriptRoot is agent/scripts; build_windows.sh's own REPO_ROOT (the "agent" dir) is
# one level up, matching how that script locates itself.
$AgentDir = Split-Path -Parent $PSScriptRoot

Write-Step "Building usbridge_agent inside MSYS2 UCRT64"
$build = Invoke-Native -ScriptBlock {
    & $msys2Shell -ucrt64 -defterm -no-start -where $AgentDir -c "./scripts/build_windows.sh"
}
if ($build.ExitCode -ne 0) {
    throw "Build failed (exit $($build.ExitCode))"
}

Write-Host "Done." -ForegroundColor Green
