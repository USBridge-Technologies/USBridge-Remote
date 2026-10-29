//go:build linux && !android

package netutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The Linux side of the Wi-Fi streaming tune: a connected Wi-Fi keeps
// scanning other channels in the background (iwd's periodic scan,
// wpa_supplicant's bgscan) and saves power between packets, and both show
// on a stream as the same periodic stutters as on Windows. One pkexec
// prompt:
//   - NetworkManager: a conf.d drop-in turning Wi-Fi power saving off
//     (wifi.powersave=2) for every connection;
//   - iwd: [Scan] DisablePeriodicScan=true in /etc/iwd/main.conf, then an
//     iwd restart (Wi-Fi reconnects for a few seconds);
//   - every Wi-Fi interface: `iw dev X set power_save off` right away.
//
// Not done: pinning the connection to the AP's BSSID (it would stop
// wpa_supplicant's roaming scans too), since a replaced router would then
// leave a headless host unreachable.
//
// Shared with the other app's identical file (USBridge-Remote agent and
// client), so tuning from either covers both.

const nmWiFiTunePath = "/etc/NetworkManager/conf.d/90-usbridge-wifi.conf"

const iwdMainConf = "/etc/iwd/main.conf"

func wifiInterfaces() []string {
	matches, _ := filepath.Glob("/sys/class/net/*/wireless")
	var out []string
	for _, m := range matches {
		out = append(out, filepath.Base(filepath.Dir(m)))
	}
	return out
}

func networkManagerPresent() bool {
	_, err := os.Stat("/etc/NetworkManager")
	return err == nil
}

func iwdPresent() bool {
	for _, p := range []string{"/usr/libexec/iwd", "/usr/lib/iwd/iwd", "/usr/bin/iwd"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func iwdPeriodicScanOff() bool {
	b, err := os.ReadFile(iwdMainConf)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.ReplaceAll(strings.TrimSpace(line), " ", "")
		if strings.EqualFold(line, "DisablePeriodicScan=true") {
			return true
		}
	}
	return false
}

// iwPath finds iw, which usually lives in an sbin outside a user's PATH.
func iwPath() string {
	if p, err := exec.LookPath("iw"); err == nil {
		return p
	}
	for _, p := range []string{"/usr/sbin/iw", "/sbin/iw"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func powerSaveOff(iface string) bool {
	iw := iwPath()
	if iw == "" {
		return true // no iw: nothing this can check or change
	}
	out, err := exec.Command(iw, "dev", iface, "get", "power_save").Output()
	if err != nil {
		return true
	}
	return strings.Contains(strings.ToLower(string(out)), "off")
}

// WLANConnected reports whether any Wi-Fi interface is up.
func WLANConnected() bool {
	if inSnap() {
		return nmWLANConnected()
	}
	for _, iface := range wifiInterfaces() {
		if b, err := os.ReadFile("/sys/class/net/" + iface + "/operstate"); err == nil && strings.TrimSpace(string(b)) == "up" {
			return true
		}
	}
	return false
}

// WiFiTuneStatus reports whether this machine's Wi-Fi has a one-time
// streaming tune to offer (applicable) and whether it is already in place
// (done).
func WiFiTuneStatus() (applicable, done bool) {
	if inSnap() {
		return nmWiFiTuneStatus()
	}
	ifaces := wifiInterfaces()
	if len(ifaces) == 0 {
		return false, false
	}
	done = true
	if networkManagerPresent() {
		if _, err := os.Stat(nmWiFiTunePath); err != nil {
			done = false
		}
	}
	if iwdPresent() && !iwdPeriodicScanOff() {
		done = false
	}
	for _, iface := range ifaces {
		if !powerSaveOff(iface) {
			done = false
		}
	}
	return true, done
}

const wifiTuneScript = `set -e
if [ -d /etc/NetworkManager ]; then
  mkdir -p /etc/NetworkManager/conf.d
  printf '# USBridge: Wi-Fi tuned for streaming (no power saving)\n[connection]\nwifi.powersave=2\n' > ` + nmWiFiTunePath + `
  chmod 644 ` + nmWiFiTunePath + `
  nmcli general reload conf >/dev/null 2>&1 || true
fi
if [ -x /usr/libexec/iwd ] || [ -x /usr/lib/iwd/iwd ] || [ -x /usr/bin/iwd ]; then
  mkdir -p /etc/iwd
  f=` + iwdMainConf + `
  touch "$f"
  if grep -qi '^[[:space:]]*DisablePeriodicScan' "$f"; then
    sed -i 's/^[[:space:]]*DisablePeriodicScan.*/DisablePeriodicScan=true/I' "$f"
  elif grep -q '^\[Scan\]' "$f"; then
    sed -i '/^\[Scan\]/a DisablePeriodicScan=true' "$f"
  else
    printf '\n[Scan]\nDisablePeriodicScan=true\n' >> "$f"
  fi
  if systemctl is-active -q iwd 2>/dev/null; then systemctl restart iwd || true; fi
fi
if command -v iw >/dev/null 2>&1; then
  for d in /sys/class/net/*/wireless; do
    [ -e "$d" ] || continue
    iw dev "$(basename "$(dirname "$d")")" set power_save off || true
  done
fi
`

// ApplyWiFiTune applies the tune above through one pkexec prompt (in a snap,
// through NetworkManager, see wifitune_nm_linux.go).
func ApplyWiFiTune() error {
	if inSnap() {
		return nmApplyWiFiTune()
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed; run this as root instead:\n%s", wifiTuneScript)
	}
	out, err := exec.Command("pkexec", "/bin/sh", "-c", wifiTuneScript).CombinedOutput()
	if err != nil {
		s := strings.TrimSpace(string(out))
		switch {
		case strings.Contains(s, "No authentication agent found"):
			return fmt.Errorf("no polkit authentication agent is running for this session, so pkexec cannot ask for the password")
		case strings.Contains(err.Error(), "exit status 126"):
			return fmt.Errorf("authentication was cancelled")
		case s != "":
			return fmt.Errorf("pkexec failed: %v (%s)", err, s)
		}
		return fmt.Errorf("pkexec failed: %v", err)
	}
	if _, done := WiFiTuneStatus(); !done {
		return fmt.Errorf("the Wi-Fi settings were written but are not in effect yet")
	}
	return nil
}
