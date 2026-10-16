//go:build windows && cgo

package platform

/*
#cgo LDFLAGS: -lole32
#define COBJMACROS
#include <windows.h>
#include <mmdeviceapi.h>
#include <audioclient.h>
#include <stdio.h>
#include "uplink_mic_ring.h"

#ifndef AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM
#define AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM 0x80000000
#endif
#ifndef AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
#define AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY 0x08000000
#endif

// Own copies, so nothing depends on which IIDs libuuid carries.
static const GUID kMicCLSID_MMDeviceEnumerator = {0xBCDE0395, 0xE52F, 0x467C, {0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}};
static const GUID kMicIID_IMMDeviceEnumerator = {0xA95664D2, 0x9614, 0x4F35, {0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}};
static const GUID kMicIID_IAudioClient = {0x1CB9AD4C, 0xDBFA, 0x4C32, {0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}};
static const GUID kMicIID_IAudioCaptureClient = {0xC8ADBD64, 0xE71E, 0x48A0, {0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}};

// The default capture device in shared mode, event driven, converted by
// WASAPI itself to 48kHz mono S16 (AUTOCONVERTPCM, Windows 7+), on a thread
// of its own that owns the COM objects.
typedef struct {
    mic_ring ring; // first: the mic_ring* handed out is this struct
    HANDLE thread, stop_ev, ready_ev;
    HRESULT init_hr;
    char init_where[64];
} win_mic;

static DWORD WINAPI win_mic_thread(LPVOID arg) {
    win_mic *m = (win_mic *)arg;
    IMMDeviceEnumerator *en = NULL;
    IMMDevice *dev = NULL;
    IAudioClient *client = NULL;
    IAudioCaptureClient *cap = NULL;
    HANDLE buf_ev = CreateEventW(NULL, FALSE, FALSE, NULL);
    HRESULT hr;
    int started = 0;

    HRESULT co = CoInitializeEx(NULL, COINIT_MULTITHREADED);
#define FAIL(where) do { m->init_hr = hr; snprintf(m->init_where, sizeof(m->init_where), "%s", where); goto out; } while (0)
    hr = CoCreateInstance(&kMicCLSID_MMDeviceEnumerator, NULL, CLSCTX_ALL, &kMicIID_IMMDeviceEnumerator, (void **)&en);
    if (FAILED(hr)) FAIL("MMDeviceEnumerator");
    hr = IMMDeviceEnumerator_GetDefaultAudioEndpoint(en, eCapture, eConsole, &dev);
    if (FAILED(hr)) FAIL("no default microphone");
    hr = IMMDevice_Activate(dev, &kMicIID_IAudioClient, CLSCTX_ALL, NULL, (void **)&client);
    if (FAILED(hr)) FAIL("IMMDevice::Activate");
    WAVEFORMATEX wfx = {
        .wFormatTag = WAVE_FORMAT_PCM, .nChannels = 1, .nSamplesPerSec = 48000,
        .wBitsPerSample = 16, .nBlockAlign = 2, .nAvgBytesPerSec = 96000, .cbSize = 0,
    };
    hr = IAudioClient_Initialize(client, AUDCLNT_SHAREMODE_SHARED,
        AUDCLNT_STREAMFLAGS_EVENTCALLBACK | AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
        200000, 0, &wfx, NULL); // 20 ms
    if (FAILED(hr)) FAIL("IAudioClient::Initialize");
    hr = IAudioClient_SetEventHandle(client, buf_ev);
    if (FAILED(hr)) FAIL("IAudioClient::SetEventHandle");
    hr = IAudioClient_GetService(client, &kMicIID_IAudioCaptureClient, (void **)&cap);
    if (FAILED(hr)) FAIL("IAudioCaptureClient");
    hr = IAudioClient_Start(client);
    if (FAILED(hr)) FAIL("IAudioClient::Start");
#undef FAIL
    started = 1;
    m->init_hr = S_OK;
    SetEvent(m->ready_ev);

    HANDLE waits[2] = {m->stop_ev, buf_ev};
    for (;;) {
        DWORD w = WaitForMultipleObjects(2, waits, FALSE, 200);
        if (w == WAIT_OBJECT_0) break;
        UINT32 packet = 0;
        while (SUCCEEDED(hr = IAudioCaptureClient_GetNextPacketSize(cap, &packet)) && packet > 0) {
            BYTE *data = NULL;
            UINT32 frames = 0;
            DWORD flags = 0;
            hr = IAudioCaptureClient_GetBuffer(cap, &data, &frames, &flags, NULL, NULL);
            if (FAILED(hr)) break;
            mic_ring_push(&m->ring, (flags & AUDCLNT_BUFFERFLAGS_SILENT) ? NULL : (const int16_t *)data, (int)frames);
            IAudioCaptureClient_ReleaseBuffer(cap, frames);
        }
        if (FAILED(hr)) { // e.g. AUDCLNT_E_DEVICE_INVALIDATED: unplugged
            mic_ring_fail(&m->ring);
            break;
        }
    }

out:
    if (!started) SetEvent(m->ready_ev);
    if (client && started) IAudioClient_Stop(client);
    if (cap) IAudioCaptureClient_Release(cap);
    if (client) IAudioClient_Release(client);
    if (dev) IMMDevice_Release(dev);
    if (en) IMMDeviceEnumerator_Release(en);
    CloseHandle(buf_ev);
    if (SUCCEEDED(co)) CoUninitialize();
    return 0;
}

static void win_mic_free(win_mic *m) {
    if (m->thread) { SetEvent(m->stop_ev); WaitForSingleObject(m->thread, INFINITE); CloseHandle(m->thread); }
    if (m->stop_ev) CloseHandle(m->stop_ev);
    if (m->ready_ev) CloseHandle(m->ready_ev);
    mic_ring_destroy(&m->ring);
    free(m);
}

mic_ring *plat_mic_open(char *err, int errlen) {
    win_mic *m = (win_mic *)calloc(1, sizeof(*m));
    if (!m || mic_ring_init(&m->ring, 48000) != 0) { snprintf(err, errlen, "out of memory"); free(m); return NULL; }
    m->stop_ev = CreateEventW(NULL, TRUE, FALSE, NULL);
    m->ready_ev = CreateEventW(NULL, TRUE, FALSE, NULL);
    m->thread = CreateThread(NULL, 0, win_mic_thread, m, 0, NULL);
    if (!m->thread) { snprintf(err, errlen, "CreateThread failed"); win_mic_free(m); return NULL; }
    WaitForSingleObject(m->ready_ev, INFINITE);
    if (FAILED(m->init_hr)) {
        snprintf(err, errlen, "WASAPI: %s (0x%08lx)", m->init_where, (unsigned long)m->init_hr);
        win_mic_free(m);
        return NULL;
    }
    return &m->ring;
}

void plat_mic_close(mic_ring *r) { win_mic_free((win_mic *)r); }
*/
import "C"

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

func openMic() (micSource, error) {
	src, err := openRingMic()
	if err != nil && micConsentDenied() {
		return nil, ErrMicAccessDenied
	}
	return src, err
}

// micConsentDenied: Settings → Privacy & security → Microphone has
// microphone access (system-wide, this user, or desktop apps) off. WASAPI
// then only says E_ACCESSDENIED.
func micConsentDenied() bool {
	const store = `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone`
	denied := func(root registry.Key, path string) bool {
		k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
		if err != nil {
			return false
		}
		defer k.Close()
		v, _, err := k.GetStringValue("Value")
		return err == nil && strings.EqualFold(v, "Deny")
	}
	return denied(registry.LOCAL_MACHINE, store) || denied(registry.CURRENT_USER, store) ||
		denied(registry.CURRENT_USER, store+`\NonPackaged`)
}
