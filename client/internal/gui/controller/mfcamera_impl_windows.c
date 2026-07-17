// Minimal Media Foundation webcam capture used by the desktop QR scanner on
// Windows. Replaces the previous gstreamer mfvideosrc pipeline so the QR
// scanner (and the Windows build in general) no longer needs GStreamer.
//
// Only ever one camera session is open at a time (the QR scan popup), so a
// single global IMFSourceReader plus a critical section is enough — no need
// for a handle table.

#define COBJMACROS
#define CINTERFACE
#define WIN32_LEAN_AND_MEAN

#include <windows.h>
#include <mfapi.h>
#include <mfidl.h>
#include <mfreadwrite.h>
#include <mferror.h>
#include <stdint.h>
#include <stdio.h>
#include <stdarg.h>

#include "mfcamera_impl_windows.h"

extern void goMFCameraLog(char *msg, int level);

static void mf_log(int level, const char *fmt, ...) {
    char buf[512];
    va_list args;
    va_start(args, fmt);
    vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    goMFCameraLog(buf, level);
}

static CRITICAL_SECTION g_lock;
static int g_lock_ready = 0;
static IMFSourceReader *g_reader = NULL;
static int g_width = 0;
static int g_height = 0;
static LONG g_stride = 0; // signed; negative means bottom-up rows
static int g_com_inited = 0;
static int g_mf_started = 0;

static void ensure_lock(void) {
    if (!g_lock_ready) {
        InitializeCriticalSection(&g_lock);
        g_lock_ready = 1;
    }
}

