# Installs (or with -Uninstall, removes) the VirtualDrivers "Virtual Display
# Driver" (MttVDD, https://github.com/VirtualDrivers/Virtual-Display-Driver,
# MIT) -- the Windows virtual-monitor backend `crates/virtual-display` falls
# back to when SudoVDA isn't installed. See docs/VIRTUAL_DISPLAY.md.
#
# Must run elevated. No nefcon/devcon needed: the root-enumerated device node
# is created directly through SetupAPI, the same calls `devcon install` makes.
#
# The driver's catalog is signed by SignPath Foundation (publicly trusted
# GlobalSign code-signing chain), so no root certificate is added -- only the
# signer's leaf certificate goes into LocalMachine\TrustedPublisher, which just
# suppresses the one-time "trust this publisher?" prompt.
#
# The driver reads its monitor count and mode list from
# C:\VirtualDisplayDriver\vdd_settings.xml only when the device starts, so this
# script writes one with a broad mode list up front: the unelevated streamer
# can only pick from that list (see crates/virtual-display/src/windows/mttvdd.rs
# for why there's no runtime way to add to it). Edit it, then
# `pnputil /restart-device <instance id>` (elevated) to change the list.
[CmdletBinding()]
param(
    [string]$DriverDir,
    [string]$DriverUrl = "https://github.com/VirtualDrivers/Virtual-Display-Driver/releases/download/25.7.23/VirtualDisplayDriver-x86.Driver.Only.zip",
    [string]$ConfigDir = "C:\VirtualDisplayDriver",
    # Transcript of this run, for a caller that can't see the elevated
    # console (the USBridge agent's "Download" button).
    [string]$LogPath,
    [switch]$Uninstall
)
$ErrorActionPreference = "Stop"
if ($LogPath) { Start-Transcript -Path $LogPath -Force | Out-Null }

$HardwareId = "Root\MttVDD"

if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run this script from an elevated (Administrator) PowerShell."
}

Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;
public static class MttVddSetup {
    [StructLayout(LayoutKind.Sequential)]
    public struct SP_DEVINFO_DATA { public uint cbSize; public Guid ClassGuid; public uint DevInst; public IntPtr Reserved; }

    [DllImport("setupapi.dll", SetLastError = true)]
    static extern IntPtr SetupDiCreateDeviceInfoList(ref Guid ClassGuid, IntPtr hwndParent);
    [DllImport("setupapi.dll", SetLastError = true, CharSet = CharSet.Unicode)]
    static extern bool SetupDiCreateDeviceInfoW(IntPtr set, string name, ref Guid classGuid, string desc, IntPtr hwnd, uint flags, ref SP_DEVINFO_DATA data);
    [DllImport("setupapi.dll", SetLastError = true, CharSet = CharSet.Unicode)]
    static extern bool SetupDiSetDeviceRegistryPropertyW(IntPtr set, ref SP_DEVINFO_DATA data, uint prop, byte[] buf, uint size);
    [DllImport("setupapi.dll", SetLastError = true)]
    static extern bool SetupDiCallClassInstaller(uint fn, IntPtr set, ref SP_DEVINFO_DATA data);
    [DllImport("setupapi.dll", SetLastError = true)]
    static extern bool SetupDiDestroyDeviceInfoList(IntPtr set);
    [DllImport("newdev.dll", SetLastError = true, CharSet = CharSet.Unicode)]
    static extern bool UpdateDriverForPlugAndPlayDevicesW(IntPtr hwnd, string hwid, string inf, uint flags, out bool reboot);

    const uint DICD_GENERATE_ID = 1, SPDRP_HARDWAREID = 1, DIF_REGISTERDEVICE = 0x19, INSTALLFLAG_FORCE = 1;

