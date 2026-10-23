//go:build windows && cgo

package platform

/*
#cgo LDFLAGS: -lole32
#define COBJMACROS
#include <windows.h>
#include <mmreg.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <propsys.h>
#include <string.h>

#ifndef SPEAKER_FRONT_LEFT
#define SPEAKER_FRONT_LEFT 0x1
#define SPEAKER_FRONT_RIGHT 0x2
#define SPEAKER_BACK_LEFT 0x10
#define SPEAKER_BACK_RIGHT 0x20
#endif
#ifndef AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM
#define AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM 0x80000000
#endif
#ifndef AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
#define AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY 0x08000000
#endif

// Own copies, so nothing depends on which IIDs libuuid carries.
static const GUID kPadCLSID_MMDeviceEnumerator = {0xBCDE0395, 0xE52F, 0x467C, {0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}};
static const GUID kPadIID_IMMDeviceEnumerator = {0xA95664D2, 0x9614, 0x4F35, {0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}};
static const GUID kPadIID_IAudioClient = {0x1CB9AD4C, 0xDBFA, 0x4C32, {0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}};
static const GUID kPadIID_IAudioRenderClient = {0xF294ACFC, 0x3146, 0x4483, {0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}};
static const GUID kPadSubtypePCM = {0x00000001, 0x0000, 0x0010, {0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}};
// PKEY_Device_ContainerId: the physical device an endpoint belongs to.
static const PROPERTYKEY kPadContainerId = {{0x8C7ED206, 0x3F8A, 0x4827, {0xB3, 0xAB, 0xAE, 0x9E, 0x1F, 0xAE, 0xFC, 0x6C}}, 2};

typedef struct {
    IAudioClient *client;
    IAudioRenderClient *render;
    UINT32 frames;   // the buffer
    int channels;
    int started;
} pad_sink;

// The active render endpoint whose container is `container`, opened in
// shared mode for 48 kHz S16 `channels` (front pair, plus the back pair on a
// DualSense), converted by WASAPI where the endpoint's mix differs.
static HRESULT pad_sink_open(pad_sink *s, const GUID *container, int channels) {
    memset(s, 0, sizeof(*s));
    s->channels = channels;
    CoInitializeEx(NULL, COINIT_MULTITHREADED);
    IMMDeviceEnumerator *en = NULL;
    IMMDeviceCollection *all = NULL;
    IMMDevice *found = NULL;
    HRESULT hr = CoCreateInstance(&kPadCLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL, &kPadIID_IMMDeviceEnumerator, (void **)&en);
    if (FAILED(hr)) return hr;
    hr = IMMDeviceEnumerator_EnumAudioEndpoints(en, eRender, DEVICE_STATE_ACTIVE, &all);
    if (FAILED(hr)) { IMMDeviceEnumerator_Release(en); return hr; }
    UINT n = 0;
    IMMDeviceCollection_GetCount(all, &n);
    for (UINT i = 0; i < n && !found; i++) {
        IMMDevice *d = NULL;
        IPropertyStore *ps = NULL;
        if (FAILED(IMMDeviceCollection_Item(all, i, &d))) continue;
        if (SUCCEEDED(IMMDevice_OpenPropertyStore(d, STGM_READ, &ps))) {
            PROPVARIANT v;
            PropVariantInit(&v);
            if (SUCCEEDED(IPropertyStore_GetValue(ps, &kPadContainerId, &v)) && v.vt == VT_CLSID && v.puuid &&
                IsEqualGUID(v.puuid, container)) {
                found = d;
                d = NULL;
            }
            PropVariantClear(&v);
            IPropertyStore_Release(ps);
        }
        if (d) IMMDevice_Release(d);
    }
    IMMDeviceCollection_Release(all);
    IMMDeviceEnumerator_Release(en);
    if (!found) return HRESULT_FROM_WIN32(ERROR_NOT_FOUND);

    hr = IMMDevice_Activate(found, &kPadIID_IAudioClient, CLSCTX_ALL, NULL, (void **)&s->client);
    IMMDevice_Release(found);
    if (FAILED(hr)) return hr;

    WAVEFORMATEXTENSIBLE f;
    memset(&f, 0, sizeof(f));
    f.Format.wFormatTag = WAVE_FORMAT_EXTENSIBLE;
    f.Format.nChannels = (WORD)channels;
    f.Format.nSamplesPerSec = 48000;
    f.Format.wBitsPerSample = 16;
    f.Format.nBlockAlign = (WORD)(channels * 2);
    f.Format.nAvgBytesPerSec = 48000 * channels * 2;
    f.Format.cbSize = sizeof(f) - sizeof(WAVEFORMATEX);
    f.Samples.wValidBitsPerSample = 16;
    f.dwChannelMask = channels >= 4 ? (SPEAKER_FRONT_LEFT | SPEAKER_FRONT_RIGHT | SPEAKER_BACK_LEFT | SPEAKER_BACK_RIGHT)
                                    : (SPEAKER_FRONT_LEFT | SPEAKER_FRONT_RIGHT);
    f.SubFormat = kPadSubtypePCM;
    // A 60 ms buffer: the writer keeps 10 ms blocks ahead of the pad.
    hr = IAudioClient_Initialize(s->client, AUDCLNT_SHAREMODE_SHARED,
                                 AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
                                 600000, 0, (WAVEFORMATEX *)&f, NULL);
    if (SUCCEEDED(hr)) hr = IAudioClient_GetService(s->client, &kPadIID_IAudioRenderClient, (void **)&s->render);
    if (SUCCEEDED(hr)) hr = IAudioClient_GetBufferSize(s->client, &s->frames);
    if (FAILED(hr)) {
        if (s->render) IAudioRenderClient_Release(s->render);
        IAudioClient_Release(s->client);
        memset(s, 0, sizeof(*s));
    }
    return hr;
}

// Writes `n` frames, waiting while the buffer has no room. 0, or the failing HRESULT.
static HRESULT pad_sink_write(pad_sink *s, const short *pcm, UINT32 n) {
    while (n > 0) {
        UINT32 padding = 0;
        HRESULT hr = IAudioClient_GetCurrentPadding(s->client, &padding);
        if (FAILED(hr)) return hr;
        UINT32 room = s->frames - padding;
        if (room == 0) { Sleep(2); continue; }
        UINT32 k = room < n ? room : n;
        BYTE *buf = NULL;
        hr = IAudioRenderClient_GetBuffer(s->render, k, &buf);
        if (FAILED(hr)) return hr;
        memcpy(buf, pcm, (size_t)k * s->channels * 2);
        hr = IAudioRenderClient_ReleaseBuffer(s->render, k, 0);
        if (FAILED(hr)) return hr;
        pcm += (size_t)k * s->channels;
        n -= k;
        if (!s->started) {
            IAudioClient_Start(s->client);
            s->started = 1;
        }
    }
    return 0;
}

static void pad_sink_close(pad_sink *s) {
    if (s->client) IAudioClient_Stop(s->client);
    if (s->render) IAudioRenderClient_Release(s->render);
    if (s->client) IAudioClient_Release(s->client);
    memset(s, 0, sizeof(*s));
}
*/
import "C"

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modPadCfgMgr                = windows.NewLazySystemDLL("cfgmgr32.dll")
	procPadGetInterfaceProperty = modPadCfgMgr.NewProc("CM_Get_Device_Interface_PropertyW")
	procPadLocateDevNode        = modPadCfgMgr.NewProc("CM_Locate_DevNodeW")
	procPadGetDevNodeProperty   = modPadCfgMgr.NewProc("CM_Get_DevNode_PropertyW")
	devpkeyDeviceInstanceID     = devPropKey{windows.GUID{Data1: 0x78C34FC8, Data2: 0x104A, Data3: 0x4ACA, Data4: [8]byte{0x9E, 0xA4, 0x52, 0x4D, 0x52, 0x99, 0x6E, 0x57}}, 256}
	devpkeyDeviceContainerID    = devPropKey{windows.GUID{Data1: 0x8C7ED206, Data2: 0x3F8A, Data3: 0x4827, Data4: [8]byte{0xB3, 0xAB, 0xAE, 0x9E, 0x1F, 0xAE, 0xFC, 0x6C}}, 2}
)

