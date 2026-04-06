package com.usbridge.client

object GestureBridge {
    @JvmStatic
    external fun onViewportGestureStateChanged(active: Boolean)

    @JvmStatic
    external fun onViewportGestureUpdate(
        scaleFactor: Float,
        focusX: Float,
        focusY: Float,
        panDx: Float,
        panDy: Float,
    )
}
