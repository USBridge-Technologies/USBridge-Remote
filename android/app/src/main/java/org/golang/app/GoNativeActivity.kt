package org.golang.app

import android.app.NativeActivity

// Compatibility shim for builds where gomobile bind does not package the
// org.golang.app runtime classes into the consumed AAR.
open class GoNativeActivity : NativeActivity() {
    open fun launchQRScanner() = Unit
}
