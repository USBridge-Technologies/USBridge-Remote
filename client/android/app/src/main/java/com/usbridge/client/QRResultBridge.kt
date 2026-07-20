package com.usbridge.client

import android.util.Log

/**
 * Passes the QR scan result directly to the main app (libUSBridge_Client.so)
 * via JNI, without going through androidbridge.aar. Avoids a conflict between two Go runtimes.
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
