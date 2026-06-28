package com.usbridge.client

object GyroBridge {
    @JvmStatic
    external fun onGyroEvent(rx: Float, ry: Float, rz: Float, dtMs: Float)

    @JvmStatic
    external fun onVolumeButton(button: Int, pressed: Boolean)

    @JvmStatic
    external fun onShakeGesture()

    @JvmStatic
    external fun isGyroMouseModeActive(): Boolean
}
