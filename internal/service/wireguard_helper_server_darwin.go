//go:build darwin

package service

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/conn"
	wgdevice "golang.zx2c4.com/wireguard/device"
	wgtun "golang.zx2c4.com/wireguard/tun"
)

type darwinWireGuardHelperServer struct {
	mutex      sync.Mutex
	tunDevice  wgtun.Device
	bind       conn.Bind
	device     *wgdevice.Device
	ifaceName  string
	routes     []string
	clientHost string
	serverHost string
}

func RunDarwinWireGuardHelper(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: wghelper serve --socket /tmp/usbridge-wg-helper.sock [--log /tmp/usbridge-wg-helper.log]")
	}

	fs := flag.NewFlagSet("wghelper", flag.ContinueOnError)
	socketPath := fs.String("socket", "", "unix socket path")
	logPath := fs.String("log", "", "log file path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*socketPath) == "" {
		return fmt.Errorf("--socket is required")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("wghelper must run as root")
	}

	if strings.TrimSpace(*logPath) != "" {
		logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			logrus.SetOutput(logFile)
		}
	}

	if err := os.MkdirAll(filepath.Dir(*socketPath), 0777); err != nil {
		return err
	}
	_ = os.Remove(*socketPath)

	listener, err := net.Listen("unix", *socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", *socketPath, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(*socketPath)
	}()
	_ = os.Chmod(*socketPath, 0666)

	server := &darwinWireGuardHelperServer{}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go server.handleConn(conn)
	}
}

func (s *darwinWireGuardHelperServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var request darwinWireGuardHelperRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(darwinWireGuardHelperResponse{Error: fmt.Sprintf("invalid request: %v", err)})
		return
	}

	response, err := s.handleRequest(request)
	if err != nil {
		response = &darwinWireGuardHelperResponse{Error: err.Error()}
	}
	if response == nil {
		response = &darwinWireGuardHelperResponse{OK: true}
	}
	if !response.OK && response.Error == "" {
		response.Error = "unknown helper error"
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func (s *darwinWireGuardHelperServer) handleRequest(request darwinWireGuardHelperRequest) (*darwinWireGuardHelperResponse, error) {
	switch request.Command {
	case "ping":
		return &darwinWireGuardHelperResponse{OK: true}, nil
	case "up":
		return s.handleUp(request.Payload)
	case "down":
		if err := s.shutdown(); err != nil {
			return nil, err
		}
		return &darwinWireGuardHelperResponse{OK: true}, nil
	case "status":
		return s.handleStatus()
	default:
		return nil, fmt.Errorf("unsupported helper command %q", request.Command)
	}
}

func (s *darwinWireGuardHelperServer) handleStatus() (*darwinWireGuardHelperResponse, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.device == nil {
		return nil, fmt.Errorf("wireguard device is not running")
	}

	uapi, err := s.device.IpcGet()
	if err != nil {
		return nil, fmt.Errorf("failed to read wireguard ipc status: %w", err)
	}
	status, err := parseWireGuardIPCStatus(uapi, "", 0)
	if err != nil {
		return nil, err
	}

	response := &darwinWireGuardHelperResponse{
		OK:        true,
		RxBytes:   status.RxBytes,
		TxBytes:   status.TxBytes,
		PeerCount: status.PeerCount,
	}
	if !status.LastHandshakeAt.IsZero() {
		response.LastHandshakeSec = status.LastHandshakeAt.Unix()
		response.LastHandshakeNSec = int64(status.LastHandshakeAt.Nanosecond())
	}
	return response, nil
}

func (s *darwinWireGuardHelperServer) handleUp(payload *darwinWireGuardUpPayload) (*darwinWireGuardHelperResponse, error) {
	if payload == nil || payload.Bootstrap == nil {
		return nil, fmt.Errorf("missing WireGuard helper payload")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := s.shutdownLocked(); err != nil {
		return nil, err
	}

	mtu := payload.Bootstrap.MTU
	if mtu <= 0 {
		mtu = 1360
	}
	ifaceName := userspaceDefaultInterfaceName(firstNonEmpty(payload.IfaceName, payload.Bootstrap.InterfaceName))
	routeTargets := normalizeAllowedRoutes(payload.Bootstrap)

	tunDevice, err := wgtun.CreateTUN(ifaceName, mtu)
	if err != nil {
		return nil, fmt.Errorf("failed to create WireGuard TUN interface: %w", err)
	}
	realIfaceName, err := tunDevice.Name()
	if err == nil && strings.TrimSpace(realIfaceName) != "" {
		ifaceName = realIfaceName
	}

	bind := conn.NewDefaultBind()
	logger := &wgdevice.Logger{
		Verbosef: func(format string, args ...any) {
			logrus.Debugf("[WireGuard helper] "+format, args...)
		},
		Errorf: func(format string, args ...any) {
			logrus.Errorf("[WireGuard helper] "+format, args...)
		},
	}
	device := wgdevice.NewDevice(tunDevice, bind, logger)

	ipcConfig, err := wireGuardIPCConfig(payload.Bootstrap, payload.PrivateKey, payload.ListenPort)
	if err != nil {
		device.Close()
		tunDevice.Close()
		bind.Close()
		return nil, err
	}
	if err := device.IpcSet(ipcConfig); err != nil {
		device.Close()
		tunDevice.Close()
		bind.Close()
		return nil, fmt.Errorf("failed to configure userspace WireGuard device: %w", err)
	}
	if err := device.Up(); err != nil {
		device.Close()
		tunDevice.Close()
		bind.Close()
		return nil, fmt.Errorf("failed to start userspace WireGuard device: %w", err)
	}

	worker := &userspaceWireGuardService{ifaceName: ifaceName, routeTargets: routeTargets}
	if err := worker.configureInterface(payload.Bootstrap, mtu, routeTargets); err != nil {
		device.Close()
		tunDevice.Close()
		bind.Close()
		return nil, err
	}

	s.tunDevice = tunDevice
	s.bind = bind
	s.device = device
	s.ifaceName = ifaceName
	s.routes = routeTargets
	s.clientHost = payload.Bootstrap.ClientAddress
	s.serverHost = payload.Bootstrap.ServerAddress

	return &darwinWireGuardHelperResponse{
		OK:        true,
		IfaceName: ifaceName,
	}, nil
}

func (s *darwinWireGuardHelperServer) shutdown() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.shutdownLocked()
}

func (s *darwinWireGuardHelperServer) shutdownLocked() error {
	var errs []string

	if strings.TrimSpace(s.ifaceName) != "" {
		worker := &userspaceWireGuardService{ifaceName: s.ifaceName, routeTargets: append([]string(nil), s.routes...)}
		if err := worker.cleanupInterface(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if s.device != nil {
		s.device.Close()
		s.device = nil
	}
	if s.tunDevice != nil {
		if err := s.tunDevice.Close(); err != nil && err != os.ErrClosed {
			errs = append(errs, err.Error())
		}
		s.tunDevice = nil
	}
	if s.bind != nil {
		s.bind.Close()
		s.bind = nil
	}

	s.ifaceName = ""
	s.routes = nil
	s.clientHost = ""
	s.serverHost = ""

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}
