package controller

import (
	"fmt"
	"net"
	"runtime"
	"strings"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// getLocalIP determines the local IP used to reach the USBridge device.
func (dw *DiskWidget) getLocalIP() (string, error) {
	if dw.usbClient == nil {
		return "", fmt.Errorf("USB client not initialized")
	}
	baseURL := dw.usbClient.GetBaseURL()
	if baseURL == "" {
		return "", fmt.Errorf("unable to get the USB client's base URL")
	}
	host := strings.TrimPrefix(baseURL, "http://")
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	conn, err := net.Dial("udp", host+":8080")
	if err != nil {
		return "", fmt.Errorf("unable to determine local IP: %v", err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// getAvailablePort finds a free TCP port for an NBD server.
func (dw *DiskWidget) getAvailablePort() (int, error) {
	const basePort = 10809
	const maxAttempts = 100

	logrus.Infof("🔍 Searching for a free port starting from %d...", basePort)
	for i := 0; i < maxAttempts; i++ {
		port := basePort + i
		portInUse := false
		dw.nbdServersMu.Lock()
		for exportName, server := range dw.nbdServers {
			if server.IsRunning() && server.GetServerStatus()["server_port"] == port {
				logrus.Debugf("🔍 Port %d is in use by NBD server %s", port, exportName)
				portInUse = true
				break
			}
		}
		dw.nbdServersMu.Unlock()
		if !portInUse {
			if listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port)); err == nil {
				listener.Close()
				logrus.Infof("✅ Found free port: %d", port)
				return port, nil
			}
		}
	}
	return 0, fmt.Errorf("failed to find a free port in range %d-%d", basePort, basePort+maxAttempts-1)
}

// getDeviceIP returns the IP of the usbridge device we are connected to.
// This is used to restrict NBD connections to that specific host only.
func (dw *DiskWidget) getDeviceIP() string {
	if dw.usbClient == nil {
		return ""
	}
	baseURL := dw.usbClient.GetBaseURL()
	host := strings.TrimPrefix(baseURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return strings.TrimSpace(host)
}

// resolveNBDBindHost returns the address the NBD server should listen on.
// FRP tunnel → 127.0.0.1; Tailscale → 100.x.x.x interface; default → 127.0.0.1.
func (dw *DiskWidget) resolveNBDBindHost() string {
	if dw.frpService != nil {
		return "127.0.0.1"
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		if !strings.Contains(name, "tailscale") && !strings.Contains(name, "wg") && !strings.Contains(name, "tun") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ip := ipnet.IP.To4(); ip != nil && ip[0] == 100 {
					return ip.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// startNBDServer starts an NBD server for the given disk file.
// VMDK/QCOW2/VDI on desktop use qemu-nbd; ISO/raw/img use the go-nbd path.
func (dw *DiskWidget) startNBDServer(diskInfo *models.DiskInfo, port int, exportName string, readOnly bool) (service.NBDRunner, error) {
	logrus.Infof("🔧 [NBD] Creating: file=%q port=%d export=%s ro=%v", diskInfo.Name, port, exportName, readOnly)

	useQemuNbd := runtime.GOOS != "android" &&
		!strings.HasPrefix(diskInfo.Path, "content://") &&
		service.IsQemuNbdFormatForPath(diskInfo.Path)

	if useQemuNbd {
		bindHost := dw.resolveNBDBindHost()
		runner := service.NewQemuNBDRunner(diskInfo.Path, readOnly, bindHost)
		if deviceIP := dw.getDeviceIP(); deviceIP != "" {
			runner.SetAllowedIP(deviceIP)
		}
		if err := runner.EnsureQemuNbdForExport(); err != nil {
			return nil, fmt.Errorf("VMDK/QCOW2/VDI images require qemu-nbd: %w", err)
		}
		if err := runner.Start(port); err != nil {
			return nil, fmt.Errorf("VMDK/QCOW2/VDI images require qemu-nbd (install QEMU): %w", err)
		}
		logrus.Infof("✅ [NBD-QEMU] started on %s:%d for %s", bindHost, port, diskInfo.Name)
		return runner, nil
	}

	bindHost := dw.resolveNBDBindHost()
	nbdServer := service.NewNBDServerWithApp(bindHost, dw.app)
	if deviceIP := dw.getDeviceIP(); deviceIP != "" {
		nbdServer.SetAllowedIP(deviceIP)
	}
	if err := nbdServer.Start(port); err != nil {
		return nil, fmt.Errorf("error starting NBD server: %v", err)
	}
	export := &models.DiskExport{
		Name:        diskInfo.Name,
		FilePath:    diskInfo.Path,
		Size:        diskInfo.Size,
		ReadOnly:    readOnly,
		Description: diskInfo.Description,
		IsActive:    true,
		ExportName:  exportName,
	}
	if err := nbdServer.AddExport(export); err != nil {
		nbdServer.Stop()
		return nil, fmt.Errorf("error adding NBD export: %v", err)
	}
	logrus.Infof("✅ [NBD] server for %q on %s:%d", diskInfo.Name, bindHost, port)
	return nbdServer, nil
}
