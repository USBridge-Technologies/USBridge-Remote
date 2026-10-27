# Rebuild moonlight-common-c + the Go client, replace the installed exe, restart it.
# Usage: .\scripts\dev_rebuild_restart.ps1 [-NoRestart] [-InstallDir <path>]
#
# Targets the installed client (default %LOCALAPPDATA%\Programs\USBridge Client)
# so the logs end up where the installed build writes them (<InstallDir>\logs\app.log).
# The old exe is kept as USBridge_Client.exe.bak.

param(
    [switch]$NoRestart,
    [string]$InstallDir = "$env:LOCALAPPDATA\Programs\USBridge Client"
)

$ErrorActionPreference = "Continue"   # cgo prints warnings on stderr; check exit codes instead
$RepoRoot  = Split-Path $PSScriptRoot -Parent
$Ucrt64Bin = "C:\msys64\ucrt64\bin"
$ExeOut    = "$RepoRoot\.cache\build\windows-amd64\release\USBridge_Client.exe"
$Target    = "$InstallDir\bin\USBridge_Client.exe"

$env:PATH            = "$((Split-Path (Get-Command go).Source -Parent));$Ucrt64Bin;$env:PATH"
$env:CGO_ENABLED     = "1"
$env:GOOS            = "windows"
$env:GOARCH          = "amd64"
$env:CC              = "$Ucrt64Bin\gcc.exe"
$env:CXX             = "$Ucrt64Bin\g++.exe"
$env:GOCACHE         = "$RepoRoot\.cache\go-build\windows-amd64"
$env:GOMODCACHE      = "$RepoRoot\.cache\go-mod"
$env:GOFLAGS         = "-buildvcs=false"
$env:PKG_CONFIG_PATH = "C:\msys64\ucrt64\lib\pkgconfig"
$env:PKG_CONFIG      = "$Ucrt64Bin\pkg-config.exe"

Write-Host "==> ninja (moonlight-common-c)"
& "$Ucrt64Bin\ninja.exe" -C "$RepoRoot\moonlight-common-c\build"
if ($LASTEXITCODE -ne 0) { Write-Host "ninja failed"; exit 1 }

Write-Host "==> go build"
$Version = (Get-Content -Raw "$RepoRoot\VERSION").Trim()
Copy-Item -Force "$RepoRoot\VERSION" "$RepoRoot\cmd\VERSION"
New-Item -ItemType Directory -Force -Path (Split-Path $ExeOut) | Out-Null
Push-Location "$RepoRoot\cmd"
& go build -trimpath -tags usbpass_gousb "-ldflags=-H=windowsgui -X main.version=$Version -extldflags=-Wl,--stack,8388608" -o $ExeOut .
$rc = $LASTEXITCODE
Pop-Location
if ($rc -ne 0) { Write-Host "go build failed"; exit 1 }

Write-Host "==> stopping running client"
Get-Process -Name "USBridge_Client" -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1

if (Test-Path $Target) { Copy-Item -Force $Target "$Target.bak" }
Copy-Item -Force $ExeOut $Target
Write-Host "==> installed: $Target"

if (-not $NoRestart) {
    Start-Process -FilePath $Target -WorkingDirectory (Split-Path $Target)
    Write-Host "==> started. Log: $InstallDir\logs\app.log"
}
