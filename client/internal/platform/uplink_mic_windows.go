//go:build windows && cgo

package platform

/*
#cgo pkg-config: opus
#cgo LDFLAGS: -lwinmm
#include <windows.h>
#include <mmsystem.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <opus.h>

#define MIC_FRAME 960
#define MIC_BUFS  6

typedef struct {
    HWAVEIN wi;
    HANDLE ev;
    WAVEHDR hdr[MIC_BUFS];
    short pcm[MIC_BUFS][MIC_FRAME];
    int next;
    OpusEncoder *enc;
} mic_t;

static void mic_free(mic_t *m) {
    if (m->wi) {
        waveInStop(m->wi);
        waveInReset(m->wi);
        for (int i = 0; i < MIC_BUFS; i++) {
            if (m->hdr[i].dwFlags & WHDR_PREPARED) waveInUnprepareHeader(m->wi, &m->hdr[i], sizeof(WAVEHDR));
        }
        waveInClose(m->wi);
    }
    if (m->ev) CloseHandle(m->ev);
    if (m->enc) opus_encoder_destroy(m->enc);
    free(m);
}

// The default recording device (WAVE_MAPPER: what Windows' Sound settings
// pick, resampled by the system to 48kHz mono 16-bit), 20ms buffers.
static mic_t *mic_open(char *err, int errlen) {
    int e = 0;
    mic_t *m = calloc(1, sizeof(*m));
    if (!m) { snprintf(err, errlen, "out of memory"); return NULL; }
    m->ev = CreateEventW(NULL, FALSE, FALSE, NULL);
    WAVEFORMATEX wf = {0};
    wf.wFormatTag = WAVE_FORMAT_PCM;
    wf.nChannels = 1;
    wf.nSamplesPerSec = 48000;
    wf.wBitsPerSample = 16;
    wf.nBlockAlign = 2;
    wf.nAvgBytesPerSec = 96000;
    MMRESULT r = waveInOpen(&m->wi, WAVE_MAPPER, &wf, (DWORD_PTR)m->ev, 0, CALLBACK_EVENT);
    if (r != MMSYSERR_NOERROR) {
        m->wi = NULL;
        if (r == MMSYSERR_BADDEVICEID || r == MMSYSERR_NODRIVER)
            snprintf(err, errlen, "no microphone (waveInOpen %u)", r);
        else
            snprintf(err, errlen, "waveInOpen: %u (microphone access off in Windows privacy settings?)", r);
        mic_free(m);
        return NULL;
    }
    for (int i = 0; i < MIC_BUFS; i++) {
        m->hdr[i].lpData = (LPSTR)m->pcm[i];
        m->hdr[i].dwBufferLength = sizeof(m->pcm[i]);
        waveInPrepareHeader(m->wi, &m->hdr[i], sizeof(WAVEHDR));
        waveInAddBuffer(m->wi, &m->hdr[i], sizeof(WAVEHDR));
    }
    m->enc = opus_encoder_create(48000, 1, OPUS_APPLICATION_VOIP, &e);
    if (!m->enc) { snprintf(err, errlen, "opus_encoder_create: %d", e); mic_free(m); return NULL; }
    opus_encoder_ctl(m->enc, OPUS_SET_BITRATE(48000));
    opus_encoder_ctl(m->enc, OPUS_SET_SIGNAL(OPUS_SIGNAL_VOICE));
    if (waveInStart(m->wi) != MMSYSERR_NOERROR) {
        snprintf(err, errlen, "waveInStart failed");
        mic_free(m);
        return NULL;
    }
    return m;
}

// One 20ms frame: waits up to 200ms for the next buffer, encodes it into
// out and gives the buffer back. 0 when nothing came (the caller checks
// for Stop), -1 on error.
static int mic_read_encode(mic_t *m, unsigned char *out, int outlen) {
    WAVEHDR *h = &m->hdr[m->next];
    while (!(h->dwFlags & WHDR_DONE)) {
        if (WaitForSingleObject(m->ev, 200) == WAIT_TIMEOUT && !(h->dwFlags & WHDR_DONE)) return 0;
    }
    int n = 0;
    if (h->dwBytesRecorded == sizeof(m->pcm[0])) {
        n = opus_encode(m->enc, (const opus_int16 *)h->lpData, MIC_FRAME, out, outlen);
    }
    h->dwFlags &= ~WHDR_DONE;
    h->dwBytesRecorded = 0;
    if (waveInAddBuffer(m->wi, h, sizeof(WAVEHDR)) != MMSYSERR_NOERROR) return -1;
    m->next = (m->next + 1) % MIC_BUFS;
    return n < 0 ? -1 : (n == 0 ? -2 : n);
}

static void mic_close(mic_t *m) { mic_free(m); }
*/
import "C"

import (
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"
)

type micCapture struct {
	stop atomic.Bool
	done chan struct{}
	once sync.Once
}

func (c *micCapture) Stop() {
	c.once.Do(func() {
		c.stop.Store(true)
		<-c.done
	})
}

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

// StartMicCapture records the default microphone (winmm waveIn, the device
// picked in Windows' Sound settings) and hands each 20ms frame to onFrame
// Opus-encoded (48kHz mono, at most 200 bytes) with a running sequence
// number, from its own goroutine -- as uplink_mic_linux.go does.
func StartMicCapture(onFrame func(seq uint16, opus []byte)) (UplinkCapture, error) {
	var errBuf [256]C.char
	m := C.mic_open(&errBuf[0], C.int(len(errBuf)))
	if m == nil {
		return nil, fmt.Errorf("%s", C.GoString(&errBuf[0]))
	}
	c := &micCapture{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer C.mic_close(m)
		out := make([]byte, 200)
		var seq uint16
		for !c.stop.Load() {
			n := C.mic_read_encode(m, (*C.uchar)(unsafe.Pointer(&out[0])), C.int(len(out)))
			switch {
			case n == -1:
				return
			case n == 0: // nothing yet: check Stop
			case n == -2: // short buffer: counts as a frame, nothing to send
				seq++
			default:
				seq++
				onFrame(seq, append([]byte(nil), out[:n]...))
			}
		}
	}()
	return c, nil
}
