package service

import (
	"fmt"
	"net"

	"github.com/sirupsen/logrus"
)

// FindAvailableUDPPort подбирает свободный локальный UDP-порт.
// Сначала пробует preferred, затем запрашивает системный ephemeral-порт.
func FindAvailableUDPPort(preferred int) (int, error) {
	if preferred > 0 {
		conn, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", preferred))
		if err == nil {
			_ = conn.Close()
			return preferred, nil
		}
		logrus.Warnf("⚠️ Локальный UDP порт %d занят, выбираем свободный порт для видео", preferred)
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

// GetLocalIPForTarget определяет локальный IP-адрес интерфейса, через который
// будет осуществляться связь с указанным целевым адресом.
func GetLocalIPForTarget(target string) string {
	conn, err := net.Dial("udp", net.JoinHostPort(target, "80"))
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
