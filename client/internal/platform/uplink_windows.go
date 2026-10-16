//go:build windows

package platform

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MIDI inputs on Windows go through winmm's midiIn API, the ports every DAW
// sees (the Windows side of /dev/snd/midi*, uplink_linux.go). winmm calls
// back on its own thread: the callback only copies the bytes into a
// channel, and the goroutine behind it hands them on and gives SysEx
// buffers back to the driver (midiIn* calls aren't allowed inside the
// callback).

var (
	procMidiInGetNumDevs      = winmm.NewProc("midiInGetNumDevs")
	procMidiInGetDevCapsW     = winmm.NewProc("midiInGetDevCapsW")
	procMidiInOpen            = winmm.NewProc("midiInOpen")
	procMidiInClose           = winmm.NewProc("midiInClose")
	procMidiInStart           = winmm.NewProc("midiInStart")
	procMidiInStop            = winmm.NewProc("midiInStop")
	procMidiInReset           = winmm.NewProc("midiInReset")
	procMidiInPrepareHeader   = winmm.NewProc("midiInPrepareHeader")
	procMidiInUnprepareHeader = winmm.NewProc("midiInUnprepareHeader")
	procMidiInAddBuffer       = winmm.NewProc("midiInAddBuffer")
)

const (
	midiCallbackFunction = 0x00030000
	mimData              = 0x3C3
	mimLongData          = 0x3C4
	mimLongError         = 0x3C6
	midiSysExBufs        = 4
	midiSysExBufSize     = 1024
)

type midiInCaps struct {
	Mid           uint16
	Pid           uint16
	DriverVersion uint32
	Pname         [32]uint16
	Support       uint32
}

type midiHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Next          uintptr
	Reserved      uintptr
	Offset        uint32
	Reserved2     [8]uintptr
}

// midiInPorts is winmm's MIDI inputs in device-number order, each with the
// id ListMIDIInputs gives it.
func midiInPorts() (ids []string, names []string, devs []uintptr) {
	n, _, _ := procMidiInGetNumDevs.Call()
	seen := map[string]int{}
	for i := uintptr(0); i < n; i++ {
		var caps midiInCaps
		if r, _, _ := procMidiInGetDevCapsW.Call(i, uintptr(unsafe.Pointer(&caps)), unsafe.Sizeof(caps)); r != 0 {
			continue
		}
		name := strings.TrimSpace(windows.UTF16ToString(caps.Pname[:]))
		if name == "" {
			name = "MIDI " + strconv.Itoa(int(i))
		}
		k := seen[name]
		seen[name] = k + 1
		display := name
		if k > 0 {
			// One device, several ports (often named alike).
			display = fmt.Sprintf("%s %d", name, k+1)
		}
		ids = append(ids, fmt.Sprintf("%s#%d", name, k))
		names = append(names, display)
		devs = append(devs, i)
	}
	return
}

