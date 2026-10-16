package platform

import (
	"fmt"
	"strings"
	"sync"
)

// The MIDI half shared by the platforms whose MIDI API calls back with
// what a device plays (WinMM on Windows, CoreMIDI on macOS and iOS,
// android.media.midi on Android): a capture registers its onData as a sink
// under a handle, the platform callback hands bytes or a packed short
// message to that handle, and they come out of onData in chunks of at most
// MIDIChunkMax bytes. Linux reads the raw device instead (uplink_linux.go).

// MIDIChunkMax is the most bytes one MIDI uplink message carries.
const MIDIChunkMax = 128

var (
	midiSinksMu sync.Mutex
	midiSinks   = map[int]func([]byte){}
	midiNext    int
)

// registerMIDISink returns the handle the platform callback uses for onData.
func registerMIDISink(onData func([]byte)) int {
	midiSinksMu.Lock()
	defer midiSinksMu.Unlock()
	midiNext++
	midiSinks[midiNext] = onData
	return midiNext
}

// unregisterMIDISink: after it returns, the handle's onData is not called
// any more (bytes still arriving for it are dropped).
func unregisterMIDISink(handle int) {
	midiSinksMu.Lock()
	delete(midiSinks, handle)
	midiSinksMu.Unlock()
}

// deliverMIDI passes data (copied) to the handle's sink, MIDIChunkMax bytes
// at a time. The lock is held during the call, so unregisterMIDISink waits
// for a delivery in flight.
func deliverMIDI(handle int, data []byte) {
	if len(data) == 0 {
		return
	}
	midiSinksMu.Lock()
	defer midiSinksMu.Unlock()
	sink := midiSinks[handle]
	if sink == nil {
		return
	}
	for len(data) > 0 {
		n := min(len(data), MIDIChunkMax)
		sink(append([]byte(nil), data[:n]...))
		data = data[n:]
	}
}

// midiShortMessage unpacks a short MIDI message packed into a little-endian
// word, status byte first (WinMM's MIM_DATA): only as many bytes as the
// status takes. nil for a stray data byte.
func midiShortMessage(msg uint32) []byte {
	b := []byte{byte(msg), byte(msg >> 8), byte(msg >> 16)}
	n := midiMessageLen(b[0])
	if n == 0 {
		return nil
	}
	return b[:n]
}

// midiMessageLen is the length of a short message with this status byte
// (0: not a status byte, or SysEx, which comes as a stream).
func midiMessageLen(status byte) int {
	switch {
	case status < 0x80:
		return 0
	case status < 0xC0, status >= 0xE0 && status < 0xF0:
		return 3
	case status < 0xE0:
		return 2
	}
	switch status {
	case 0xF1, 0xF3:
		return 2
	case 0xF2:
		return 3
	case 0xF0, 0xF7:
		return 0
	}
	return 1
}

// isGadgetMIDIName: an input that is a Linux USB gadget's MIDI function
// (a NanoKVM's own port, its interface string): when the target PC is this
// machine, forwarding it would feed the host's output straight back into it.
// Linux checks the interface itself (isGadgetMIDICard).
func isGadgetMIDIName(name string) bool {
	return strings.Contains(name, "MIDI function")
}

// uniqueMIDIInputs names inputs by their names, with " #2", " #3" on the
// second and later ones alike: an id that stays the same while the device
// stays plugged in, where the platform has no better one (WinMM numbers
// devices by position).
func uniqueMIDIInputs(names []string) []MIDIInputInfo {
	seen := map[string]int{}
	out := make([]MIDIInputInfo, 0, len(names))
	for _, name := range names {
		seen[name]++
		id := name
		if n := seen[name]; n > 1 {
			id = fmt.Sprintf("%s #%d", name, n)
		}
		out = append(out, MIDIInputInfo{ID: id, Name: id})
	}
	return out
}
