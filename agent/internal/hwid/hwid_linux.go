//go:build linux

package hwid

import "fmt"

// rawMachineID on Linux reads systemd's /etc/machine-id (falling back to
// D-Bus's /var/lib/dbus/machine-id on a system without systemd, which is
// where D-Bus itself originally sourced this from and systemd later
// adopted the same file for). Generated once at OS install/first-boot time
// and stable across reboots and application reinstalls -- but NOT stable
// across a fresh OS reinstall, and by design differs between a VM/container
// and its host (cloud images and most container base images regenerate or
// clear it on first boot specifically so clones don't collide) -- see
// systemd-machine-id-setup(1)/machine-id(5).
func rawMachineID() (id string, source string, err error) {
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if v, readErr := readFileTrimmed(path); readErr == nil && v != "" {
			return v, "linux-machine-id", nil
		}
	}
	return "", "", fmt.Errorf("no readable machine-id at /etc/machine-id or /var/lib/dbus/machine-id")
}
