package com.usbridge.client

import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import android.content.Context

object HapticBridge {

    /** Trigger a short haptic tap (≈30 ms). Called from native when RMB long-press threshold is hit. */
    @JvmStatic
    fun triggerShortTap() {
        val activity = MainActivity.getInstance() ?: return
        @Suppress("DEPRECATION")
        val vibrator = activity.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator ?: return
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            vibrator.vibrate(VibrationEffect.createOneShot(30, VibrationEffect.DEFAULT_AMPLITUDE))
        } else {
            @Suppress("DEPRECATION")
            vibrator.vibrate(30)
        }
    }
}
