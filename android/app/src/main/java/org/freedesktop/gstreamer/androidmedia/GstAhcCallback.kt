package org.freedesktop.gstreamer.androidmedia

import android.hardware.Camera

class GstAhcCallback(callback: Long, userData: Long) : Camera.PreviewCallback,
    Camera.ErrorCallback,
    Camera.AutoFocusCallback {

    var mUserData: Long = userData
    var mCallback: Long = callback

    override fun onPreviewFrame(data: ByteArray, camera: Camera) {
        gst_ah_camera_on_preview_frame(data, camera, mCallback, mUserData)
    }

    override fun onError(error: Int, camera: Camera) {
        gst_ah_camera_on_error(error, camera, mCallback, mUserData)
    }

    override fun onAutoFocus(success: Boolean, camera: Camera) {
        gst_ah_camera_on_auto_focus(success, camera, mCallback, mUserData)
    }

    companion object {
        @JvmStatic
        external fun gst_ah_camera_on_preview_frame(
            data: ByteArray,
            camera: Camera,
            callback: Long,
            userData: Long,
        )

        @JvmStatic
        external fun gst_ah_camera_on_error(
            error: Int,
            camera: Camera,
            callback: Long,
            userData: Long,
        )

        @JvmStatic
        external fun gst_ah_camera_on_auto_focus(
            success: Boolean,
            camera: Camera,
            callback: Long,
            userData: Long,
        )
    }
}
