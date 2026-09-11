package usbpass

import (
	"context"
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

// DeviceBackend answers URBs for an imported device. ctx is per-URB: it is
// cancelled the moment a CMD_UNLINK for that same URB arrives (see
// serveURBs), so an implementation whose transfer takes a context (like
// gousb's ReadContext/WriteContext) must actually use it — that's what lets
// a hung transfer be aborted instead of blocking the whole connection until
// its own internal timeout, which is what left CMD_UNLINK unprocessable and
// made Windows reset the port after a slow/stuck transfer (see serveURBs).
type DeviceBackend interface {
	HandleControl(ctx context.Context, setup [8]byte, wLength int) (status int32, data []byte)
	HandleBulk(ctx context.Context, ep uint8, dirIn bool, length int, outData []byte) (status int32, data []byte)
	Close() error
}

// Server is an in-process USB/IP v1.1.1 export listener.
type Server struct {
	mu       sync.Mutex
	ln       net.Listener
	devices  []*ExportedDevice
	conns    []net.Conn
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
	conns := append([]net.Conn(nil), s.conns...)
	s.conns = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.Close()
	}
	for _, d := range devs {
		if d.Backend != nil {
			_ = d.Backend.Close()
		}
	}
	s.wg.Wait()
}

// Devices returns the exported devices, so a terminal harness can talk to a
// live backend directly and tell a broken libusb claim apart from a broken
// USB/IP conversation.
func (s *Server) Devices() []*ExportedDevice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*ExportedDevice(nil), s.devices...)
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
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer func() {
				_ = c.Close()
				s.mu.Lock()
				for i, x := range s.conns {
					if x == c {
						s.conns = append(s.conns[:i], s.conns[i+1:]...)
						break
					}
				}
				s.mu.Unlock()
			}()
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

// serveURBs reads CMD_SUBMIT/CMD_UNLINK frames off c and answers them.
//
// Each CMD_SUBMIT is dispatched in its own goroutine rather than inline: a
// real USB/IP client (confirmed live against Windows' usbip-win2 VHCI)
// pipelines URBs — it does not wait for one RET_SUBMIT before sending the
// next CMD_SUBMIT or a CMD_UNLINK to cancel one that's taking too long. The
// previous inline version blocked this whole read loop for the duration of
// one URB (up to HandleBulk's own internal timeout), during which a
// CMD_UNLINK for that exact URB was sitting unread in the socket buffer —
// Windows got no RET_UNLINK, decided the port was unresponsive, and reset
// it. Tracking in-flight URBs by seq and actually cancelling the matching
// context on UNLINK is what lets a stuck transfer be aborted immediately
// instead of stalling the connection.
func (s *Server) serveURBs(c net.Conn, dev *ExportedDevice) error {
	connCtx, cancelConn := context.WithCancel(context.Background())
	defer cancelConn()

	var writeMu sync.Mutex
	writeFrame := func(b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_, err := c.Write(b)
		return err
	}

	var inflightMu sync.Mutex
	inflight := make(map[uint32]context.CancelFunc)

	var wg sync.WaitGroup
	defer func() {
		cancelConn() // aborts every still-running HandleBulk/HandleControl
		wg.Wait()
	}()

	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	var loopErr error
	for loopErr == nil {
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
				loopErr = perr
				break
			}
			buf = buf[consumed:]

			if frame.cmd == cmdUnlink {
				inflightMu.Lock()
				if cancel, ok := inflight[frame.unlinkSeq]; ok {
					cancel()
					delete(inflight, frame.unlinkSeq)
				}
				inflightMu.Unlock()
				if werr := writeFrame(packRetUnlink(frame.seq, errnoECONNRESET)); werr != nil {
					loopErr = werr
					break
				}
				continue
			}

			ctx, cancel := context.WithCancel(connCtx)
			inflightMu.Lock()
			inflight[frame.seq] = cancel
			inflightMu.Unlock()

			wg.Add(1)
			go func(f urbFrame) {
				defer wg.Done()
				resp := dispatchURB(ctx, dev, f)
				inflightMu.Lock()
				_, stillPending := inflight[f.seq]
				delete(inflight, f.seq)
				inflightMu.Unlock()
				cancel()
				if !stillPending {
					// Already unlinked; RET_UNLINK was already sent for
					// this seq and Windows has moved on — skip the
					// now-meaningless second reply.
					return
				}
				if werr := writeFrame(resp); werr != nil {
					logrus.Debugf("usbpass: conn: write RET_SUBMIT seq=%d: %v", f.seq, werr)
				}
			}(frame)
		}
		if loopErr == nil && err != nil {
			loopErr = err
		}
	}
	return loopErr
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

// dispatchURB handles exactly one CMD_SUBMIT (cmdUnlink is handled directly
// in serveURBs, since it needs to cancel a different in-flight call rather
// than answer one of its own).
func dispatchURB(ctx context.Context, dev *ExportedDevice, f urbFrame) []byte {
	ep := uint8(f.ep)
	var status int32
	var data []byte
	if ep&0x7f == 0 {
		wLen := int(binary.LittleEndian.Uint16(f.setup[6:8]))
		if f.transferLen > 0 && int(f.transferLen) < wLen {
			wLen = int(f.transferLen)
		}
		status, data = dev.Backend.HandleControl(ctx, f.setup, wLen)
	} else {
		status, data = dev.Backend.HandleBulk(ctx, ep, f.direction == dirIn, int(f.transferLen), f.data)
	}
	return packRetSubmit(f.seq, status, data, f.numPackets)
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