type devPropKey struct {
	fmtid windows.GUID
	pid   uint32
}

// padHIDPath is the game collection of the PlayStation pad "winmm:N".
func padHIDPath(deviceID string) (string, PadFamily, error) {
	joyID, err := strconv.Atoi(strings.TrimPrefix(deviceID, "winmm:"))
	if err != nil || !strings.HasPrefix(deviceID, "winmm:") {
		return "", PadOther, ErrNoPadAudio
	}
	vid, pid, ok := winmmVIDPIDOf(uintptr(joyID))
	family := PadFamilyOf(vid, pid)
	if !ok || family == PadOther {
		return "", PadOther, ErrNoPadAudio
	}
	cols, err := hidCollections(vid, pid)
	if err != nil {
		return "", family, err
	}
	for _, c := range cols {
		if c.UsagePage == 0x01 && c.Usage == 0x05 {
			return c.Path, family, nil
		}
	}
	return "", family, ErrNoPadAudio
}

// containerOf is the container (the physical device) of a HID interface
// path: what Windows groups the pad's HID and sound functions under.
func containerOf(path string) (windows.GUID, error) {
	var g windows.GUID
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return g, err
	}
	buf := make([]uint16, 512)
	var typ uint32
	size := uint32(len(buf) * 2)
	if r, _, _ := procPadGetInterfaceProperty.Call(uintptr(unsafe.Pointer(p16)), uintptr(unsafe.Pointer(&devpkeyDeviceInstanceID)),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0); r != 0 {
		return g, fmt.Errorf("instance id: CONFIGRET %d", r)
	}
	var inst uint32
	if r, _, _ := procPadLocateDevNode.Call(uintptr(unsafe.Pointer(&inst)), uintptr(unsafe.Pointer(&buf[0])), 0); r != 0 {
		return g, fmt.Errorf("locate devnode: CONFIGRET %d", r)
	}
	size = uint32(unsafe.Sizeof(g))
	if r, _, _ := procPadGetDevNodeProperty.Call(uintptr(inst), uintptr(unsafe.Pointer(&devpkeyDeviceContainerID)),
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&g)), uintptr(unsafe.Pointer(&size)), 0); r != 0 {
		return g, fmt.Errorf("container id: CONFIGRET %d", r)
	}
	return g, nil
}

