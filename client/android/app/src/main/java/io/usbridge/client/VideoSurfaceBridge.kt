package io.usbridge.client

import android.graphics.SurfaceTexture
import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import android.util.Log
import android.view.Surface
import java.util.concurrent.atomic.AtomicBoolean

/**
 * JNI bridge for GPU-accelerated video decoding via SurfaceTexture.
 *
 * Flow (called from native CGO code in moonlight_cgo_android.go):
 *   1. C code creates an EGL context and generates a GL_TEXTURE_EXTERNAL_OES name
 *   2. Calls createSurface(texName) → returns android.view.Surface
 *   3. C code calls ANativeWindow_fromSurface to get ANativeWindow*
 *   4. AMediaCodec is configured with that ANativeWindow → HW decode to GPU
 *   5. After each decoded frame: AMediaCodec_releaseOutputBuffer(..., true)
 *   6. C code calls updateTexImage() (with EGL context current) to latch frame
 *   7. C code renders OES texture to FBO and calls glReadPixels → RGBA
 *   8. RGBA pixels are delivered to goVTFrame for display
 */
object VideoSurfaceBridge {
    private const val TAG = "VideoSurfaceBridge"

    @Volatile private var surfaceTexture: SurfaceTexture? = null
    @Volatile private var surface: Surface? = null

    // Set by SurfaceTexture's onFrameAvailable callback (fires on an arbitrary
    // binder/GL thread), consumed by the native decoder thread right before it
    // calls updateTexImage(). Without this, updateTexImage() can be called
    // before the newly-decoded buffer has actually reached the SurfaceTexture's
    // BufferQueue, silently re-latching the previous frame instead — a classic
    // SurfaceTexture race that Android's own docs warn against. This shows up
    // as ghosting/smearing (stale macroblocks mixed with fresh ones), and is
    // far more likely to trigger against an encoder with jittery per-frame
    // timing (e.g. Allwinner VE2 h264_omx) than a consistent one.
    private val frameAvailable = AtomicBoolean(false)
    private val frameLock = Object()

    // Low-latency mode: onFrameAvailable runs on this thread instead of the
    // main looper, so a busy UI thread cannot delay the decoder thread
    // waiting in waitFrameAvailable.
    @Volatile private var frameThread: HandlerThread? = null

    private val onFrame = SurfaceTexture.OnFrameAvailableListener {
        synchronized(frameLock) {
            frameAvailable.set(true)
            frameLock.notifyAll()
        }
    }

    /**
     * Create a SurfaceTexture bound to the given OpenGL ES texture name.
     * The texture must already exist and the EGL context that owns it must be
     * current on the calling thread.
     */
    @JvmStatic
    fun createSurface(texName: Int, lowLatency: Boolean): Surface? {
        release()
        return try {
            val st = SurfaceTexture(texName)
            if (lowLatency) {
                val t = HandlerThread("usbridge-video-frames", android.os.Process.THREAD_PRIORITY_URGENT_DISPLAY)
                t.start()
                frameThread = t
                st.setOnFrameAvailableListener(onFrame, Handler(t.looper))
            } else {
                st.setOnFrameAvailableListener(onFrame)
            }
            val s = Surface(st)
            surfaceTexture = st
            surface = s
            Log.i(TAG, "createSurface: texName=$texName lowLatency=$lowLatency")
            s
        } catch (e: Exception) {
            Log.e(TAG, "createSurface failed", e)
            null
        }
    }

    /**
     * Consume the pending frame-available flag. Returns true if a new frame
     * has arrived since the last call (and clears the flag), false otherwise.
     */
    @JvmStatic
    fun consumeFrameAvailable(): Boolean = frameAvailable.getAndSet(false)

