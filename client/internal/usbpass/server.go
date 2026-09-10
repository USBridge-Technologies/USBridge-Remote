package usbpass

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	usbipVersion   = 0x0111
	opReqDevlist   = 0x8005
	opRepDevlist   = 0x0005
	opReqImport    = 0x8003
	opRepImport    = 0x0003
	cmdSubmit      = 0x00000001
	cmdUnlink      = 0x00000002
	retSubmit      = 0x00000003
	retUnlink      = 0x00000004
	dirOut         = 0
	dirIn          = 1
	errnoEPIPE     = -32
	errnoECONNRESET = -104
)

// ExportedDevice is one device advertised on the USB/IP wire.
type ExportedDevice struct {
	BusID      string
	Path       string
	Busnum     uint32
	Devnum     uint32
	Speed      uint32
	VID        uint16
	PID        uint16
	BCDDevice  uint16
	Class      uint8
	SubClass   uint8
	Protocol   uint8
	ConfigVal  uint8
	NumConfigs uint8
	Interfaces [][3]uint8 // class, subclass, protocol
	DeviceDesc []byte
	ConfigDesc []byte
	Backend    DeviceBackend
}

// DeviceBackend answers URBs for an imported device.
type DeviceBackend interface {
	HandleControl(setup [8]byte, wLength int) (status int32, data []byte)
	HandleBulk(ep uint8, dirIn bool, length int, outData []byte) (status int32, data []byte)
	Close() error
}

// Server is an in-process USB/IP v1.1.1 export listener.
type Server struct {
	mu       sync.Mutex
	ln       net.Listener
	devices  []*ExportedDevice
	closing  bool
	wg       sync.WaitGroup
}

// StartExport binds addr (e.g. "0.0.0.0:3240") and serves devices.
func StartExport(addr string, devices []*ExportedDevice) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, devices: devices}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop()
	}()
	logrus.Infof("usbpass: USB/IP export listen %s (%d device(s))", addr, len(devices))
	return s, nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	s.closing = true
	ln := s.ln
	devs := append([]*ExportedDevice(nil), s.devices...)
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, d := range devs {
		if d.Backend != nil {
			_ = d.Backend.Close()
		}
	}
	s.wg.Wait()
}

// Addr returns the bound listen address.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closing := s.closing
			s.mu.Unlock()
			if closing {
				return
			}
			logrus.Debugf("usbpass: accept: %v", err)
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(2 * time.Minute))
			if err := s.handleConn(c); err != nil && err != io.EOF {
				logrus.Debugf("usbpass: conn: %v", err)
			}
		}(conn)
	}
}

func (s *Server) handleConn(c net.Conn) error {
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	code := binary.BigEndian.Uint16(hdr[2:4])
	switch code {
	case opReqDevlist:
		s.mu.Lock()
		devs := append([]*ExportedDevice(nil), s.devices...)
		s.mu.Unlock()
		_, err := c.Write(packRepDevlist(devs))
		return err
	case opReqImport:
		bus := make([]byte, 32)
		if _, err := io.ReadFull(c, bus); err != nil {
			return err
		}
		busID := cstr(bus)
		s.mu.Lock()
		var found *ExportedDevice
		for _, d := range s.devices {
			if d.BusID == busID {
				found = d
				break
			}
		}
		s.mu.Unlock()
		if _, err := c.Write(packRepImport(found)); err != nil {
			return err
		}
		if found == nil {
			return fmt.Errorf("unknown busid %q", busID)
		}
		_ = c.SetDeadline(time.Time{}) // URB session can be long
		return s.serveURBs(c, found)
	default:
		return fmt.Errorf("unknown usbip op %#04x", code)
	}
}

