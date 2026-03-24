//go:build windows

package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/conn"
	wgdevice "golang.zx2c4.com/wireguard/device"
	wgtun "golang.zx2c4.com/wireguard/tun"
)

var (
	windowsWireGuardHelperStateMu sync.Mutex
	windowsWireGuardHelperState   *windowsWireGuardHelperClient
)

type windowsWireGuardHelperClient struct {
	addr      string
	token     string
	logPath   string
	parentPID int
}

type windowsWireGuardHelperServer struct {
	mutex      sync.Mutex
	listener   net.Listener
	token      string
	tunDevice  wgtun.Device
	bind       conn.Bind
	device     *wgdevice.Device
	ifaceName  string
	routes     []string
	clientHost string
	serverHost string
}

func platformUsesWireGuardHelper() bool {
	return true
}

func (s *userspaceWireGuardService) connectWithElevatedWireGuardHelper(resp *models.WireGuardBootstrapResponse, mtu int) error {
	client, err := newWindowsWireGuardHelperClient()
	if err != nil {
		return err
	}

	response, err := client.up(&windowsWireGuardHelperUpPayload{
		Bootstrap:  resp,
		PrivateKey: s.privateKey,
		ListenPort: s.config.WireGuardListenPort,
		IfaceName:  s.ifaceName,
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(response.IfaceName) != "" {
		s.ifaceName = response.IfaceName
	}
	s.serverHost = resp.ServerAddress
	s.clientHost = resp.ClientAddress
	s.running = true

	logrus.Infof("🔐 [WireGuard windows] elevated helper backend started iface=%s client=%s server=%s", s.ifaceName, s.clientHost, s.serverHost)
	_ = mtu
	return nil
}

func (s *userspaceWireGuardService) disconnectWithElevatedWireGuardHelper() error {
	client, err := newWindowsWireGuardHelperClient()
	if err != nil {
		return err
	}
	return client.down()
}

func newWindowsWireGuardHelperClient() (*windowsWireGuardHelperClient, error) {
	windowsWireGuardHelperStateMu.Lock()
	defer windowsWireGuardHelperStateMu.Unlock()

	if windowsWireGuardHelperState != nil {
		return windowsWireGuardHelperState, nil
	}

	addr, err := reserveLoopbackAddress()
	if err != nil {
		return nil, err
	}
	token, err := randomHelperToken()
	if err != nil {
		return nil, err
	}

	windowsWireGuardHelperState = &windowsWireGuardHelperClient{
		addr:      addr,
		token:     token,
		logPath:   filepath.Join(os.TempDir(), "usbridge-wg-helper.log"),
		parentPID: os.Getpid(),
	}
	return windowsWireGuardHelperState, nil
}

func (c *windowsWireGuardHelperClient) up(payload *windowsWireGuardHelperUpPayload) (*windowsWireGuardHelperResponse, error) {
	if err := c.ensureRunning(); err != nil {
		return nil, err
	}
	return c.call(windowsWireGuardHelperRequest{
		Command: "up",
		Payload: payload,
	})
}

func (c *windowsWireGuardHelperClient) down() error {
	if err := c.ensureRunning(); err != nil {
		return err
	}
	_, err := c.call(windowsWireGuardHelperRequest{Command: "down"})
	return err
}

func (c *windowsWireGuardHelperClient) ensureRunning() error {
	if _, err := c.call(windowsWireGuardHelperRequest{Command: "ping"}); err == nil {
		return nil
	}
	if err := c.launch(); err != nil {
		return err
	}
	for i := 0; i < 30; i++ {
		time.Sleep(250 * time.Millisecond)
		if _, err := c.call(windowsWireGuardHelperRequest{Command: "ping"}); err == nil {
			return nil
		}
	}
	return fmt.Errorf("WireGuard helper did not start in time; check %s", c.logPath)
}

func (c *windowsWireGuardHelperClient) launch() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable: %w", err)
	}

	args := []string{
		"wg-helper",
		"serve",
		"--addr", c.addr,
		"--token", c.token,
		"--parent-pid", strconv.Itoa(c.parentPID),
		"--log", c.logPath,
	}

	if err := shellExecuteRunAs(exePath, args); err != nil {
		return err
	}
	return nil
}

func reserveLoopbackAddress() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to reserve loopback address for WireGuard helper: %w", err)
	}
	defer listener.Close()
	return listener.Addr().String(), nil
}

func randomHelperToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to create helper token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func shellExecuteRunAs(exePath string, args []string) error {
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteW := shell32.NewProc("ShellExecuteW")

	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	filePtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return err
	}

	quotedArgs := make([]string, 0, len(args))
	for _, arg := range args {
		quotedArgs = append(quotedArgs, syscall.EscapeArg(arg))
	}
	paramsPtr, err := windows.UTF16PtrFromString(strings.Join(quotedArgs, " "))
	if err != nil {
		return err
	}

	ret, _, callErr := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		0,
		uintptr(windows.SW_HIDE),
	)
	if ret <= 32 {
		if ret == 5 {
			return fmt.Errorf("administrator privileges were not granted")
		}
		if callErr != syscall.Errno(0) {
			return fmt.Errorf("failed to start elevated WireGuard helper: %w", callErr)
		}
		return fmt.Errorf("failed to start elevated WireGuard helper: ShellExecuteW returned %d", ret)
	}
	return nil
}

func RunWindowsWireGuardHelper(args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return fmt.Errorf("usage: wg-helper serve --addr 127.0.0.1:38745 --token secret [--parent-pid 1234] [--log helper.log]")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("wg-helper must run elevated")
	}

	fs := flag.NewFlagSet("wg-helper", flag.ContinueOnError)
	addr := fs.String("addr", "", "loopback listen address")
	token := fs.String("token", "", "shared secret")
	logPath := fs.String("log", "", "log file path")
	parentPID := fs.Int("parent-pid", 0, "parent process id")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*addr) == "" {
		return fmt.Errorf("--addr is required")
	}
	if strings.TrimSpace(*token) == "" {
		return fmt.Errorf("--token is required")
	}

	if strings.TrimSpace(*logPath) != "" {
		if err := os.MkdirAll(filepath.Dir(*logPath), 0700); err == nil {
			logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err == nil {
				logrus.SetOutput(logFile)
			}
		}
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", *addr, err)
	}
	server := &windowsWireGuardHelperServer{
		listener: listener,
		token:    *token,
	}
	defer func() {
		_ = server.shutdown()
		_ = listener.Close()
	}()

	if *parentPID > 0 {
		go server.watchParent(*parentPID)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errorsIsNetClosed(err) {
				return nil
			}
			return err
		}
		go server.handleConn(conn)
	}
}

func (s *windowsWireGuardHelperServer) watchParent(parentPID int) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(parentPID))
	if err != nil {
		return
	}
	defer windows.CloseHandle(handle)

	_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
	_ = s.shutdown()
	if s.listener != nil {
		_ = s.listener.Close()
	}
}

func (s *windowsWireGuardHelperServer) handleConn(conn net.Conn) {
	defer conn.Close()

	var request windowsWireGuardHelperRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		_ = json.NewEncoder(conn).Encode(windowsWireGuardHelperResponse{Error: fmt.Sprintf("invalid request: %v", err)})
		return
	}
	if subtleConstantTimeCompare(request.Token, s.token) == 0 {
		_ = json.NewEncoder(conn).Encode(windowsWireGuardHelperResponse{Error: "unauthorized helper request"})
		return
	}

	response, err := s.handleRequest(&request)
	if err != nil {
		response = &windowsWireGuardHelperResponse{Error: err.Error()}
	}
	if response == nil {
		response = &windowsWireGuardHelperResponse{OK: true}
	}
	if !response.OK && response.Error == "" {
		response.Error = "unknown helper error"
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func (s *windowsWireGuardHelperServer) handleRequest(request *windowsWireGuardHelperRequest) (*windowsWireGuardHelperResponse, error) {
	switch request.Command {
	case "ping":
		return &windowsWireGuardHelperResponse{OK: true}, nil
	case "up":
		return s.handleUp(request.Payload)
	case "down":
		if err := s.shutdown(); err != nil {
			return nil, err
		}
		return &windowsWireGuardHelperResponse{OK: true}, nil
	default:
		return nil, fmt.Errorf("unsupported helper command %q", request.Command)
	}
}

func (s *windowsWireGuardHelperServer) handleUp(payload *windowsWireGuardHelperUpPayload) (*windowsWireGuardHelperResponse, error) {
	if payload == nil || payload.Bootstrap == nil {
		return nil, fmt.Errorf("missing WireGuard helper payload")
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := s.shutdownLocked(); err != nil {
		return nil, err
	}
	if err := ensureWireGuardRuntimeAvailable(); err != nil {
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

	return &windowsWireGuardHelperResponse{
		OK:        true,
		IfaceName: ifaceName,
	}, nil
}

func (s *windowsWireGuardHelperServer) shutdown() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.shutdownLocked()
}

func (s *windowsWireGuardHelperServer) shutdownLocked() error {
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
		return fmt.Errorf(strings.Join(errs, "; "))
	}
	return nil
}

func subtleConstantTimeCompare(left, right string) int {
	if len(left) != len(right) {
		return 0
	}
	var diff byte
	for i := 0; i < len(left); i++ {
		diff |= left[i] ^ right[i]
	}
	if diff == 0 {
		return 1
	}
	return 0
}

func errorsIsNetClosed(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "use of closed network connection")
}
