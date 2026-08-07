package org.golang.app

import android.app.NativeActivity
import android.content.Context
import android.content.pm.ActivityInfo
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.text.Editable
import android.text.InputType
import android.text.TextWatcher
import android.text.method.DigitsKeyListener
import android.util.Log
import android.view.KeyEvent
import android.view.KeyCharacterMap
import android.view.View
import android.view.WindowInsets
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.TextView
import java.io.File

// Compatibility shim for builds where gomobile bind does not package the
// org.golang.app runtime classes into the consumed AAR.
open class GoNativeActivity : NativeActivity() {
    private var textEdit: EditText? = null
    private var defaultKeyListener: android.text.method.KeyListener? = null
    private var ignoreKey = false
    private var keyboardUp = false

    init {
        goNativeActivity = this
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        loadLibraryFromManifest()
        preloadNativeRuntime()
        super.onCreate(savedInstanceState)
        setupEntry()
        updateLayout()
    }

    open fun launchQRScanner() = Unit

    fun getTmpdir(): String = cacheDir.absolutePath

    private external fun keyboardTyped(text: String)
    private external fun keyboardDelete()
    private external fun backPressed()

    private fun updateLayout() {
        try {
            val insets: WindowInsets = window.decorView.rootWindowInsets ?: return
            @Suppress("DEPRECATION")
            insetsChanged(
                insets.systemWindowInsetTop,
                insets.systemWindowInsetBottom,
                insets.systemWindowInsetLeft,
                insets.systemWindowInsetRight,
            )
        } catch (_: Throwable) {
        }
    }

    private external fun insetsChanged(top: Int, bottom: Int, left: Int, right: Int)

    private fun setupEntry() {
        runOnUiThread {
            if (textEdit != null) {
                return@runOnUiThread
            }

            val edit = EditText(goNativeActivity)
            edit.visibility = View.GONE
            edit.inputType = DEFAULT_INPUT_TYPE
            defaultKeyListener = edit.keyListener

            val layoutParams = FrameLayout.LayoutParams(
                FrameLayout.LayoutParams.WRAP_CONTENT,
                FrameLayout.LayoutParams.WRAP_CONTENT,
            )
            edit.layoutParams = layoutParams
            addContentView(edit, layoutParams)

            ignoreKey = true
            edit.setText(" ")
            edit.setSelection(edit.text.length)
            ignoreKey = false

            edit.addTextChangedListener(object : TextWatcher {
                override fun onTextChanged(s: CharSequence, start: Int, before: Int, count: Int) {
                    if (ignoreKey || count <= 0) {
                        return
                    }
                    keyboardTyped(s.subSequence(start, start + count).toString())
                }

                override fun beforeTextChanged(s: CharSequence, start: Int, count: Int, after: Int) {
                    if (ignoreKey || count <= 0) {
                        return
                    }
                    repeat(count) {
                        keyboardDelete()
                    }
                }

                override fun afterTextChanged(s: Editable) {
                    if (s.length >= 1) {
                        return
                    }
                    ignoreKey = true
                    edit.setText(" ")
                    edit.setSelection(edit.text.length)
                    ignoreKey = false
                }
            })

            textEdit = edit
        }
    }

    private fun doShowKeyboard(keyboardType: Int) {
        runOnUiThread {
            val edit = textEdit ?: return@runOnUiThread
            var imeOptions = EditorInfo.IME_FLAG_NO_ENTER_ACTION
            var inputType = DEFAULT_INPUT_TYPE
            var keys: String? = null

            when (keyboardType) {
                DEFAULT_KEYBOARD_CODE -> Unit
                SINGLELINE_KEYBOARD_CODE -> imeOptions = EditorInfo.IME_ACTION_DONE
                NUMBER_KEYBOARD_CODE -> {
                    imeOptions = EditorInfo.IME_ACTION_DONE
                    inputType = InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_VARIATION_NORMAL
                    keys = "0123456789.,-' "
                }
                PASSWORD_KEYBOARD_CODE -> {
                    imeOptions = EditorInfo.IME_ACTION_DONE
                    inputType = InputType.TYPE_CLASS_TEXT or
                        InputType.TYPE_TEXT_VARIATION_PASSWORD or
                        InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                }
                else -> Log.e(TAG, "unknown keyboard type, use default")
            }

            edit.imeOptions = imeOptions or EditorInfo.IME_FLAG_NO_FULLSCREEN
            edit.inputType = inputType
            edit.keyListener = if (keys != null) {
                DigitsKeyListener.getInstance(keys)
            } else {
                defaultKeyListener
            }
            edit.setOnEditorActionListener(TextView.OnEditorActionListener { _, actionId, _: KeyEvent? ->
                if (actionId == EditorInfo.IME_ACTION_DONE) {
                    keyboardTyped("\n")
                }
                false
            })

            ignoreKey = true
            edit.setText(" ")
            edit.setSelection(edit.text.length)
            ignoreKey = false

            edit.visibility = View.VISIBLE
            edit.bringToFront()
            edit.requestFocus()
            val imm = getSystemService(Context.INPUT_METHOD_SERVICE) as? InputMethodManager
            imm?.showSoftInput(edit, 0)
        }
    }

