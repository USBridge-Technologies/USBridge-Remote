package com.usbridge.client

import android.util.Log

/**
 * Передаёт результат QR-сканирования напрямую в main app (libUSBridge_Client.so)
 * через JNI, без использования nbdbridge.aar. Избегает конфликта двух Go runtime.
 */
object QRResultBridge {
    private const val TAG = "QRResultBridge"

    init {
        try {
            System.loadLibrary("USBridge_Client")
        } catch (e: UnsatisfiedLinkError) {
            Log.e(TAG, "Failed to load USBridge_Client", e)
        }
    }

    @JvmStatic
    external fun deliverQRResult(contents: String)

    @JvmStatic
    external fun deliverQRCancel()
}
