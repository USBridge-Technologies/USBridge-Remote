package io.usbridge.client

/**
 * Delivers the exact Android IME (system keyboard) height to Go via JNI.
 * Same pattern as GestureBridge: the external functions are declared here,
 * the JNI wrapper implementation lives in keyboard_ime_android.go (CGo).
 *
 * @param imeHeightPx  keyboard height in pixels (0 when hidden)
 * @param screenHeightPx full window (decorView) height in pixels
 */
object KeyboardBridge {
    @JvmStatic
    external fun onIMEHeightChanged(imeHeightPx: Int, screenHeightPx: Int)

    @JvmStatic
    external fun onLanguageChanged(lang: String)

    @JvmStatic
    fun requestLanguageReport() {
        MainActivity.getInstance()?.requestLanguageReport()
    }
}
