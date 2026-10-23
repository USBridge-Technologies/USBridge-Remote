//go:build linux && !android && cgo

package platform

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

// libasound, loaded at run time: the client builds without its headers, and
// every desktop Linux has the library.
typedef struct _snd_pcm snd_pcm_t;
static void *pa_lib;
static int (*pa_open)(snd_pcm_t **, const char *, int, int);
static int (*pa_set_params)(snd_pcm_t *, int, int, unsigned int, unsigned int, int, unsigned int);
static long (*pa_writei)(snd_pcm_t *, const void *, unsigned long);
static int (*pa_recover)(snd_pcm_t *, int, int);
static int (*pa_close)(snd_pcm_t *);
static const char *(*pa_strerror)(int);

static int pa_load(void) {
    if (pa_lib) return 0;
    void *l = dlopen("libasound.so.2", RTLD_NOW | RTLD_LOCAL);
    if (!l) return -1;
    pa_open = dlsym(l, "snd_pcm_open");
    pa_set_params = dlsym(l, "snd_pcm_set_params");
    pa_writei = dlsym(l, "snd_pcm_writei");
    pa_recover = dlsym(l, "snd_pcm_recover");
    pa_close = dlsym(l, "snd_pcm_close");
    pa_strerror = dlsym(l, "snd_strerror");
    if (!pa_open || !pa_set_params || !pa_writei || !pa_recover || !pa_close || !pa_strerror) { dlclose(l); return -1; }
    pa_lib = l;
    return 0;
}

// Playback (0), S16_LE (2), interleaved read/write (3), resampled if need be,
// a 40 ms buffer.
static int pa_open_pcm(snd_pcm_t **pcm, const char *name, unsigned int channels) {
    int err = pa_open(pcm, name, 0, 0);
    if (err < 0) return err;
    err = pa_set_params(*pcm, 2, 3, channels, 48000, 1, 40000);
    if (err < 0) { pa_close(*pcm); *pcm = NULL; }
    return err;
}

// cgo calls functions, not pointers.
static long pa_w(snd_pcm_t *pcm, const void *buf, unsigned long frames) { return pa_writei(pcm, buf, frames); }
static int pa_rec(snd_pcm_t *pcm, int err) { return pa_recover(pcm, err, 1); }
static void pa_cl(snd_pcm_t *pcm) { pa_close(pcm); }
static const char *pa_err(int err) { return pa_strerror(err); }
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"
)

// padCard is the ALSA card of the pad behind an evdev node and its playback
// channel count, from /proc/asound/cardN/stream0.
func padCard(deviceID string) (card int, channels int, err error) {
	h, err := psHIDOf(deviceID)
	if err != nil || h.usbDir == "" {
		return 0, 0, ErrNoPadAudio
	}
	cards, _ := filepath.Glob(filepath.Join(h.usbDir, "*", "sound", "card*"))
	for _, c := range cards {
		n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(c), "card"))
		if err != nil {
			continue
		}
		stream, err := os.ReadFile(fmt.Sprintf("/proc/asound/card%d/stream0", n))
		if err != nil {
			continue
		}
		_, playback, found := strings.Cut(string(stream), "Playback:")
		if !found {
			continue
		}
		playback, _, _ = strings.Cut(playback, "Capture:")
		for _, line := range strings.Split(playback, "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Channels:"); ok {
				if ch, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && (ch == 2 || ch == 4) {
					return n, ch, nil
				}
			}
		}
	}
	return 0, 0, ErrNoPadAudio
}

func padAudioChannels(deviceID string) (int, error) {
	_, ch, err := padCard(deviceID)
	return ch, err
}

type alsaPadSink struct {
	pcm      *C.snd_pcm_t
	channels int
}

func openPadSink(deviceID string, channels int) (padSink, error) {
	card, _, err := padCard(deviceID)
	if err != nil {
		return nil, err
	}
	if C.pa_load() != 0 {
		return nil, fmt.Errorf("%w: no libasound", ErrNoPadAudio)
	}
	name := C.CString(fmt.Sprintf("plughw:%d,0", card))
	defer C.free(unsafe.Pointer(name))
	var pcm *C.snd_pcm_t
	if e := C.pa_open_pcm(&pcm, name, C.uint(channels)); e < 0 {
		return nil, fmt.Errorf("plughw:%d,0: %s", card, C.GoString(C.pa_err(e)))
	}
	return &alsaPadSink{pcm: pcm, channels: channels}, nil
}

func (s *alsaPadSink) write(pcm []int16) error {
	frames := len(pcm) / s.channels
	for frames > 0 {
		w := C.pa_w(s.pcm, unsafe.Pointer(&pcm[0]), C.ulong(frames))
		if w < 0 {
			if e := C.pa_rec(s.pcm, C.int(w)); e < 0 {
				return fmt.Errorf("ALSA: %s", C.GoString(C.pa_err(e)))
			}
			continue
		}
		pcm = pcm[int(w)*s.channels:]
		frames -= int(w)
	}
	return nil
}

func (s *alsaPadSink) close() {
	C.pa_cl(s.pcm)
}
