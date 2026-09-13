# Fast incremental rebuild — Go only, no DLL copy, no fyne package.
# Usage (from any PowerShell in the project root):
#   .\scripts\fast_rebuild.ps1
#
# ~15-30s on cached build; only changed packages recompile.
# For a full dist rebuild run .\scripts\build_windows.ps1 from PowerShell.

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path $PSScriptRoot -Parent
$ExeOut   = "$RepoRoot\.cache\build\windows-amd64\release\USBridge_Client.exe"
$DistExe  = "$RepoRoot\dist\windows\USBridge_Client.exe"

# Pin MSYS2 UCRT64 gcc. A plain PATH hit (e.g. Strawberry Perl's MinGW) links
# a PE with an empty .idata and Windows then fails with 0xc000007b before main.
$Msys2Root = if ($env:MSYS2_ROOT) { $env:MSYS2_ROOT } else { "C:\msys64" }
$Ucrt64Bin = Join-Path $Msys2Root "ucrt64\bin"
$Gcc = Join-Path $Ucrt64Bin "gcc.exe"
$Gpp = Join-Path $Ucrt64Bin "g++.exe"
if (-not (Test-Path $Gcc)) {
    Write-Error "MSYS2 UCRT64 gcc not found at $Gcc. Install mingw-w64-ucrt-x86_64-gcc or set MSYS2_ROOT."
    exit 1
}

$GoBin = Split-Path (Get-Command go).Source -Parent
Write-Host "==> fast_rebuild: go build..."
Write-Host "==> CC=$Gcc"
$env:CGO_ENABLED  = "1"
$env:GOOS         = "windows"
$env:GOARCH       = "amd64"
$env:CC           = $Gcc
$env:CXX          = $Gpp
$env:GOCACHE      = "$RepoRoot\.cache\go-build\windows-amd64"
$env:GOMODCACHE   = "$RepoRoot\.cache\go-mod"
$env:GOMAXPROCS   = "12"
# Go bin must stay ahead of ucrt64 — MSYS ships its own trimmed go.exe there.
$env:PATH         = "$GoBin;$Ucrt64Bin;$env:PATH"

# Vulkan headers — prefer ucrt64 native path, fallback to msys64
$ucrt64Inc = if (Test-Path (Join-Path $Msys2Root "ucrt64\include")) { "$($Msys2Root -replace '\\','/')/ucrt64/include" } else { "/ucrt64/include" }
$ucrt64Lib = if (Test-Path (Join-Path $Msys2Root "ucrt64\lib"))     { "$($Msys2Root -replace '\\','/')/ucrt64/lib" }     else { "/ucrt64/lib" }
$env:CGO_CFLAGS  = "-I$ucrt64Inc"
$env:CGO_LDFLAGS = "-L$ucrt64Lib -lvulkan-1 -lgdi32 -luser32"

$sw = [System.Diagnostics.Stopwatch]::StartNew()

Set-Location "$RepoRoot\cmd"
& go build -trimpath -ldflags="-H=windowsgui -extldflags=-Wl,--stack,8388608" -o $ExeOut .
if ($LASTEXITCODE -ne 0) { Write-Error "go build failed"; exit 1 }

$sw.Stop()
Write-Host "==> Build done in $($sw.Elapsed.TotalSeconds.ToString('0.0'))s"

Write-Host "==> Copying to dist\windows..."
Copy-Item $ExeOut $DistExe -Force
$size = (Get-Item $DistExe).Length / 1MB
Write-Host "==> Done: $DistExe ($($size.ToString('0.0')) MB)"
