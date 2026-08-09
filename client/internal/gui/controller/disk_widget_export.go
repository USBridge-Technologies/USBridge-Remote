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

// getAvailablePort finds a free TCP port for a disk-export server, starting
// from the standard iSCSI portal port (3260).
func (dw *DiskWidget) getAvailablePort() (int, error) {
	const basePort = 3260
	const maxAttempts = 100

	logrus.Infof("🔍 Searching for a free port starting from %d...", basePort)
	for i := 0; i < maxAttempts; i++ {
		port := basePort + i
		portInUse := false
		dw.exportServersMu.Lock()
		for exportName, server := range dw.exportServers {
			if server.IsRunning() && server.GetServerStatus()["server_port"] == port {
				logrus.Debugf("🔍 Port %d is in use by export server %s", port, exportName)
				portInUse = true
				break
			}
		}
		dw.exportServersMu.Unlock()
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
// This is used to restrict the export server to connections from that
// specific host only.
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

// resolveExportBindHost returns the address the export server should listen
// on. Tailscale → 100.x.x.x interface; default → 127.0.0.1.
func (dw *DiskWidget) resolveExportBindHost() string {
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

// startExportServer starts an iSCSI target server for the given disk file.
func (dw *DiskWidget) startExportServer(diskInfo *models.DiskInfo, port int, exportName string, readOnly bool) (service.BlockExportRunner, error) {
	logrus.Infof("🔧 [ISCSI] Creating target: file=%q port=%d export=%s ro=%v", diskInfo.Name, port, exportName, readOnly)

	bindHost := dw.resolveExportBindHost()
	iqn := service.BuildTargetIQN(exportName, fmt.Sprintf("%d", port))

	var runner *service.IscsiTargetRunner
	if runtime.GOOS == "android" && strings.HasPrefix(diskInfo.Path, "content://") {
		// gotgt's stock backing store needs a real filesystem path; the
		// androidsaf backing store (registered in
		// iscsi_backingstore_saf.go) bridges an already-open SAF fd instead
		// — NewIscsiTargetRunnerWithApp needs dw.app to reach
		// platform.SAFHelper.
		runner = service.NewIscsiTargetRunnerWithApp(diskInfo.Path, readOnly, bindHost, iqn, dw.app)
	} else {
		runner = service.NewIscsiTargetRunner(diskInfo.Path, readOnly, bindHost, iqn)
	}
	if deviceIP := dw.getDeviceIP(); deviceIP != "" {
		runner.SetAllowedIP(deviceIP)
	}
	if err := runner.Start(port); err != nil {
		return nil, fmt.Errorf("error starting iSCSI target: %v", err)
	}
	logrus.Infof("✅ [ISCSI] target %q for %q on %s:%d", iqn, diskInfo.Name, bindHost, port)
	return runner, nil
}
