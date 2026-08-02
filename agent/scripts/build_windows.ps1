#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Builds usbridge_agent for Windows, driving MSYS2 UCRT64 entirely from PowerShell.

.DESCRIPTION
    build_windows.sh (the actual build logic: Go build, DLL bundling, rust-shine build,
    Tailscale staging) still runs inside the UCRT64 toolchain -- Go's cgo (needed for
    Fyne/OpenGL) requires a GCC-compatible compiler, which on Windows means mingw-w64.
    This script just removes the manual steps around that: no more opening an "MSYS2
    UCRT64" terminal by hand and running pacman yourself. It installs MSYS2 itself (via
    winget) if missing, installs whichever required UCRT64 packages aren't already
    present, then invokes build_windows.sh non-interactively inside UCRT64 and streams
    its output back here.

.PARAMETER Streamer
    sunshine or rustshine (default: rustshine).

.PARAMETER Msys2Root
    Path to the MSYS2 install. Defaults to $env:MSYS2_ROOT or C:\msys64.

.EXAMPLE
    ./agent/scripts/build_windows.ps1
    ./agent/scripts/build_windows.ps1 -Streamer sunshine
#>
param(
    [ValidateSet("sunshine", "rustshine")]
    [string]$Streamer = "rustshine",
    [string]$Msys2Root = $(if ($env:MSYS2_ROOT) { $env:MSYS2_ROOT } else { "C:\msys64" })
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$Message) {
    Write-Host "==> $Message" -ForegroundColor Cyan
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
$installed = @(& $bashExe -lc "pacman -Qq" 2>$null) -split "`n" | ForEach-Object { $_.Trim() }
$missing = $requiredPkgs | Where-Object { $installed -notcontains $_ }

if ($missing.Count -gt 0) {
    Write-Step "Installing missing packages: $($missing -join ', ')"
    & $bashExe -lc "pacman -Sy --noconfirm"
    if ($LASTEXITCODE -ne 0) { throw "pacman -Sy failed (exit $LASTEXITCODE)" }
    & $bashExe -lc "pacman -S --needed --noconfirm $($missing -join ' ')"
    if ($LASTEXITCODE -ne 0) { throw "pacman -S failed installing: $($missing -join ' ')" }
} else {
    Write-Step "All required UCRT64 packages already installed"
}

# $PSScriptRoot is agent/scripts; build_windows.sh's own REPO_ROOT (the "agent" dir) is
# one level up, matching how that script locates itself.
$AgentDir = Split-Path -Parent $PSScriptRoot

if ($Streamer -eq "rustshine") {
    $checkoutRoot = Split-Path -Parent $AgentDir
    $rustShineDir = Join-Path (Split-Path -Parent $checkoutRoot) "rust-shine"
    if (-not (Test-Path $rustShineDir)) {
        throw "rust-shine checkout not found at $rustShineDir (expected as a sibling of this repo's checkout). Clone itsme228/rust-shine there, or set RUSTSHINE_SRC_DIR before running."
    }
}

Write-Step "Building usbridge_agent (streamer=$Streamer) inside MSYS2 UCRT64"
& $msys2Shell -ucrt64 -defterm -no-start -where $AgentDir -c "./scripts/build_windows.sh -streamer $Streamer"
if ($LASTEXITCODE -ne 0) {
    throw "Build failed (exit $LASTEXITCODE)"
}

Write-Host "Done." -ForegroundColor Green