    // devcon install: create a root devnode carrying hwid, register it, then bind the INF to it.
    public static bool Install(string inf, string hwid) {
        Guid display = new Guid("4d36e968-e325-11ce-bfc1-08002be10318");
        IntPtr set = SetupDiCreateDeviceInfoList(ref display, IntPtr.Zero);
        if (set == new IntPtr(-1)) throw new System.ComponentModel.Win32Exception();
        try {
            var data = new SP_DEVINFO_DATA { cbSize = (uint)Marshal.SizeOf(typeof(SP_DEVINFO_DATA)) };
            if (!SetupDiCreateDeviceInfoW(set, "Display", ref display, null, IntPtr.Zero, DICD_GENERATE_ID, ref data))
                throw new System.ComponentModel.Win32Exception();
            byte[] ids = System.Text.Encoding.Unicode.GetBytes(hwid + "\0\0");
            if (!SetupDiSetDeviceRegistryPropertyW(set, ref data, SPDRP_HARDWAREID, ids, (uint)ids.Length))
                throw new System.ComponentModel.Win32Exception();
            if (!SetupDiCallClassInstaller(DIF_REGISTERDEVICE, set, ref data))
                throw new System.ComponentModel.Win32Exception();
        } finally { SetupDiDestroyDeviceInfoList(set); }
        bool reboot;
        if (!UpdateDriverForPlugAndPlayDevicesW(IntPtr.Zero, hwid, inf, INSTALLFLAG_FORCE, out reboot))
            throw new System.ComponentModel.Win32Exception();
        return reboot;
    }
}
"@

function Get-MttVddDevices {
    Get-PnpDevice -ErrorAction SilentlyContinue | Where-Object { $_.HardwareID -contains $HardwareId }
}

if ($Uninstall) {
    foreach ($dev in Get-MttVddDevices) {
        Write-Host "Removing device $($dev.InstanceId)"
        & pnputil.exe /remove-device $dev.InstanceId | Out-Host
    }
    $infs = (& pnputil.exe /enum-drivers) -join "`n" -split "`n\s*`n" | Where-Object { $_ -match "(?i)mttvdd\.inf" } |
        ForEach-Object { if ($_ -match "Published Name:\s+(\S+)") { $Matches[1] } }
    foreach ($inf in $infs) {
        Write-Host "Deleting driver package $inf"
        & pnputil.exe /delete-driver $inf /uninstall /force | Out-Host
    }
    Write-Host "MttVDD removed. ($ConfigDir left in place.)"
    return
}

if (Get-MttVddDevices) {
    # Installed but stopped (e.g. CM_PROB_FAILED_POST_START after its host
    # crashed a few times in a row): a device restart is the whole fix.
    foreach ($dev in (Get-MttVddDevices | Where-Object { $_.Status -ne "OK" })) {
        Write-Host "Restarting $($dev.InstanceId) (status $($dev.Status), problem $($dev.Problem))"
        & pnputil.exe /restart-device $dev.InstanceId | Out-Host
    }
    Write-Host "MttVDD is already installed:"
    Get-MttVddDevices | Format-Table -AutoSize Status, FriendlyName, InstanceId | Out-Host
    return
}