    private fun doHideKeyboard() {
        val imm = getSystemService(Context.INPUT_METHOD_SERVICE) as? InputMethodManager
        val view = findViewById<View>(android.R.id.content)?.rootView
        if (imm != null && view != null) {
            imm.hideSoftInputFromWindow(view.windowToken, 0)
        }
        runOnUiThread {
            textEdit?.visibility = View.GONE
        }
    }

    override fun onBackPressed() {
        if (keyboardUp) {
            hideKeyboard()
            return
        }
        backPressed()
    }

    private fun preloadNativeRuntime() {
        val nativeDir = applicationInfo?.nativeLibraryDir ?: return
        val files = File(nativeDir)
            .listFiles { file -> file.isFile && file.name.endsWith(".so") }
            ?.sortedWith(compareBy<File>({ libraryLoadRank(it.name) }, { it.name }))
            ?: return

        for (file in files) {
            if (file.name == APP_LIBRARY) {
                continue
            }
            if (libraryLoadRank(file.name) > MAX_PRELOAD_RANK) {
                continue
            }
            try {
                System.load(file.absolutePath)
                Log.d(TAG, "Preloaded ${file.name}")
            } catch (e: UnsatisfiedLinkError) {
                Log.w(TAG, "Failed to preload ${file.name}", e)
            }
        }
    }

    private fun libraryLoadRank(name: String): Int = when (name) {
        "libc++_shared.so" -> 0
        "libgojni.so" -> 80
        else -> 70
    }

    companion object {
        private const val TAG = "GoNativeActivity"
        private const val APP_LIBRARY = "libUSBridge_Client.so"
        private const val MAX_PRELOAD_RANK = 50
        private const val DEFAULT_INPUT_TYPE = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
        private const val DEFAULT_KEYBOARD_CODE = 0
        private const val SINGLELINE_KEYBOARD_CODE = 1
        private const val NUMBER_KEYBOARD_CODE = 2
        private const val PASSWORD_KEYBOARD_CODE = 3
        @Volatile
        private var goNativeActivity: GoNativeActivity? = null

        @JvmStatic
        fun getRune(deviceId: Int, keyCode: Int, metaState: Int): Int {
            return try {
                val rune = KeyCharacterMap.load(deviceId).get(keyCode, metaState)
                if (rune == 0) -1 else rune
            } catch (_: KeyCharacterMap.UnavailableException) {
                -1
            } catch (t: Throwable) {
                Log.e(TAG, "exception reading KeyCharacterMap", t)
                -1
            }
        }

        @JvmStatic
        fun showKeyboard(keyboardType: Int) {
            goNativeActivity?.doShowKeyboard(keyboardType)
            goNativeActivity?.keyboardUp = true
        }

        @JvmStatic
        fun hideKeyboard() {
            goNativeActivity?.doHideKeyboard()
            goNativeActivity?.keyboardUp = false
        }
    }

    private fun loadLibraryFromManifest() {
        try {
            val component = intent?.component
            if (component == null) {
                Log.e(TAG, "loadLibrary failed: intent.component is null")
                return
            }
            val ai: ActivityInfo = packageManager.getActivityInfo(
                component,
                PackageManager.GET_META_DATA,
            )
            val libName = ai.metaData?.getString("android.app.lib_name")
            if (libName.isNullOrBlank()) {
                Log.e(TAG, "loadLibrary: no manifest metadata found")
                return
            }
            System.loadLibrary(libName)
        } catch (t: Throwable) {
            Log.e(TAG, "loadLibrary failed", t)
        }
    }

}
