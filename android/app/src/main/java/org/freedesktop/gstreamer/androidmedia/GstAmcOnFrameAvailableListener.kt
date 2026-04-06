package org.freedesktop.gstreamer.androidmedia

import android.graphics.SurfaceTexture

class GstAmcOnFrameAvailableListener : SurfaceTexture.OnFrameAvailableListener {
    private var context: Long = 0

    override fun onFrameAvailable(surfaceTexture: SurfaceTexture) {
        native_onFrameAvailable(context, surfaceTexture)
    }

    fun getContext(): Long = context

    fun setContext(c: Long) {
        context = c
    }

    private external fun native_onFrameAvailable(context: Long, surfaceTexture: SurfaceTexture)
}
