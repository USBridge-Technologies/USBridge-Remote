//go:build android && cgo

package platform

/*
#cgo CFLAGS: -D__ANDROID_UNAVAILABLE_SYMBOLS_ARE_WEAK__
#cgo LDFLAGS: -laaudio -llog -landroid
#include <jni.h>
#include <aaudio/AAudio.h>
#include <android/log.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "uplink_mic_ring.h"

extern void usbridgeMIDIData(int handle, unsigned char *data, int length);

// ── Microphone: AAudio, converted here to 48kHz mono when the device
// gives another rate or more channels.

typedef struct {
    mic_ring ring; // first: the mic_ring* handed out is this struct
    AAudioStream *s;
    int rate, channels;
    // Resampler: the next output sample lies between input samples
    // (int)pos-1 and (int)pos of this callback, frac of the way; -1 is
    // prev, the last sample of the previous callback.
    double pos;
    int16_t prev;
    int16_t out[4096];
} droid_mic;

static aaudio_data_callback_result_t droid_mic_cb(AAudioStream *s, void *ud, void *audio, int32_t frames) {
    droid_mic *m = (droid_mic *)ud;
    const int16_t *in = (const int16_t *)audio;
    if (m->rate == 48000 && m->channels == 1) {
        mic_ring_push(&m->ring, in, frames);
        return AAUDIO_CALLBACK_RESULT_CONTINUE;
    }
    // Downmix to mono, then linear interpolation to 48 kHz.
    double step = (double)m->rate / 48000.0;
    int n = 0;
    for (;;) {
        int i = (int)m->pos;
        if (i >= frames) break;
        double frac = m->pos - i;
        int32_t a = 0, b = 0;
        for (int c = 0; c < m->channels; c++) {
            b += in[i * m->channels + c];
            a += i > 0 ? in[(i - 1) * m->channels + c] : 0;
        }
        b /= m->channels;
        a = i > 0 ? a / m->channels : m->prev;
        m->out[n++] = (int16_t)(a + (b - a) * frac);
        if (n == (int)(sizeof(m->out) / sizeof(m->out[0]))) { mic_ring_push(&m->ring, m->out, n); n = 0; }
        m->pos += step;
    }
    if (n) mic_ring_push(&m->ring, m->out, n);
    m->pos -= frames;
    int32_t last = 0;
    for (int c = 0; c < m->channels; c++) last += in[(frames - 1) * m->channels + c];
    m->prev = (int16_t)(last / m->channels);
    return AAUDIO_CALLBACK_RESULT_CONTINUE;
}

static void droid_mic_err(AAudioStream *s, void *ud, aaudio_result_t e) {
    mic_ring_fail(&((droid_mic *)ud)->ring);
}

mic_ring *plat_mic_open(char *err, int errlen) {
    droid_mic *m = (droid_mic *)calloc(1, sizeof(*m));
    if (!m || mic_ring_init(&m->ring, 48000) != 0) { snprintf(err, errlen, "out of memory"); free(m); return NULL; }
    AAudioStreamBuilder *b = NULL;
    aaudio_result_t r = AAudio_createStreamBuilder(&b);
    if (r != AAUDIO_OK) { snprintf(err, errlen, "AAudio: %s", AAudio_convertResultToText(r)); mic_ring_destroy(&m->ring); free(m); return NULL; }
    AAudioStreamBuilder_setDirection(b, AAUDIO_DIRECTION_INPUT);
    AAudioStreamBuilder_setSampleRate(b, 48000);
    AAudioStreamBuilder_setChannelCount(b, 1);
    AAudioStreamBuilder_setFormat(b, AAUDIO_FORMAT_PCM_I16);
    AAudioStreamBuilder_setPerformanceMode(b, AAUDIO_PERFORMANCE_MODE_LOW_LATENCY);
    AAudioStreamBuilder_setDataCallback(b, droid_mic_cb, m);
    AAudioStreamBuilder_setErrorCallback(b, droid_mic_err, m);
    r = AAudioStreamBuilder_openStream(b, &m->s);
    AAudioStreamBuilder_delete(b);
    if (r != AAUDIO_OK) { snprintf(err, errlen, "AAudio openStream: %s", AAudio_convertResultToText(r)); mic_ring_destroy(&m->ring); free(m); return NULL; }
    m->rate = AAudioStream_getSampleRate(m->s);
    m->channels = AAudioStream_getChannelCount(m->s);
    if (m->rate <= 0) m->rate = 48000;
    if (m->channels <= 0) m->channels = 1;
    __android_log_print(ANDROID_LOG_INFO, "UplinkMic", "AAudio input %d Hz, %d ch", m->rate, m->channels);
    r = AAudioStream_requestStart(m->s);
    if (r != AAUDIO_OK) {
        snprintf(err, errlen, "AAudio start: %s", AAudio_convertResultToText(r));
        AAudioStream_close(m->s); mic_ring_destroy(&m->ring); free(m);
        return NULL;
    }
    return &m->ring;
}

void plat_mic_close(mic_ring *r) {
    droid_mic *m = (droid_mic *)r;
    AAudioStream_requestStop(m->s);
    AAudioStream_close(m->s); // no callback runs after close returns
    mic_ring_destroy(&m->ring);
    free(m);
}

// ── JNI: io.usbridge.client.UplinkBridge (permission, MIDI).

__attribute__((used))
JNIEXPORT void JNICALL Java_io_usbridge_client_UplinkBridge_nativeMidiData(JNIEnv *env, jclass cls, jint handle, jbyteArray data, jint off, jint len) {
    unsigned char buf[512];
    while (len > 0) {
        int n = len > (jint)sizeof(buf) ? (int)sizeof(buf) : (int)len;
        (*env)->GetByteArrayRegion(env, data, off, n, (jbyte *)buf);
        if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); return; }
        usbridgeMIDIData((int)handle, buf, n);
        off += n;
        len -= n;
    }
}

void uplinkKeepJNISymbols(void) {
    (void)Java_io_usbridge_client_UplinkBridge_nativeMidiData;
}

// uplink_bridge loads UplinkBridge through the activity's class loader (a
// Go thread's FindClass only sees system classes).
static jclass uplink_bridge(JNIEnv *env, jobject ctx) {
    jclass ctxCls = (*env)->GetObjectClass(env, ctx);
    jmethodID getCL = (*env)->GetMethodID(env, ctxCls, "getClassLoader", "()Ljava/lang/ClassLoader;");
    jobject cl = (*env)->CallObjectMethod(env, ctx, getCL);
    jclass clCls = (*env)->FindClass(env, "java/lang/ClassLoader");
    jmethodID loadCls = (*env)->GetMethodID(env, clCls, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
    jstring name = (*env)->NewStringUTF(env, "io.usbridge.client.UplinkBridge");
    jclass cls = (jclass)(*env)->CallObjectMethod(env, cl, loadCls, name);
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); cls = NULL; }
    (*env)->DeleteLocalRef(env, name);
    (*env)->DeleteLocalRef(env, clCls);
    (*env)->DeleteLocalRef(env, cl);
    (*env)->DeleteLocalRef(env, ctxCls);
    return cls;
}

static int jni_mic_permission(uintptr_t envp, uintptr_t ctxp, int request) {
    JNIEnv *env = (JNIEnv *)envp;
    jobject ctx = (jobject)ctxp;
    jclass cls = uplink_bridge(env, ctx);
    if (!cls) return 0;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "micPermission", "(Landroid/app/Activity;Z)Z");
    jboolean ok = mid ? (*env)->CallStaticBooleanMethod(env, cls, mid, ctx, (jboolean)(request != 0)) : JNI_FALSE;
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); ok = JNI_FALSE; }
    (*env)->DeleteLocalRef(env, cls);
    return ok ? 1 : 0;
}

// jni_midi_list: malloc'd "id\tname\n" lines, NULL on failure.
static char *jni_midi_list(uintptr_t envp, uintptr_t ctxp) {
    JNIEnv *env = (JNIEnv *)envp;
    jobject ctx = (jobject)ctxp;
    jclass cls = uplink_bridge(env, ctx);
    if (!cls) return NULL;
    char *out = NULL;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "listMidiInputs", "(Landroid/content/Context;)Ljava/lang/String;");
    jstring s = mid ? (jstring)(*env)->CallStaticObjectMethod(env, cls, mid, ctx) : NULL;
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); s = NULL; }
    if (s) {
        const char *utf = (*env)->GetStringUTFChars(env, s, NULL);
        if (utf) { out = strdup(utf); (*env)->ReleaseStringUTFChars(env, s, utf); }
        (*env)->DeleteLocalRef(env, s);
    }
    (*env)->DeleteLocalRef(env, cls);
    return out;
}

static int jni_midi_open(uintptr_t envp, uintptr_t ctxp, const char *id, int handle) {
    JNIEnv *env = (JNIEnv *)envp;
    jobject ctx = (jobject)ctxp;
    jclass cls = uplink_bridge(env, ctx);
    if (!cls) return 0;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "openMidiInput", "(Landroid/content/Context;Ljava/lang/String;I)Z");
    jstring jid = (*env)->NewStringUTF(env, id);
    jboolean ok = mid ? (*env)->CallStaticBooleanMethod(env, cls, mid, ctx, jid, (jint)handle) : JNI_FALSE;
    if ((*env)->ExceptionCheck(env)) { (*env)->ExceptionClear(env); ok = JNI_FALSE; }
    (*env)->DeleteLocalRef(env, jid);
    (*env)->DeleteLocalRef(env, cls);
    return ok ? 1 : 0;
}

static void jni_midi_close(uintptr_t envp, uintptr_t ctxp, int handle) {
    JNIEnv *env = (JNIEnv *)envp;
    jobject ctx = (jobject)ctxp;
    jclass cls = uplink_bridge(env, ctx);
    if (!cls) return;
    jmethodID mid = (*env)->GetStaticMethodID(env, cls, "closeMidiInput", "(I)V");
    if (mid) (*env)->CallStaticVoidMethod(env, cls, mid, (jint)handle);
    if ((*env)->ExceptionCheck(env)) (*env)->ExceptionClear(env);
    (*env)->DeleteLocalRef(env, cls);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"
)

func init() { C.uplinkKeepJNISymbols() }

// withJNI runs fn with the JNI env and the activity, on a JVM thread.
func withJNI(fn func(env, ctx C.uintptr_t)) error {
	return driver.RunNative(func(c any) error {
		a, ok := c.(*driver.AndroidContext)
		if !ok {
			return errors.New("no Android context")
		}
		fn(C.uintptr_t(a.Env), C.uintptr_t(a.Ctx))
		return nil
	})
}

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

// micPermissionWait is how long a switched-on microphone waits for the
// user to answer the RECORD_AUDIO prompt.
const micPermissionWait = 2 * time.Minute

// droidMic opens AAudio once RECORD_AUDIO is granted: the first read asks
// for it, and reads come back empty until the user allows it.
type droidMic struct {
	inner   micSource
	asked   time.Time
	checked time.Time
}

func openMic() (micSource, error) { return &droidMic{}, nil }

func micPermission(request bool) bool {
	granted := false
	_ = withJNI(func(env, ctx C.uintptr_t) {
		granted = C.jni_mic_permission(env, ctx, boolInt(request)) != 0
	})
	return granted
}

func boolInt(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (m *droidMic) read(pcm []int16) (bool, error) {
	if m.inner != nil {
		return m.inner.read(pcm)
	}
	if !m.checked.IsZero() && time.Since(m.checked) < 250*time.Millisecond {
		time.Sleep(20 * time.Millisecond)
		return false, nil
	}
	m.checked = time.Now()
	if !micPermission(m.asked.IsZero()) {
		if m.asked.IsZero() {
			m.asked = time.Now()
		} else if time.Since(m.asked) > micPermissionWait {
			return false, fmt.Errorf("microphone permission not granted")
		}
		return false, nil
	}
	inner, err := openRingMic()
	if err != nil {
		return false, err
	}
	m.inner = inner
	return m.inner.read(pcm)
}

func (m *droidMic) close() {
	if m.inner != nil {
		m.inner.close()
	}
}

var (
	midiListMu   sync.Mutex
	midiListAt   time.Time
	midiListLast []MIDIInputInfo
)

// midiListTTL: the Devices screen polls every second; asking MidiManager
// (a JNI hop and a binder call) every few seconds is enough.
const midiListTTL = 3 * time.Second

// ListMIDIInputs lists android.media.midi's output ports (USB, Bluetooth
// and virtual MIDI devices), named "device:port".
func ListMIDIInputs() []MIDIInputInfo {
	midiListMu.Lock()
	defer midiListMu.Unlock()
	if !midiListAt.IsZero() && time.Since(midiListAt) < midiListTTL {
		return midiListLast
	}
	midiListAt = time.Now()
	midiListLast = listAndroidMIDIInputs()
	return midiListLast
}

func listAndroidMIDIInputs() []MIDIInputInfo {
	var list string
	_ = withJNI(func(env, ctx C.uintptr_t) {
		if s := C.jni_midi_list(env, ctx); s != nil {
			list = C.GoString(s)
			C.free(unsafe.Pointer(s))
		}
	})
	var out []MIDIInputInfo
	for _, line := range strings.Split(list, "\n") {
		id, name, ok := strings.Cut(line, "\t")
		if !ok || isGadgetMIDIName(name) {
			continue
		}
		out = append(out, MIDIInputInfo{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

type droidMIDICapture struct {
	handle int
	once   sync.Once
}

func (c *droidMIDICapture) Stop() {
	c.once.Do(func() {
		_ = withJNI(func(env, ctx C.uintptr_t) { C.jni_midi_close(env, ctx, C.int(c.handle)) })
		unregisterMIDISink(c.handle)
	})
}

// StartMIDICapture passes what the port id (see ListMIDIInputs) plays to
// onData, from android.media.midi's thread.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	handle := registerMIDISink(onData)
	cid := C.CString(id)
	defer C.free(unsafe.Pointer(cid))
	opened := false
	err := withJNI(func(env, ctx C.uintptr_t) {
		opened = C.jni_midi_open(env, ctx, cid, C.int(handle)) != 0
	})
	if err != nil || !opened {
		unregisterMIDISink(handle)
		return nil, fmt.Errorf("MIDI input %q is gone", id)
	}
	return &droidMIDICapture{handle: handle}, nil
}
