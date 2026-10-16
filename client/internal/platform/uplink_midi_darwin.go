//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework CoreMIDI -framework CoreFoundation
#include <CoreMIDI/CoreMIDI.h>
#include <CoreFoundation/CoreFoundation.h>
#include <dispatch/dispatch.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>

extern void usbridgeMIDIData(int handle, unsigned char *data, int length);

// One CoreMIDI client for macOS and iOS. It is made on the main thread
// when that can be arranged (its run loop is what keeps CoreMIDI's source
// list current as devices come and go), else where it is first needed.
static MIDIClientRef g_midi_client = 0;
static pthread_mutex_t g_midi_mu = PTHREAD_MUTEX_INITIALIZER;

static void midi_client_make(void *unused) {
    pthread_mutex_lock(&g_midi_mu);
    if (!g_midi_client) MIDIClientCreate(CFSTR("USBridge"), NULL, NULL, &g_midi_client);
    pthread_mutex_unlock(&g_midi_mu);
}

static void midi_client_prepare(void) {
    if (pthread_main_np()) midi_client_make(NULL);
    else dispatch_async_f(dispatch_get_main_queue(), NULL, midi_client_make);
}

static int midi_source_count(void) { return (int)MIDIGetNumberOfSources(); }

static int midi_source_id(int i) {
    SInt32 id = 0;
    MIDIObjectGetIntegerProperty(MIDIGetSource((ItemCount)i), kMIDIPropertyUniqueID, &id);
    return (int)id;
}

// midi_source_name: malloc'd UTF-8, the caller frees it.
static char *midi_source_name(int i) {
    CFStringRef name = NULL;
    MIDIEndpointRef src = MIDIGetSource((ItemCount)i);
    if (MIDIObjectGetStringProperty(src, kMIDIPropertyDisplayName, &name) != noErr || !name)
        MIDIObjectGetStringProperty(src, kMIDIPropertyName, &name);
    char *out = (char *)calloc(256, 1);
    if (name) {
        CFStringGetCString(name, out, 256, kCFStringEncodingUTF8);
        CFRelease(name);
    }
    return out;
}

static void midi_read(const MIDIPacketList *list, void *port_ref, void *src_ref) {
    int handle = (int)(intptr_t)port_ref;
    const MIDIPacket *p = &list->packet[0];
    for (UInt32 i = 0; i < list->numPackets; i++) {
        if (p->length) usbridgeMIDIData(handle, (unsigned char *)p->data, (int)p->length);
        p = MIDIPacketNext(p);
    }
}

// midi_open connects the source with this unique id to an input port of
// its own that reports to handle; 0 when it is gone.
static MIDIPortRef midi_open(int unique_id, int handle) {
    midi_client_make(NULL);
    MIDIObjectRef obj = 0;
    MIDIObjectType type;
    if (!g_midi_client || MIDIObjectFindByUniqueID(unique_id, &obj, &type) != noErr || type != kMIDIObjectType_Source)
        return 0;
    MIDIPortRef port = 0;
    if (MIDIInputPortCreate(g_midi_client, CFSTR("USBridge MIDI in"), midi_read, (void *)(intptr_t)handle, &port) != noErr)
        return 0;
    if (MIDIPortConnectSource(port, (MIDIEndpointRef)obj, NULL) != noErr) {
        MIDIPortDispose(port);
        return 0;
    }
    return port;
}
*/
import "C"

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"unsafe"
)

var midiClientOnce sync.Once

// ListMIDIInputs lists CoreMIDI's sources (USB, Bluetooth and network MIDI
// alike), named by CoreMIDI's unique id.
func ListMIDIInputs() []MIDIInputInfo {
	midiClientOnce.Do(func() { C.midi_client_prepare() })
	var out []MIDIInputInfo
	for i := 0; i < int(C.midi_source_count()); i++ {
		cname := C.midi_source_name(C.int(i))
		name := C.GoString(cname)
		C.free(unsafe.Pointer(cname))
		if name == "" {
			name = "MIDI " + strconv.Itoa(i+1)
		}
		if isGadgetMIDIName(name) {
			continue
		}
		out = append(out, MIDIInputInfo{ID: strconv.Itoa(int(C.midi_source_id(C.int(i)))), Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type coreMIDICapture struct {
	port   C.MIDIPortRef
	handle int
	once   sync.Once
}

func (c *coreMIDICapture) Stop() {
	c.once.Do(func() {
		C.MIDIPortDispose(c.port)
		unregisterMIDISink(c.handle)
	})
}

// StartMIDICapture passes what the source id (see ListMIDIInputs) plays to
// onData, from CoreMIDI's thread.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	uid, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("bad MIDI input id %q", id)
	}
	handle := registerMIDISink(onData)
	port := C.midi_open(C.int(uid), C.int(handle))
	if port == 0 {
		unregisterMIDISink(handle)
		return nil, fmt.Errorf("MIDI input %q is gone", id)
	}
	return &coreMIDICapture{port: port, handle: handle}, nil
}