if (-not $DriverDir) {
    $tmp = Join-Path $env:TEMP ("mttvdd-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Force $tmp | Out-Null
    Write-Host "Downloading $DriverUrl"
    Invoke-WebRequest -Uri $DriverUrl -OutFile "$tmp\vdd.zip" -UseBasicParsing
    Expand-Archive "$tmp\vdd.zip" -DestinationPath $tmp -Force
    $DriverDir = Join-Path $tmp "VirtualDisplayDriver"
}
$inf = Join-Path $DriverDir "MttVDD.inf"
$cat = Join-Path $DriverDir "mttvdd.cat"
if (-not (Test-Path $inf)) { throw "MttVDD.inf not found in $DriverDir" }

$sig = Get-AuthenticodeSignature $cat
if ($sig.Status -ne "Valid") { throw "mttvdd.cat signature is not valid: $($sig.Status) $($sig.StatusMessage)" }
# The signer is about to become a trusted publisher machine-wide: only accept
# the one VirtualDrivers' releases are actually signed by, not merely any
# valid signature on whatever the download returned.
if ($sig.SignerCertificate.Subject -notmatch '^CN=SignPath Foundation,') {
    throw "mttvdd.cat is signed by an unexpected publisher: $($sig.SignerCertificate.Subject)"
}
Write-Host "Catalog signed by: $($sig.SignerCertificate.Subject)"
$store = New-Object System.Security.Cryptography.X509Certificates.X509Store("TrustedPublisher", "LocalMachine")
$store.Open("ReadWrite"); $store.Add($sig.SignerCertificate); $store.Close()

# Config first: the driver reads it once, when the device starts.
New-Item -ItemType Directory -Force $ConfigDir | Out-Null
$settings = Join-Path $ConfigDir "vdd_settings.xml"
if (-not (Test-Path $settings)) {
    # Keep the total (resolutions x (1 + global rates)) at 100 or fewer: at 104
    # IddCxMonitorArrival fails with STATUS_NOT_SUPPORTED and the monitor never
    # appears (verified live against 25.7.23; 100 was fine).
    # 60 Hz comes from each <resolution>, the rest from <global>, so no dupes:
    # 22 x 4 = 88 modes.
    $modes = @(
        "1280x720", "1366x768", "1440x900", "1600x900", "1920x1080", "1920x1200", "2048x1536", "2256x1504",
        "2360x1640", "2388x1668", "2400x1080", "2532x1170", "2556x1179", "2560x1080", "2560x1440", "2560x1600",
        "2732x2048", "2796x1290", "2880x1800", "3440x1440", "3840x2160", "5120x1440"
    )
    $resXml = ($modes | ForEach-Object { $w, $h = $_ -split "x"; "        <resolution><width>$w</width><height>$h</height><refresh_rate>60</refresh_rate></resolution>" }) -join "`n"
    $rates = (@(90, 120, 144) | ForEach-Object { "        <g_refresh_rate>$_</g_refresh_rate>" }) -join "`n"
    @"
<?xml version='1.0' encoding='utf-8'?>
<!-- Written by rust-shine scripts/install-mttvdd.ps1. The driver only reads this when the device starts. -->
<vdd_settings>
    <monitors><count>1</count></monitors>
    <gpu><friendlyname>default</friendlyname></gpu>
    <global>
$rates
    </global>
    <resolutions>
$resXml
    </resolutions>
    <logging>
        <SendLogsThroughPipe>false</SendLogsThroughPipe>
        <logging>false</logging>
        <debuglogging>false</debuglogging>
    </logging>
    <colour><SDR10bit>false</SDR10bit><HDRPlus>false</HDRPlus><ColourFormat>RGB</ColourFormat></colour>
    <cursor>
        <HardwareCursor>true</HardwareCursor><CursorMaxX>128</CursorMaxX><CursorMaxY>128</CursorMaxY>
        <AlphaCursorSupport>true</AlphaCursorSupport><XorCursorSupportLevel>2</XorCursorSupportLevel>
    </cursor>
    <edid><CustomEdid>false</CustomEdid><PreventSpoof>false</PreventSpoof><EdidCeaOverride>false</EdidCeaOverride></edid>
</vdd_settings>
"@ | Set-Content -Path $settings -Encoding UTF8
}
New-Item -Force "HKLM:\SOFTWARE\MikeTheTech\VirtualDisplayDriver" | Out-Null
Set-ItemProperty "HKLM:\SOFTWARE\MikeTheTech\VirtualDisplayDriver" -Name VDDPATH -Value $ConfigDir

Write-Host "Creating $HardwareId device node and installing $inf"
$reboot = [MttVddSetup]::Install((Resolve-Path $inf).Path, $HardwareId)
Get-MttVddDevices | Format-Table -AutoSize Status, FriendlyName, InstanceId | Out-Host
if ($reboot) { Write-Warning "Windows asks for a reboot to finish installing the driver." }
