//go:build windows

package account

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func collectPlatformHardware() (cpu, gpu string, ramMB int) {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE); err == nil {
		cpu, _, _ = k.GetStringValue("ProcessorNameString")
		k.Close()
	}

	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	ms := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ms))); r != 0 {
		ramMB = int(ms.TotalPhys / (1024 * 1024))
	}

	// Display adapters live under the display device class key, one numbered
	// subkey each (0000, 0001, ...) alongside non-adapter entries like
	// "Configuration", which simply have no DriverDesc.
	const classKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, classKey, registry.ENUMERATE_SUB_KEYS); err == nil {
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		var gpus []string
		for _, n := range names {
			sub, err := registry.OpenKey(registry.LOCAL_MACHINE, classKey+`\`+n, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			if d, _, err := sub.GetStringValue("DriverDesc"); err == nil && d != "" {
				gpus = append(gpus, d)
			}
			sub.Close()
		}
		if len(gpus) > 2 {
			gpus = gpus[:2]
		}
		gpu = strings.Join(gpus, " + ")
	}
	return
}
