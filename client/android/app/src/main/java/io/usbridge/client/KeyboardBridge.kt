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

    /**
     * Sticky soft-IME text path (RustDesk-style): net deletes + insert string.
     * Bypasses Fyne keyboardTyped Press/Release which doubles characters and
     * mishandles GBoard composition (whole word resent each key).
     */
    @JvmStatic
    external fun onIMETextInput(deleteCount: Int, text: String)

    /**
     * System Back (or equivalent) dismissed the soft IME while our sticky
     * stack was open — Go must collapse special-keys + clear the footer toggle.
     */
    @JvmStatic
    external fun onIMEUserDismissed()

    @JvmStatic
    fun requestLanguageReport() {
        MainActivity.getInstance()?.requestLanguageReport()
    }

    /** Keep the soft keyboard open until explicitly dismissed (system/auto mode). */
    @JvmStatic
    fun setStickyIME(enabled: Boolean) {
        MainActivity.getInstance()?.setStickyIME(enabled)
    }
}
