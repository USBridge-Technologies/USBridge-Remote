package platform

import (
	"bytes"
	"testing"
)

func TestMIDIShortMessage(t *testing.T) {
	for _, tc := range []struct {
		msg  uint32
		want []byte
	}{
		{0x007F3C90, []byte{0x90, 0x3C, 0x7F}}, // note on
		{0x000005C0, []byte{0xC0, 0x05}},       // program change
		{0x000040D0, []byte{0xD0, 0x40}},       // channel pressure
		{0x002000E0, []byte{0xE0, 0x00, 0x20}}, // pitch bend
		{0x000000F8, []byte{0xF8}},             // clock
		{0x000010F3, []byte{0xF3, 0x10}},       // song select
		{0x00000040, nil},                      // stray data byte
	} {
		if got := midiShortMessage(tc.msg); !bytes.Equal(got, tc.want) {
			t.Errorf("%08x: got % x, want % x", tc.msg, got, tc.want)
		}
	}
}

func TestDeliverMIDIChunksAndUnregister(t *testing.T) {
	var chunks [][]byte
	h := registerMIDISink(func(b []byte) { chunks = append(chunks, b) })
	deliverMIDI(h, make([]byte, 300))
	if len(chunks) != 3 || len(chunks[0]) != MIDIChunkMax || len(chunks[2]) != 300-2*MIDIChunkMax {
		t.Fatalf("chunks: %d", len(chunks))
	}
	unregisterMIDISink(h)
	deliverMIDI(h, []byte{0x90, 1, 2})
	if len(chunks) != 3 {
		t.Fatal("delivered after unregister")
	}
}

func TestUniqueMIDIInputs(t *testing.T) {
	got := uniqueMIDIInputs([]string{"Keystation", "USB MIDI", "Keystation"})
	if got[0].ID != "Keystation" || got[1].ID != "USB MIDI" || got[2].ID != "Keystation #2" {
		t.Fatalf("got %+v", got)
	}
}