// padAudioChannels is 4 for a DualSense (speaker and voice coils), 2 for the
// headset of any other PlayStation pad, if the pad has a sound endpoint; the
// endpoint itself is found when opened.
func padAudioChannels(deviceID string) (int, error) {
	path, family, err := padHIDPath(deviceID)
	if err != nil {
		return 0, err
	}
	if _, err := containerOf(path); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrNoPadAudio, err)
	}
	if family.IsDualSense() {
		return 4, nil
	}
	return 2, nil
}

type wasapiPadSink struct {
	s        C.pad_sink
	channels int
}

func openPadSink(deviceID string, channels int) (padSink, error) {
	path, _, err := padHIDPath(deviceID)
	if err != nil {
		return nil, err
	}
	g, err := containerOf(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoPadAudio, err)
	}
	s := &wasapiPadSink{channels: channels}
	if hr := C.pad_sink_open(&s.s, (*C.GUID)(unsafe.Pointer(&g)), C.int(channels)); hr != 0 {
		return nil, fmt.Errorf("%w: no sound endpoint of the pad (HRESULT 0x%08x)", ErrNoPadAudio, uint32(hr))
	}
	return s, nil
}

func (s *wasapiPadSink) write(pcm []int16) error {
	if hr := C.pad_sink_write(&s.s, (*C.short)(unsafe.Pointer(&pcm[0])), C.UINT32(len(pcm)/s.channels)); hr != 0 {
		return fmt.Errorf("WASAPI: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}

func (s *wasapiPadSink) close() {
	C.pad_sink_close(&s.s)
}