func (s *Server) serveURBs(c net.Conn, dev *ExportedDevice) error {
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := c.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		for {
			frame, consumed, perr := parseCmd(buf)
			if perr == errNeedMore {
				break
			}
			if perr != nil {
				return perr
			}
			buf = buf[consumed:]
			resp, err := dispatchURB(dev, frame)
			if err != nil {
				return err
			}
			if _, err := c.Write(resp); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
}

type urbFrame struct {
	cmd, seq, devid, direction, ep uint32
	unlinkSeq                      uint32
	transferFlags                  uint32
	transferLen                    int32
	startFrame                     int32
	numPackets                     int32
	interval                       int32
	setup                          [8]byte
	data                           []byte
}

var errNeedMore = fmt.Errorf("need more")

func parseCmd(buf []byte) (urbFrame, int, error) {
	if len(buf) < 48 {
		return urbFrame{}, 0, errNeedMore
	}
	var f urbFrame
	f.cmd = binary.BigEndian.Uint32(buf[0:4])
	f.seq = binary.BigEndian.Uint32(buf[4:8])
	f.devid = binary.BigEndian.Uint32(buf[8:12])
	f.direction = binary.BigEndian.Uint32(buf[12:16])
	f.ep = binary.BigEndian.Uint32(buf[16:20])
	switch f.cmd {
	case cmdSubmit:
		f.transferFlags = binary.BigEndian.Uint32(buf[20:24])
		f.transferLen = int32(binary.BigEndian.Uint32(buf[24:28]))
		f.startFrame = int32(binary.BigEndian.Uint32(buf[28:32]))
		f.numPackets = int32(binary.BigEndian.Uint32(buf[32:36]))
		f.interval = int32(binary.BigEndian.Uint32(buf[36:40]))
		copy(f.setup[:], buf[40:48])
		need := 48
		if f.direction == dirOut && f.transferLen > 0 {
			need += int(f.transferLen)
		}
		isoN := f.numPackets
		if isoN < 0 {
			isoN = 0
		}
		need += int(isoN) * 16
		if len(buf) < need {
			return urbFrame{}, 0, errNeedMore
		}
		if f.direction == dirOut && f.transferLen > 0 {
			f.data = append([]byte(nil), buf[48:48+int(f.transferLen)]...)
		}
		return f, need, nil
	case cmdUnlink:
		f.unlinkSeq = binary.BigEndian.Uint32(buf[20:24])
		return f, 48, nil
	default:
		return urbFrame{}, 0, fmt.Errorf("bad urb cmd %#x", f.cmd)
	}
}

func dispatchURB(dev *ExportedDevice, f urbFrame) ([]byte, error) {
	switch f.cmd {
	case cmdUnlink:
		return packRetUnlink(f.seq, errnoECONNRESET), nil
	case cmdSubmit:
		ep := uint8(f.ep)
		var status int32
		var data []byte
		if ep&0x7f == 0 {
			wLen := int(binary.LittleEndian.Uint16(f.setup[6:8]))
			if f.transferLen > 0 && int(f.transferLen) < wLen {
				wLen = int(f.transferLen)
			}
			status, data = dev.Backend.HandleControl(f.setup, wLen)
		} else {
			status, data = dev.Backend.HandleBulk(ep, f.direction == dirIn, int(f.transferLen), f.data)
		}
		return packRetSubmit(f.seq, status, data, f.numPackets), nil
	default:
		return nil, fmt.Errorf("unsupported cmd")
	}
}

func packRepDevlist(devs []*ExportedDevice) []byte {
	out := make([]byte, 0, 64+len(devs)*400)
	out = appendU16(out, usbipVersion)
	out = appendU16(out, opRepDevlist)
	out = appendU32(out, 0)
	out = appendU32(out, uint32(len(devs)))
	for _, d := range devs {
		out = appendDeviceBody(out, d, true)
	}
	return out
}

func packRepImport(d *ExportedDevice) []byte {
	out := make([]byte, 0, 400)
	out = appendU16(out, usbipVersion)
	out = appendU16(out, opRepImport)
	if d == nil {
		return appendU32(out, 1)
	}
	out = appendU32(out, 0)
	return appendDeviceBody(out, d, false)
}

func appendDeviceBody(out []byte, d *ExportedDevice, withIfaces bool) []byte {
	out = appendCstr(out, d.Path, 256)
	out = appendCstr(out, d.BusID, 32)
	out = appendU32(out, d.Busnum)
	out = appendU32(out, d.Devnum)
	out = appendU32(out, d.Speed)
	out = appendU16(out, d.VID)
	out = appendU16(out, d.PID)
	out = appendU16(out, d.BCDDevice)
	out = append(out, d.Class, d.SubClass, d.Protocol, d.ConfigVal, d.NumConfigs, byte(len(d.Interfaces)))
	if withIfaces {
		for _, iface := range d.Interfaces {
			out = append(out, iface[0], iface[1], iface[2], 0)
		}
	}
	return out
}

func packRetSubmit(seq uint32, status int32, data []byte, numPackets int32) []byte {
	out := make([]byte, 0, 48+len(data))
	out = appendU32(out, retSubmit)
	out = appendU32(out, seq)
	out = appendU32(out, 0) // devid
	out = appendU32(out, 0) // direction
	out = appendU32(out, 0) // ep
	out = appendI32(out, status)
	out = appendI32(out, int32(len(data)))
	out = appendI32(out, 0) // start_frame
	if numPackets < 0 {
		out = appendI32(out, -1)
	} else {
		out = appendI32(out, numPackets)
	}
	out = appendI32(out, 0) // error_count
	out = append(out, make([]byte, 8)...)
	out = append(out, data...)
	return out
}

func packRetUnlink(seq uint32, status int32) []byte {
	out := make([]byte, 0, 48)
	out = appendU32(out, retUnlink)
	out = appendU32(out, seq)
	out = appendU32(out, 0)
	out = appendU32(out, 0)
	out = appendU32(out, 0)
	out = appendI32(out, status)
	out = append(out, make([]byte, 24)...)
	return out
}

func appendU16(b []byte, v uint16) []byte {
	var tmp [2]byte
	binary.BigEndian.PutUint16(tmp[:], v)
	return append(b, tmp[:]...)
}
func appendU32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.BigEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}
func appendI32(b []byte, v int32) []byte {
	return appendU32(b, uint32(v))
}
func appendCstr(b []byte, s string, n int) []byte {
	raw := []byte(s)
	if len(raw) > n-1 {
		raw = raw[:n-1]
	}
	b = append(b, raw...)
	for i := len(raw); i < n; i++ {
		b = append(b, 0)
	}
	return b
}
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