// mf_camera_open opens the first available video capture device and
// negotiates an RGB32 output at (reqWidth x reqHeight), letting the source
// reader's built-in video processor handle color conversion and scaling.
// On success, the negotiated size is written to outWidth/outHeight (may
// differ slightly from the request if the device can't scale exactly).
int mf_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight) {
    ensure_lock();
    EnterCriticalSection(&g_lock);

    if (g_reader != NULL) {
        *outWidth = g_width;
        *outHeight = g_height;
        LeaveCriticalSection(&g_lock);
        return 1;
    }

    HRESULT hr = CoInitializeEx(NULL, COINIT_MULTITHREADED);
    if (SUCCEEDED(hr)) {
        g_com_inited = 1;
    } else if (hr != RPC_E_CHANGED_MODE) {
        mf_log(2, "CoInitializeEx failed: 0x%08lx", hr);
        LeaveCriticalSection(&g_lock);
        return 0;
    }

    hr = MFStartup(MF_VERSION, MFSTARTUP_LITE);
    if (FAILED(hr)) {
        mf_log(2, "MFStartup failed: 0x%08lx", hr);
        goto fail_com;
    }
    g_mf_started = 1;

    IMFAttributes *pAttr = NULL;
    IMFActivate **ppDevices = NULL;
    UINT32 deviceCount = 0;
    IMFMediaSource *pSource = NULL;
    IMFAttributes *pReaderAttr = NULL;
    IMFMediaType *pType = NULL;
    IMFMediaType *pNegotiatedType = NULL;

    hr = MFCreateAttributes(&pAttr, 1);
    if (FAILED(hr)) { mf_log(2, "MFCreateAttributes failed: 0x%08lx", hr); goto fail_mf; }

    hr = IMFAttributes_SetGUID(pAttr, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE,
                                &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID);
    if (FAILED(hr)) { mf_log(2, "SetGUID(SOURCE_TYPE) failed: 0x%08lx", hr); goto fail_attr; }

    hr = MFEnumDeviceSources(pAttr, &ppDevices, &deviceCount);
    if (FAILED(hr) || deviceCount == 0) {
        mf_log(1, "No video capture device found (hr=0x%08lx, count=%u)", hr, deviceCount);
        goto fail_attr;
    }

    hr = IMFActivate_ActivateObject(ppDevices[0], &IID_IMFMediaSource, (void **)&pSource);
    if (FAILED(hr)) { mf_log(2, "ActivateObject failed: 0x%08lx", hr); goto fail_devices; }

    hr = MFCreateAttributes(&pReaderAttr, 1);
    if (FAILED(hr)) { mf_log(2, "MFCreateAttributes(reader) failed: 0x%08lx", hr); goto fail_source; }

    hr = IMFAttributes_SetUINT32(pReaderAttr, &MF_SOURCE_READER_ENABLE_VIDEO_PROCESSING, TRUE);
    if (FAILED(hr)) { mf_log(2, "SetUINT32(ENABLE_VIDEO_PROCESSING) failed: 0x%08lx", hr); goto fail_reader_attr; }

    hr = MFCreateSourceReaderFromMediaSource(pSource, pReaderAttr, &g_reader);
    if (FAILED(hr)) { mf_log(2, "MFCreateSourceReaderFromMediaSource failed: 0x%08lx", hr); goto fail_reader_attr; }

    hr = MFCreateMediaType(&pType);
    if (FAILED(hr)) { mf_log(2, "MFCreateMediaType failed: 0x%08lx", hr); goto fail_reader; }

    hr = IMFMediaType_SetGUID(pType, &MF_MT_MAJOR_TYPE, &MFMediaType_Video);
    if (SUCCEEDED(hr)) hr = IMFMediaType_SetGUID(pType, &MF_MT_SUBTYPE, &MFVideoFormat_RGB32);
    if (SUCCEEDED(hr)) {
        // MF_MT_FRAME_SIZE packs width into the high 32 bits, height into the low
        // 32 bits of a UINT64 (MFSetAttributeSize's convenience wrapper is a
        // C++-only inline in mfapi.h, so pack it manually here).
        UINT64 packedSize = (((UINT64)(UINT32)reqWidth) << 32) | (UINT32)reqHeight;
        hr = IMFMediaType_SetUINT64(pType, &MF_MT_FRAME_SIZE, packedSize);
    }
    if (FAILED(hr)) { mf_log(2, "Building requested media type failed: 0x%08lx", hr); goto fail_type; }

    hr = IMFSourceReader_SetCurrentMediaType(g_reader, MF_SOURCE_READER_FIRST_VIDEO_STREAM, NULL, pType);
    if (FAILED(hr)) { mf_log(2, "SetCurrentMediaType(RGB32) failed: 0x%08lx", hr); goto fail_type; }

    hr = IMFSourceReader_GetCurrentMediaType(g_reader, MF_SOURCE_READER_FIRST_VIDEO_STREAM, &pNegotiatedType);
    if (FAILED(hr)) { mf_log(2, "GetCurrentMediaType failed: 0x%08lx", hr); goto fail_type; }

    UINT64 packedNegotiated = 0;
    hr = IMFMediaType_GetUINT64(pNegotiatedType, &MF_MT_FRAME_SIZE, &packedNegotiated);
    UINT32 w = (UINT32)(packedNegotiated >> 32);
    UINT32 h = (UINT32)(packedNegotiated & 0xFFFFFFFF);
    if (FAILED(hr) || w == 0 || h == 0) {
        mf_log(2, "Negotiated frame size unavailable: 0x%08lx", hr);
        IMFMediaType_Release(pNegotiatedType);
        goto fail_type;
    }

    UINT32 strideU32 = 0;
    hr = IMFMediaType_GetUINT32(pNegotiatedType, &MF_MT_DEFAULT_STRIDE, &strideU32);
    if (SUCCEEDED(hr)) {
        g_stride = (LONG)strideU32; // reinterpret as signed
    } else {
        LONG computed = 0;
        GUID subtype;
        if (SUCCEEDED(IMFMediaType_GetGUID(pNegotiatedType, &MF_MT_SUBTYPE, &subtype)) &&
            SUCCEEDED(MFGetStrideForBitmapInfoHeader(subtype.Data1, w, &computed))) {
            g_stride = computed;
        } else {
            g_stride = (LONG)(w * 4); // best-effort fallback: packed, top-down
        }
    }

    g_width = (int)w;
    g_height = (int)h;
    *outWidth = g_width;
    *outHeight = g_height;

    mf_log(0, "Camera opened: %dx%d stride=%ld", g_width, g_height, g_stride);

    IMFMediaType_Release(pNegotiatedType);
    IMFMediaType_Release(pType);
    IMFAttributes_Release(pReaderAttr);
    IMFMediaSource_Release(pSource);
    for (UINT32 i = 0; i < deviceCount; i++) IMFActivate_Release(ppDevices[i]);
    CoTaskMemFree(ppDevices);
    IMFAttributes_Release(pAttr);

    LeaveCriticalSection(&g_lock);
    return 1;

fail_type:
    IMFMediaType_Release(pType);
fail_reader:
    IMFSourceReader_Release(g_reader);
    g_reader = NULL;
fail_reader_attr:
    if (pReaderAttr) IMFAttributes_Release(pReaderAttr);
fail_source:
    IMFMediaSource_Release(pSource);
fail_devices:
    for (UINT32 i = 0; i < deviceCount; i++) IMFActivate_Release(ppDevices[i]);
    CoTaskMemFree(ppDevices);
fail_attr:
    IMFAttributes_Release(pAttr);
fail_mf:
    if (g_mf_started) { MFShutdown(); g_mf_started = 0; }
fail_com:
    if (g_com_inited) { CoUninitialize(); g_com_inited = 0; }
    LeaveCriticalSection(&g_lock);
    return 0;
}

