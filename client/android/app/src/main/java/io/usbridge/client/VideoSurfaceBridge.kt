package io.usbridge.client

import android.graphics.SurfaceTexture
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

    /**
     * Create a SurfaceTexture bound to the given OpenGL ES texture name.
     * The texture must already exist and the EGL context that owns it must be
     * current on the calling thread.
     */
    @JvmStatic
    fun createSurface(texName: Int): Surface? {
        release()
        return try {
            val st = SurfaceTexture(texName)
            st.setOnFrameAvailableListener { frameAvailable.set(true) }
            val s = Surface(st)
            surfaceTexture = st
            surface = s
            Log.i(TAG, "createSurface: texName=$texName")
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

    @JvmStatic
    fun release() {
        surface?.release()
        surfaceTexture?.release()
        surface = null
        surfaceTexture = null
        frameAvailable.set(false)
        Log.i(TAG, "release")
    }
}
