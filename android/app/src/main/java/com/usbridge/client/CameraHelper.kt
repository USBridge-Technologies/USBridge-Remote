package com.usbridge.client

import android.app.Activity
import android.content.Intent
import android.util.Log

/**
 * Helper class for camera operations
 * Works with any Activity (including GoNativeActivity)
 *
 * NOTE: Due to Fyne limitations, we cannot use custom activities or startActivityForResult
 * This implementation serves as a placeholder - the actual camera functionality
 * should use the image picker (gallery) which already works through SAF in the app.
 */
object CameraHelper {
    private const val TAG = "CameraHelper"

    // Camera capture result (currently unused - placeholder for future implementation)
    @Volatile
    private var capturedImageData: ByteArray? = null

    /**
     * Launch camera to capture image
     * Called from Go via JNI
     *
     * LIMITATION: Cannot actually launch camera due to Fyne restrictions on custom activities
     * Returns an error that will be caught by Go code
     */
    fun launchCamera(activity: Activity) {
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "⚠️ [CAMERA-HELPER] launchCamera() called")
        Log.i(TAG, "⚠️ [CAMERA-HELPER] Camera not supported due to Fyne limitations")
        Log.i(TAG, "⚠️ [CAMERA-HELPER] Please use image picker (gallery) instead")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        // Clear any previous data
        capturedImageData = null

        // Note: We cannot launch camera due to Fyne's strict activity requirements
        // The Go code will timeout and show an error to use gallery instead
    }

    /**
     * Handle camera result
     * Should be called from Activity.onActivityResult()
     */
    fun handleCameraResult(resultCode: Int, data: Intent?) {
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "📥 [CAMERA-RESULT] handleCameraResult called")
        Log.i(TAG, "📍 [CAMERA-RESULT] Result code: $resultCode")
        Log.i(TAG, "📍 [CAMERA-RESULT] Has data: ${data != null}")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        if (resultCode != Activity.RESULT_OK) {
            Log.w(TAG, "⚠️ [CAMERA-RESULT] Camera cancelled or failed")
            capturedImageData = null
            return
        }

        if (data == null) {
            Log.e(TAG, "❌ [CAMERA-RESULT] No data returned")
            capturedImageData = null
            return
        }

        try {
            val extras = data.extras
            val bitmap = extras?.get("data") as? android.graphics.Bitmap

            if (bitmap == null) {
                Log.e(TAG, "❌ [CAMERA-RESULT] No bitmap in extras")
                capturedImageData = null
                return
            }

            Log.i(TAG, "✅ [CAMERA-RESULT] Bitmap received: ${bitmap.width}x${bitmap.height}")

            // Convert bitmap to byte array (PNG format)
            val stream = java.io.ByteArrayOutputStream()
            bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, stream)
            capturedImageData = stream.toByteArray()

            Log.i(TAG, "✅ [CAMERA-RESULT] Image data prepared: ${capturedImageData?.size} bytes")

        } catch (e: Exception) {
            Log.e(TAG, "❌ [CAMERA-RESULT] Exception processing camera result", e)
            capturedImageData = null
        }
    }

    /**
     * Get captured image data
     * Called from Go via JNI
     */
    fun getCapturedImage(): ByteArray? {
        val data = capturedImageData
        Log.i(TAG, "📍 [CAMERA-GET] getCapturedImage() called, data size: ${data?.size ?: 0}")

        // Note: Don't clear data here as Go might call this multiple times while polling
        // Data will be cleared when launchCamera() is called again

        return data
    }

    /**
     * Clear captured image data
     * Called from Go via JNI after successfully receiving the image
     */
    fun clearCapturedImage() {
        Log.i(TAG, "🗑️ [CAMERA-CLEAR] Clearing captured image data")
        capturedImageData = null
    }
}