// ListMIDIInputs lists winmm's MIDI inputs -- USB MIDI controllers,
// keyboards, interfaces. Device numbers shift as ports come and go, so a
// port's id is its name plus which of the ports by that name it is.
// A NanoKVM's own MIDI port (its USB gadget's f_midi, "MIDI function") is
// left out, as on Linux: when the target PC is this machine, forwarding it
// would feed the host's output straight back into it.
func ListMIDIInputs() []MIDIInputInfo {
	ids, names, _ := midiInPorts()
	var out []MIDIInputInfo
	for i := range ids {
		if strings.Contains(names[i], "MIDI function") {
			continue
		}
		out = append(out, MIDIInputInfo{ID: ids[i], Name: names[i]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func midiInputDevice(id string) (uintptr, error) {
	ids, _, devs := midiInPorts()
	for i := range ids {
		if ids[i] == id {
			return devs[i], nil
		}
	}
	return 0, fmt.Errorf("MIDI input %q is gone", id)
}

// midiEvent is what the winmm callback passes on: a short message's bytes,
// and/or a SysEx buffer the driver is done with.
type midiEvent struct {
	data []byte
	hdr  *midiHdr
}

type midiCapture struct {
	handle uintptr
	inst   uintptr
	events chan midiEvent
	hdrs   []*midiHdr
	bufs   [][]byte
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex // handle use vs Stop
	closed bool
}

var (
	midiCapturesMu sync.Mutex
	midiCaptures   = map[uintptr]*midiCapture{}
	midiNextInst   uintptr
	// One callback for every port: syscall.NewCallback slots are limited.
	midiCallback = syscall.NewCallback(midiInProc)
)

// shortMessageLen is how many of a short message's (up to 3) bytes count.
// winmm hands every short message over with its status byte.
func shortMessageLen(status byte) int {
	switch {
	case status >= 0xC0 && status <= 0xDF, status == 0xF1, status == 0xF3:
		return 2
	case status >= 0xF4:
		return 1
	default:
		return 3
	}
}

func midiInProc(hmi, msg, instance, param1, param2 uintptr) uintptr {
	midiCapturesMu.Lock()
	c := midiCaptures[instance]
	midiCapturesMu.Unlock()
	if c == nil {
		return 0
	}
	var ev midiEvent
	switch msg {
	case mimData:
		b := []byte{byte(param1), byte(param1 >> 8), byte(param1 >> 16)}
		if b[0] < 0x80 {
			return 0
		}
		ev.data = b[:shortMessageLen(b[0])]
	case mimLongData, mimLongError:
		// param1 is one of c.hdrs; find it rather than convert the uintptr.
		for i, h := range c.hdrs {
			if uintptr(unsafe.Pointer(h)) == param1 {
				if n := min(int(h.BytesRecorded), len(c.bufs[i])); n > 0 && msg == mimLongData {
					ev.data = append([]byte(nil), c.bufs[i][:n]...)
				}
				ev.hdr = h
				break
			}
		}
		if ev.hdr == nil {
			return 0
		}
	default:
		return 0
	}
	select {
	case c.events <- ev:
	default: // the reader is far behind: drop rather than block winmm
	}
	return 0
}

func (c *midiCapture) Stop() {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		procMidiInStop.Call(c.handle)
		procMidiInReset.Call(c.handle) // hands the SysEx buffers back
		for _, h := range c.hdrs {
			procMidiInUnprepareHeader.Call(c.handle, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		}
		procMidiInClose.Call(c.handle)
		c.mu.Unlock()
		midiCapturesMu.Lock()
		delete(midiCaptures, c.inst)
		midiCapturesMu.Unlock()
		close(c.done)
	})
}

// StartMIDICapture reads the MIDI input id (see ListMIDIInputs) and hands
// what it plays to onData, in chunks of at most 128 bytes, from its own
// goroutine. Short messages and SysEx are passed on in the order they come;
// the host plays them back as one ordered byte stream.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	dev, err := midiInputDevice(id)
	if err != nil {
		return nil, err
	}
	c := &midiCapture{events: make(chan midiEvent, 1024), done: make(chan struct{})}
	for i := 0; i < midiSysExBufs; i++ {
		buf := make([]byte, midiSysExBufSize)
		c.bufs = append(c.bufs, buf)
		c.hdrs = append(c.hdrs, &midiHdr{Data: uintptr(unsafe.Pointer(&buf[0])), BufferLength: midiSysExBufSize})
	}
	midiCapturesMu.Lock()
	midiNextInst++
	c.inst = midiNextInst
	midiCaptures[c.inst] = c
	midiCapturesMu.Unlock()

	if r, _, _ := procMidiInOpen.Call(uintptr(unsafe.Pointer(&c.handle)), dev, midiCallback, c.inst, midiCallbackFunction); r != 0 {
		midiCapturesMu.Lock()
		delete(midiCaptures, c.inst)
		midiCapturesMu.Unlock()
		return nil, fmt.Errorf("midiInOpen(%s): MMRESULT %d (is another app holding the port?)", id, r)
	}
	for _, h := range c.hdrs {
		procMidiInPrepareHeader.Call(c.handle, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		procMidiInAddBuffer.Call(c.handle, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
	}
	if r, _, _ := procMidiInStart.Call(c.handle); r != 0 {
		c.Stop()
		return nil, fmt.Errorf("midiInStart(%s): MMRESULT %d", id, r)
	}
	go func() {
		for {
			select {
			case <-c.done:
				return
			case ev := <-c.events:
				for d := ev.data; len(d) > 0; {
					n := min(len(d), 128)
					onData(d[:n])
					d = d[n:]
				}
				if ev.hdr != nil {
					c.mu.Lock()
					if !c.closed {
						ev.hdr.BytesRecorded = 0
						procMidiInAddBuffer.Call(c.handle, uintptr(unsafe.Pointer(ev.hdr)), unsafe.Sizeof(*ev.hdr))
					}
					c.mu.Unlock()
				}
			}
		}
	}()
	return c, nil
}
