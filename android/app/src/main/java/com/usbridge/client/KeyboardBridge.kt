package com.usbridge.client

/**
 * Передаёт точную высоту Android IME (системной клавиатуры) в Go через JNI.
 * Паттерн аналогичен GestureBridge: external-функции объявлены здесь,
 * реализация JNI-обёрток — в keyboard_ime_android.go (CGo).
 *
 * @param imeHeightPx  высота клавиатуры в пикселях (0 когда скрыта)
 * @param screenHeightPx полная высота окна (decorView) в пикселях
 */
object KeyboardBridge {
    @JvmStatic
    external fun onIMEHeightChanged(imeHeightPx: Int, screenHeightPx: Int)
}