// mf_camera_read_frame blocks until the next frame is available (or the
// device stops), converting it to top-down packed RGBA into buf.
// Returns: 1 = frame written, 0 = end of stream / fatal error (caller must
// stop), 2 = no sample this call (caller should just call again).
int mf_camera_read_frame(uint8_t *buf, int bufLen) {
    EnterCriticalSection(&g_lock);
    IMFSourceReader *reader = g_reader;
    int width = g_width, height = g_height;
    LONG stride = g_stride;
    LeaveCriticalSection(&g_lock);

    if (reader == NULL) return 0;

    DWORD streamIndex = 0, flags = 0;
    LONGLONG timestamp = 0;
    IMFSample *pSample = NULL;

    HRESULT hr = IMFSourceReader_ReadSample(reader, MF_SOURCE_READER_FIRST_VIDEO_STREAM, 0,
                                             &streamIndex, &flags, &timestamp, &pSample);
    if (FAILED(hr)) {
        mf_log(2, "ReadSample failed: 0x%08lx", hr);
        return 0;
    }
    if (flags & MF_SOURCE_READERF_ENDOFSTREAM) {
        mf_log(0, "Camera stream ended");
        if (pSample) IMFSample_Release(pSample);
        return 0;
    }
    if (pSample == NULL) {
        return 2; // e.g. gap in the stream; try again
    }

    IMFMediaBuffer *pBuffer = NULL;
    hr = IMFSample_ConvertToContiguousBuffer(pSample, &pBuffer);
    if (FAILED(hr)) {
        mf_log(1, "ConvertToContiguousBuffer failed: 0x%08lx", hr);
        IMFSample_Release(pSample);
        return 2;
    }

    BYTE *data = NULL;
    DWORD maxLen = 0, curLen = 0;
    hr = IMFMediaBuffer_Lock(pBuffer, &data, &maxLen, &curLen);
    if (FAILED(hr)) {
        mf_log(1, "MediaBuffer Lock failed: 0x%08lx", hr);
        IMFMediaBuffer_Release(pBuffer);
        IMFSample_Release(pSample);
        return 2;
    }

    LONG rowBytes = stride >= 0 ? stride : -stride;
    int expected = (int)(rowBytes >= width * 4 ? (rowBytes * height) : (width * 4 * height));
    if ((int)curLen < expected || bufLen < width * height * 4) {
        mf_log(1, "Frame buffer too small: curLen=%lu expected=%d bufLen=%d", curLen, expected, bufLen);
        IMFMediaBuffer_Unlock(pBuffer);
        IMFMediaBuffer_Release(pBuffer);
        IMFSample_Release(pSample);
        return 2;
    }

    // RGB32 is BGRX in memory; convert to RGBA while copying and undo the
    // bottom-up row order that Media Foundation uses for a negative stride.
    for (int y = 0; y < height; y++) {
        int srcY = (stride < 0) ? (height - 1 - y) : y;
        const BYTE *srcRow = data + (size_t)srcY * rowBytes;
        uint8_t *dstRow = buf + (size_t)y * width * 4;
        for (int x = 0; x < width; x++) {
            uint8_t b = srcRow[x * 4 + 0];
            uint8_t g = srcRow[x * 4 + 1];
            uint8_t r = srcRow[x * 4 + 2];
            dstRow[x * 4 + 0] = r;
            dstRow[x * 4 + 1] = g;
            dstRow[x * 4 + 2] = b;
            dstRow[x * 4 + 3] = 0xFF;
        }
    }

    IMFMediaBuffer_Unlock(pBuffer);
    IMFMediaBuffer_Release(pBuffer);
    IMFSample_Release(pSample);
    return 1;
}

void mf_camera_close(void) {
    ensure_lock();
    EnterCriticalSection(&g_lock);
    if (g_reader != NULL) {
        IMFSourceReader_Release(g_reader);
        g_reader = NULL;
    }
    g_width = 0;
    g_height = 0;
    g_stride = 0;
    if (g_mf_started) { MFShutdown(); g_mf_started = 0; }
    if (g_com_inited) { CoUninitialize(); g_com_inited = 0; }
    LeaveCriticalSection(&g_lock);
}
