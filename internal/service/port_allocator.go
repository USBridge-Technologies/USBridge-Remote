package service

import (
	"fmt"
	"net"

	"github.com/sirupsen/logrus"
)

// FindAvailableUDPPort finds an available local UDP port.
// First it tries the preferred port, then requests a system ephemeral port.
func FindAvailableUDPPort(preferred int) (int, error) {
	if preferred > 0 {
		conn, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", preferred))
		if err == nil {
			_ = conn.Close()
			return preferred, nil
		}
		logrus.Warnf("⚠️ Local UDP port %d is busy, selecting an available port for video", preferred)
	}

	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected local UDP addr type: %T", conn.LocalAddr())
	}
	return addr.Port, nil
}

// GetLocalIPForTarget determines the local IP address of the interface through which
// communication with the specified target address will be carried out.
func GetLocalIPForTarget(target string) string {
	conn, err := net.Dial("udp", net.JoinHostPort(target, "80"))
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
