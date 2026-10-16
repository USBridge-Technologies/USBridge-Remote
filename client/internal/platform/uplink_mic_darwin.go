//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework AudioToolbox -framework CoreFoundation
#cgo ios LDFLAGS: -framework AVFAudio
#include <AudioToolbox/AudioToolbox.h>
#include <TargetConditionals.h>
#include <stdio.h>
#include "uplink_mic_ring.h"
#if TARGET_OS_IPHONE
#include <objc/runtime.h>
#include <objc/message.h>
#endif

// One AudioQueue input for macOS and iOS: the queue converts the default
// input to 48kHz mono S16 and calls back on its own thread.
#define MIC_AQ_BUFFERS 3

typedef struct {
    mic_ring ring; // first: the mic_ring* handed out is this struct
    AudioQueueRef q;
    volatile int running;
} apple_mic;

static void apple_mic_cb(void *ud, AudioQueueRef q, AudioQueueBufferRef b,
                         const AudioTimeStamp *t, UInt32 n, const AudioStreamPacketDescription *d) {
    apple_mic *m = (apple_mic *)ud;
    if (!m->running) return;
    mic_ring_push(&m->ring, (const int16_t *)b->mAudioData, (int)(b->mAudioDataByteSize / 2));
    AudioQueueEnqueueBuffer(q, b, 0, NULL);
}

#if TARGET_OS_IPHONE
// iOS records only in a PlayAndRecord audio session. The stream's own audio
// keeps playing through the speaker (DefaultToSpeaker) or a headset; when
// the microphone stops, the session goes back to what it was. The first
// recording asks for the microphone permission (NSMicrophoneUsageDescription)
// unless the Devices screen asked first (RequestMicPermission).
static id ios_saved_category = NULL;
static unsigned long ios_saved_options = 0;

static id ios_audio_session(void) {
    return ((id (*)(id, SEL))objc_msgSend)((id)objc_getClass("AVAudioSession"), sel_registerName("sharedInstance"));
}

static int ios_audio_session_set(id session, id category, unsigned long options) {
    BOOL ok = ((BOOL (*)(id, SEL, id, unsigned long, id *))objc_msgSend)(
        session, sel_registerName("setCategory:withOptions:error:"), category, options, NULL);
    ((BOOL (*)(id, SEL, BOOL, id *))objc_msgSend)(session, sel_registerName("setActive:error:"), YES, NULL);
    return ok ? 1 : 0;
}

static int ios_audio_session_record(int on) {
    id session = ios_audio_session();
    if (!session) return 0;
    if (on) {
        if (!ios_saved_category) {
            id cat = ((id (*)(id, SEL))objc_msgSend)(session, sel_registerName("category"));
            ios_saved_category = cat ? ((id (*)(id, SEL))objc_msgSend)(cat, sel_registerName("retain")) : NULL;
            ios_saved_options = ((unsigned long (*)(id, SEL))objc_msgSend)(session, sel_registerName("categoryOptions"));
        }
        // DefaultToSpeaker 0x8 | AllowBluetooth 0x4 | AllowBluetoothA2DP 0x20 | MixWithOthers 0x1
        return ios_audio_session_set(session, (id)CFSTR("AVAudioSessionCategoryPlayAndRecord"), 0x8 | 0x4 | 0x20 | 0x1);
    }
    if (ios_saved_category) {
        ios_audio_session_set(session, ios_saved_category, ios_saved_options);
        ((void (*)(id, SEL))objc_msgSend)(ios_saved_category, sel_registerName("release"));
        ios_saved_category = NULL;
    }
    return 1;
}
#endif

static void apple_mic_free(apple_mic *m) {
    if (m->q) {
        m->running = 0;
        AudioQueueStop(m->q, true);
        AudioQueueDispose(m->q, true);
    }
    mic_ring_destroy(&m->ring);
    free(m);
#if TARGET_OS_IPHONE
    ios_audio_session_record(0);
#endif
}

mic_ring *plat_mic_open(char *err, int errlen) {
    apple_mic *m = (apple_mic *)calloc(1, sizeof(*m));
    if (!m || mic_ring_init(&m->ring, 48000) != 0) { snprintf(err, errlen, "out of memory"); free(m); return NULL; }
#if TARGET_OS_IPHONE
    if (!ios_audio_session_record(1)) {
        snprintf(err, errlen, "the audio session can't record");
        mic_ring_destroy(&m->ring); free(m);
        return NULL;
    }
#endif
    AudioStreamBasicDescription f = {0};
    f.mSampleRate = 48000;
    f.mFormatID = kAudioFormatLinearPCM;
    f.mFormatFlags = kLinearPCMFormatFlagIsSignedInteger | kLinearPCMFormatFlagIsPacked;
    f.mChannelsPerFrame = 1;
    f.mBitsPerChannel = 16;
    f.mBytesPerFrame = 2;
    f.mFramesPerPacket = 1;
    f.mBytesPerPacket = 2;
    OSStatus st = AudioQueueNewInput(&f, apple_mic_cb, m, NULL, NULL, 0, &m->q);
    if (st != noErr) {
        snprintf(err, errlen, "AudioQueueNewInput: %d", (int)st);
        m->q = NULL; apple_mic_free(m);
        return NULL;
    }
    m->running = 1;
    for (int i = 0; i < MIC_AQ_BUFFERS; i++) {
        AudioQueueBufferRef b = NULL;
        if (AudioQueueAllocateBuffer(m->q, 960 * 2, &b) == noErr) AudioQueueEnqueueBuffer(m->q, b, 0, NULL);
    }
    st = AudioQueueStart(m->q, NULL);
    if (st != noErr) {
        // macOS: refused microphone permission (TCC) lands here too.
        snprintf(err, errlen, "AudioQueueStart: %d (microphone permission?)", (int)st);
        apple_mic_free(m);
        return NULL;
    }
    return &m->ring;
}

void plat_mic_close(mic_ring *r) { apple_mic_free((apple_mic *)r); }
*/
import "C"

// MicSupported reports whether StartMicCapture can work in this build.
func MicSupported() bool { return true }

// openMic: with the permission refused, AudioQueue would record silence
// (macOS) or fail with a bare status (iOS); say so instead.
func openMic() (micSource, error) {
	if MicPermission() == MicAuthDenied {
		return nil, ErrMicAccessDenied
	}
	return openRingMic()
}
