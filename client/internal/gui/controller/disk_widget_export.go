package controller

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// getLocalIP determines the local IP the agent should connect back to for
// the iSCSI portal.
//
// When the agent's address is a Tailscale one (100.x.x.x or *.ts.net), this
// client's own Tailscale (tsnet) service is queried for our tailnet IP
// instead of the net.Dial-below approach — deliberately: this client's
// Tailscale connectivity is a userspace tsnet stack embedded in the app
// process, not an OS-level VPN, so the kernel routing table has no route to
// 100.x.x.x at all and a plain net.Dial("udp", <tailscale-ip>:8080) doesn't
// fail (UDP "connect" just picks a route without verifying reachability) —
// it silently returns whichever LAN/Wi-Fi address the OS's default route
// would use, which the agent then can't ever reach the resulting iSCSI
// target at ("connection refused"/timeout, nothing wrong with the network,
// just advertising the wrong address).
func (dw *DiskWidget) getLocalIP() (string, error) {
	if dw.usbClient == nil {
		return "", fmt.Errorf("USB client not initialized")
	}
	baseURL := dw.usbClient.GetBaseURL()
	if baseURL == "" {
		return "", fmt.Errorf("unable to get the USB client's base URL")
	}
	host := strings.TrimPrefix(baseURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	if isLikelyTailscaleHost(host) && dw.tailscaleService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if ip, err := dw.tailscaleService.TailnetIPv4(ctx); err == nil && ip != "" {
			return ip, nil
		}
		logrus.Warnf("⚠️ [ISCSI] agent host %q looks like Tailscale but this client's own tailnet IP lookup failed; falling back to route-derived local IP", host)
	}

	conn, err := net.Dial("udp", host+":8080")
	if err != nil {
		return "", fmt.Errorf("unable to determine local IP: %v", err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// isLikelyTailscaleHost reports whether host looks like a Tailscale
// address (100.x CGNAT range or a *.ts.net MagicDNS name).
func isLikelyTailscaleHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host != "" && (strings.HasSuffix(host, ".ts.net") || strings.HasPrefix(host, "100."))
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
// on. Always "0.0.0.0" (all interfaces) — deliberately NOT trying to guess
// a single "right" interface (e.g. preferring Tailscale) here: the address
// actually advertised to the agent as the connect-back target comes from
// getLocalIP() (the local address the OS routing table picks to reach the
// agent's own host), which can legitimately be a LAN address even when a
// Tailscale interface also exists — binding only to that interface's IP
// then silently made the target unreachable at the address the agent was
// actually told to dial ("connection refused", nothing wrong on the
// network, just bound to the wrong interface). Access is still restricted
// to the agent's IP by the allow-list proxy (SetAllowedIP) started right
// after this, so listening on every interface here doesn't widen exposure.
func (dw *DiskWidget) resolveExportBindHost() string {
	return "0.0.0.0"
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
	// When the agent is reached over Tailscale, the port must be accepted
	// through this client's own tsnet identity (TailscaleService.Listen) —
	// see its doc comment and resolveExportBindHost's: a plain OS-level
	// bind never sees traffic arriving over tsnet's userspace tunnel.
	if isLikelyTailscaleHost(dw.getDeviceIP()) && dw.tailscaleService != nil {
		runner.SetTailscaleListenFunc(dw.tailscaleService.Listen)
	}
	if err := runner.Start(port); err != nil {
		return nil, fmt.Errorf("error starting iSCSI target: %v", err)
	}
	logrus.Infof("✅ [ISCSI] target %q for %q on %s:%d", iqn, diskInfo.Name, bindHost, port)
	return runner, nil
}
