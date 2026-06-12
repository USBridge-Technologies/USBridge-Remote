# Fast incremental rebuild — Go only, no DLL copy, no fyne package.
# Usage (from any PowerShell in the project root):
#   .\scripts\fast_rebuild.ps1
#
# ~15-30s on cached build; only changed packages recompile.
# For a full dist rebuild run scripts\build_windows.sh from UCRT64 shell.

$ErrorActionPreference = "Stop"
$RepoRoot = Split-Path $PSScriptRoot -Parent
$ExeOut   = "$RepoRoot\.cache\build\windows-amd64\release\USBridge_Client.exe"
$DistExe  = "$RepoRoot\dist\windows\USBridge_Client.exe"

Write-Host "==> fast_rebuild: go build..."
$env:CGO_ENABLED  = "1"
$env:GOOS         = "windows"
$env:GOARCH       = "amd64"
$env:GOCACHE      = "$RepoRoot\.cache\go-build\windows-amd64"
$env:GOMODCACHE   = "$RepoRoot\.cache\go-mod"
$env:GOMAXPROCS   = "12"

# Vulkan headers — prefer ucrt64 native path, fallback to msys64
$ucrt64Inc = if (Test-Path "C:\msys64\ucrt64\include") { "C:/msys64/ucrt64/include" } else { "/ucrt64/include" }
$ucrt64Lib = if (Test-Path "C:\msys64\ucrt64\lib")     { "C:/msys64/ucrt64/lib" }     else { "/ucrt64/lib" }
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