    /**
     * Block until a new frame has arrived (and consume it), or timeoutMs
     * passes. Returns false on timeout.
     */
    @JvmStatic
    fun waitFrameAvailable(timeoutMs: Int): Boolean {
        val deadline = SystemClock.uptimeMillis() + timeoutMs
        synchronized(frameLock) {
            while (!frameAvailable.get()) {
                val left = deadline - SystemClock.uptimeMillis()
                if (left <= 0) return false
                frameLock.wait(left)
            }
            frameAvailable.set(false)
            return true
        }
    }

    @JvmStatic
    fun setDefaultBufferSize(width: Int, height: Int) {
        surfaceTexture?.setDefaultBufferSize(width, height)
    }

    /**
     * Latch the most-recently decoded frame onto the GL texture.
     * MUST be called with the same EGL context current that owns texName.
     */
    @JvmStatic
    fun updateTexImage() {
        try {
            surfaceTexture?.updateTexImage()
        } catch (e: Exception) {
            Log.e(TAG, "updateTexImage failed", e)
        }
    }

    /**
     * Get the texture coordinate transform matrix for the OES texture.
     * mtx must be a float[16].
     */
    @JvmStatic
    fun getTransformMatrix(mtx: FloatArray) {
        surfaceTexture?.getTransformMatrix(mtx)
    }

    /**
     * The largest size up to w x h, same aspect ratio, that the hardware
     * decoder for [mime] accepts, as [width, height, maxFpsAtThatSize];
     * null if there is no hardware decoder (or no answer). Decoders refuse
     * to configure above their maximum size at all, e.g. Snapdragon 695
     * tops out at 1920x1088, so a 4K stream would never start.
     */
    @JvmStatic
    fun bestDecoderSize(mime: String, w: Int, h: Int): IntArray? {
        if (w <= 0 || h <= 0) return null
        return try {
            val caps = hardwareDecoderCaps(mime) ?: return null
            val name = caps.first
            val vc = caps.second
            val wa = vc.widthAlignment.coerceAtLeast(2)
            val ha = vc.heightAlignment.coerceAtLeast(2)
            val heights = listOf(h, 2160, 1600, 1440, 1200, 1080, 900, 720, 540, 480)
                .filter { it <= h }.distinct()
            for (ch0 in heights) {
                val ch = ch0 / ha * ha
                val cw = ((ch.toLong() * w / h).toInt()) / wa * wa
                if (cw <= 0 || ch <= 0 || !vc.isSizeSupported(cw, ch)) continue
                val maxFps = vc.getSupportedFrameRatesFor(cw, ch).upper.toInt()
                Log.i(TAG, "bestDecoderSize($mime ${w}x$h): $name -> ${cw}x$ch (max ${maxFps} fps)")
                return intArrayOf(cw, ch, maxFps)
            }
            Log.w(TAG, "bestDecoderSize($mime ${w}x$h): $name supports none of the candidate sizes")
            null
        } catch (e: Exception) {
            Log.e(TAG, "bestDecoderSize failed", e)
            null
        }
    }

    // The decoder MediaCodec.createDecoderByType picks for mime: the first
    // hardware one in the platform list.
    private fun hardwareDecoderCaps(mime: String): Pair<String, MediaCodecInfo.VideoCapabilities>? {
        for (info in MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos) {
            if (info.isEncoder) continue
            val type = info.supportedTypes.firstOrNull { it.equals(mime, ignoreCase = true) } ?: continue
            val hw = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                info.isHardwareAccelerated && !info.isSoftwareOnly
            } else {
                !info.name.startsWith("OMX.google.") && !info.name.startsWith("c2.android.")
            }
            if (!hw) continue
            val vc = info.getCapabilitiesForType(type).videoCapabilities ?: continue
            return Pair(info.name, vc)
        }
        return null
    }

    @JvmStatic
    fun release() {
        surface?.release()
        surfaceTexture?.release()
        surface = null
        surfaceTexture = null
        frameThread?.quitSafely()
        frameThread = null
        frameAvailable.set(false)
        Log.i(TAG, "release")
    }
}
