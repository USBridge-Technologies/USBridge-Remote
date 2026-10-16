//go:build windows

package netutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Intel Wi-Fi drivers run background scans of their own on top of
// Windows' (which SetWLANStreamingMode turns off for a session): the
// driver's "Global BG Scan blocking" setting, BgScanGlobalBlocking in the
// adapter's registry key, ships as 0 (never block). 1 blocks them always;
// 2 only "on good RSSI", which a host at -70 dBm never has, so the tune
// sets 1. The driver reads it when the adapter starts, hence the adapter
// restart (Wi-Fi drops for a few seconds) and the one UAC prompt.
//
// Shared with the other app's identical file (USBridge-Remote agent and
// client), so tuning from either covers both.

const netAdapterClassKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`

const intelBGScanValue = "BgScanGlobalBlocking"

type intelWiFiAdapter struct {
	subkey  string // e.g. "0000" under netAdapterClassKey
	guid    string // NetCfgInstanceId == WLAN interface GUID
	blocked bool
}

// intelWiFiAdapters lists the Wi-Fi adapters present right now whose
// driver has the background scan blocking setting.
func intelWiFiAdapters() []intelWiFiAdapter {
	present := map[string]bool{}
	h, err := wlanOpen()
	if err != nil || h == 0 {
		return nil
	}
	ifaces, err := wlanInterfaces(h)
	procWlanCloseHandle.Call(h, 0)
	if err != nil {
		return nil
	}
	for guid := range ifaces {
		present[strings.ToUpper(guid.String())] = true
	}

	class, err := registry.OpenKey(registry.LOCAL_MACHINE, netAdapterClassKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer class.Close()
	names, _ := class.ReadSubKeyNames(-1)
	var out []intelWiFiAdapter
	for _, name := range names {
		k, err := registry.OpenKey(class, name, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		guid, _, _ := k.GetStringValue("NetCfgInstanceId")
		value, _, verr := k.GetStringValue(intelBGScanValue)
		k.Close()
		if verr != nil || !present[strings.ToUpper(guid)] {
			continue
		}
		out = append(out, intelWiFiAdapter{subkey: name, guid: guid, blocked: strings.TrimSpace(value) == "1"})
	}
	return out
}

// WiFiTuneStatus reports whether this machine's Wi-Fi has a one-time
// streaming tune to offer (applicable) and whether it is already in place
// (done). Cheap: a WLAN enumeration and a few registry reads.
func WiFiTuneStatus() (applicable, done bool) {
	adapters := intelWiFiAdapters()
	if len(adapters) == 0 {
		return false, false
	}
	for _, a := range adapters {
		if !a.blocked {
			return true, false
		}
	}
	return true, true
}

// ApplyWiFiTune sets BgScanGlobalBlocking=1 on every such adapter and
// restarts it, through one UAC prompt.
func ApplyWiFiTune() error {
	adapters := intelWiFiAdapters()
	if len(adapters) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'Stop'\r\n")
	for _, a := range adapters {
		fmt.Fprintf(&b, "Set-ItemProperty -Path 'HKLM:\\%s\\%s' -Name '%s' -Value '1' -Type String\r\n", netAdapterClassKey, a.subkey, intelBGScanValue)
		fmt.Fprintf(&b, "Get-NetAdapter | Where-Object { $_.InterfaceGuid -eq '%s' } | Restart-NetAdapter -Confirm:$false\r\n", a.guid)
	}
	script := filepath.Join(os.TempDir(), "usbridge-wifi-tune.ps1")
	if err := os.WriteFile(script, []byte(b.String()), 0o644); err != nil {
		return err
	}
	defer os.Remove(script)
	if err := runElevated("powershell.exe", `-NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "`+script+`"`); err != nil {
		return err
	}
	if _, done := WiFiTuneStatus(); !done {
		return fmt.Errorf("the Wi-Fi driver setting did not change")
	}
	return nil
}

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// runElevated runs file with params through a UAC prompt and waits for it.
func runElevated(file, params string) error {
	verb, _ := windows.UTF16PtrFromString("runas")
	f, _ := windows.UTF16PtrFromString(file)
	p, _ := windows.UTF16PtrFromString(params)
	const seeMaskNoCloseProcess = 0x40
	info := shellExecuteInfo{fMask: seeMaskNoCloseProcess, lpVerb: verb, lpFile: f, lpParameters: p}
	info.cbSize = uint32(unsafe.Sizeof(info))
	if r, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if e == windows.ERROR_CANCELLED {
			return fmt.Errorf("administrator rights were not granted")
		}
		return e
	}
	defer windows.CloseHandle(info.hProcess)
	if ev, err := windows.WaitForSingleObject(info.hProcess, 90_000); err != nil || ev != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("the elevated command did not finish")
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.hProcess, &code); err == nil && code != 0 {
		return fmt.Errorf("the elevated command failed (exit code %d)", code)
	}
	return nil
}
