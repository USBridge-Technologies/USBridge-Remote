package com.usbridge.client

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Rect
import android.net.ConnectivityManager
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import android.util.Log
import android.view.MotionEvent
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import org.golang.app.GoNativeActivity
import java.net.NetworkInterface
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Main Activity extending GoNativeActivity for Fyne
 * Handles SAF (Storage Access Framework) file picker results and QR code scanning
 */
class MainActivity : GoNativeActivity() {

    companion object {
        private const val TAG = "MainActivity"
        const val SAF_PICK_REQUEST_CODE = 1001
        const val FYNE_FILE_OPEN_REQUEST_CODE = 1002
        const val FYNE_FILE_SAVE_REQUEST_CODE = 1003
        const val CAMERA_REQUEST_CODE = 3001
        const val QR_SCAN_REQUEST_CODE = 4001
        const val VPN_PREPARE_REQUEST_CODE = 4501
        private const val CAMERA_PERMISSION_REQUEST_CODE = 5001
        private const val CAMERA_LAUNCH_PERMISSION_REQUEST_CODE = 5002

        // Singleton instance for accessing from Go via JNI
        @Volatile
        private var instance: MainActivity? = null

        @JvmStatic
        fun getInstance(): MainActivity? = instance
    }

    // Camera capture result
    @Volatile
    private var capturedImageData: ByteArray? = null

    @Volatile
    private var vpnPermissionState: Int = 0

    // Two-finger gesture tracker — mode (PAN_ZOOM vs SCROLL_ZOOM) is locked at gesture start.
    // 150 dp converted to physical pixels: fingers closer than this → scroll, farther → pan+zoom.
    private val gestureTracker: TwoFingerGestureTracker by lazy {
        TwoFingerGestureTracker(
            panZoomThresholdPx = 150f * resources.displayMetrics.density,
            onActiveChanged = { active ->
                GestureBridge.onViewportGestureStateChanged(active)
            },
            onPanZoom = { scale, focusX, focusY, dx, dy ->
                GestureBridge.onViewportGestureUpdate(scale, focusX, focusY, dx, dy)
            },
            onScrollZoom = { scrollDy, scale, focusX, focusY ->
                // Scroll wheel and zoom are independent — both delivered every frame.
                GestureBridge.onScrollGesture(scrollDy)
                if (scale != 1f) {
                    GestureBridge.onViewportGestureUpdate(scale, focusX, focusY, 0f, 0f)
                }
            },
        )
    }

    private lateinit var connectivityManager: ConnectivityManager
    private val networkCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: android.net.Network) {
            Log.i(TAG, "🌐 [NETWORK] Network available: $network")
            NetworkBridge.onNetworkChanged()
        }

        override fun onLost(network: android.net.Network) {
            Log.i(TAG, "🌐 [NETWORK] Network lost: $network")
            NetworkBridge.onNetworkChanged()
        }

        override fun onCapabilitiesChanged(network: android.net.Network, capabilities: android.net.NetworkCapabilities) {
            Log.d(TAG, "🌐 [NETWORK] Network capabilities changed: $network")
            // This is often triggered when WiFi signal strength changes or switching between 4G/5G
            // We notify Go to ensure Tailscale/netmon stays up to date
            NetworkBridge.onNetworkChanged()
        }
    }

    private val inputMethodReceiver = object : android.content.BroadcastReceiver() {
        override fun onReceive(context: android.content.Context?, intent: android.content.Intent?) {
            if (intent?.action == Intent.ACTION_INPUT_METHOD_CHANGED) {
                Log.d(TAG, "⌨️ [IME] Input method changed")
                reportLanguage()
            }
        }
    }


    private var lastReportedLang = ""

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        instance = this
        Log.i(TAG, "MainActivity created")
        setupIMEListener()
        
        connectivityManager = getSystemService(ConnectivityManager::class.java)
        connectivityManager.registerDefaultNetworkCallback(networkCallback)

        val filter = android.content.IntentFilter(Intent.ACTION_INPUT_METHOD_CHANGED)
        registerReceiver(inputMethodReceiver, filter)
    }

    private fun reportLanguage() {
        try {
            val imm = getSystemService(android.view.inputmethod.InputMethodManager::class.java)
            val ims = imm?.currentInputMethodSubtype
            
            var lang = ""
            if (ims != null) {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                    lang = ims.languageTag.ifEmpty { ims.locale }
                } else {
                    @Suppress("DEPRECATION")
                    lang = ims.locale
                }
            }
            
            if (lang.isEmpty()) {
                lang = java.util.Locale.getDefault().toString()
            }
            
            if (lang == lastReportedLang) return
            lastReportedLang = lang
            
            Log.i(TAG, "⌨️ [IME] Current language detected: $lang (subtype: ${ims?.mode}, extra: ${ims?.extraValue})")
            KeyboardBridge.onLanguageChanged(lang)
        } catch (e: Exception) {
            Log.e(TAG, "❌ [IME] Error reporting language", e)
        }
    }

    /**
     * Отслеживает высоту системной клавиатуры Android (IME) через ViewTreeObserver.
     *
     * С темой Fullscreen `windowSoftInputMode=adjustResize` не работает (ограничение Android),
     * поэтому мы вручную измеряем высоту IME в пикселях и передаём точное значение в Go
     * через KeyboardBridge.onIMEHeightChanged(). Go конвертирует пиксели в Fyne-единицы
     * пропорционально высоте экрана и выставляет нижний отступ под нашей клавиатурой.
     *
     * Дополнительно, когда IME скрывается (imeHeight становится 0):
     *  - вызываем GoNativeActivity.hideKeyboard() чтобы сбросить keyboardUp=false
     *    → иначе кнопка "Назад" не выходит из fullscreen (видит keyboardUp=true и снова прячет клавиатуру)
     *  - это также устанавливает textEdit.visibility=GONE, что триггерит Fyne перерисовать layout
     *    → иначе в обычном режиме layout не возвращается на место пока поле ввода в фокусе
     */
    private fun setupIMEListener() {
        val decorView = window.decorView
        var lastImeHeightPx = -1

        decorView.viewTreeObserver.addOnGlobalLayoutListener {
            val rect = Rect()
            decorView.getWindowVisibleDisplayFrame(rect)
            val screenHeight = decorView.height
            if (screenHeight == 0) return@addOnGlobalLayoutListener

            val visibleImeHeight = (screenHeight - rect.bottom).coerceAtLeast(0)

            // В edge-to-edge / fullscreen режиме getWindowVisibleDisplayFrame не учитывает
            // навигационный бар (окно занимает весь экран). Получаем его высоту через WindowInsets,
            // чтобы тулбар с кнопками всегда поднимался над видимым навбаром.
            val navBarHeight = ViewCompat.getRootWindowInsets(decorView)
                ?.getInsets(WindowInsetsCompat.Type.navigationBars())
                ?.bottom ?: 0

            // Используем максимум: IME-высота или высота навбара.
            // Это гарантирует корректный отступ как в обычном, так и в fullscreen edge-to-edge режиме.
            val imeHeight = maxOf(visibleImeHeight, navBarHeight)

            // Проверяем язык всегда, когда клавиатура открыта,
            // так как переключение раскладки может не менять высоту окна.
            if (imeHeight > 0) {
                reportLanguage()
            }

            if (imeHeight != lastImeHeightPx) {
                val wasKeyboardVisible = lastImeHeightPx > navBarHeight
                val isInitialLayout = lastImeHeightPx < 0
                lastImeHeightPx = imeHeight
                Log.d(TAG, "⌨️ [IME] height changed: imeHeight=$imeHeight (visible=$visibleImeHeight, navBar=$navBarHeight) screenHeight=$screenHeight")

                if (visibleImeHeight == 0 && wasKeyboardVisible) {
                    // IME только что скрылась (пользователь нажал ↓ или кнопку сворачивания).
                    // Синхронизируем состояние GoNativeActivity: keyboardUp=false и textEdit=GONE.
                    // Без этого: кнопка "Назад" видит keyboardUp=true и не выходит из fullscreen;
                    // а в обычном режиме Fyne не знает что клавиатура ушла и не перерисовывает layout.
                    Log.d(TAG, "⌨️ [IME] скрылась — сбрасываем keyboardUp через hideKeyboard()")
                    org.golang.app.GoNativeActivity.hideKeyboard()

                    // Снимаем фокус с поля ввода, чтобы Fyne перерисовал layout.
                    // Без этого в обычном режиме layout не возвращается на место, пока не кликнешь по окну.
                    currentFocus?.let {
                        Log.d(TAG, "⌨️ [IME] сбрасываем фокус с ${it.javaClass.simpleName}")
                        it.clearFocus()
                    }
                } else if (imeHeight == 0 && isInitialLayout) {
                    // Первый запуск: Fyne использует полный canvas включая NavBar.
                    // hideKeyboard() заставляет Fyne пересчитать размер canvas без NavBar,
                    // чтобы кнопки и поля ввода изначально были в правильной позиции.
                    // Задержка нужна чтобы Fyne успел инициализировать textEdit.
                    Log.d(TAG, "⌨️ [IME] первый layout — планируем начальный hideKeyboard()")
                    decorView.postDelayed({
                        try {
                            org.golang.app.GoNativeActivity.hideKeyboard()
                            Log.d(TAG, "⌨️ [IME] начальный hideKeyboard() выполнен")
                        } catch (e: Exception) {
                            Log.e(TAG, "❌ [IME] начальный hideKeyboard() не удался: ${e.message}")
                        }
                    }, 600)
                }

                try {
                    KeyboardBridge.onIMEHeightChanged(imeHeight, screenHeight)
                } catch (e: Exception) {
                    Log.e(TAG, "❌ [IME] Error calling KeyboardBridge", e)
                }
            }
        }
    }

    override fun onDestroy() {
        try {
            unregisterReceiver(inputMethodReceiver)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to unregister input method receiver", e)
        }
        try {
            connectivityManager.unregisterNetworkCallback(networkCallback)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to unregister network callback", e)
        }
        gestureTracker.cancel()
        super.onDestroy()
        instance = null
        Log.i(TAG, "MainActivity destroyed")
    }

    override fun dispatchTouchEvent(ev: MotionEvent): Boolean {
        gestureTracker.onTouchEvent(ev)
        return super.dispatchTouchEvent(ev)
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        val startTime = System.currentTimeMillis()
        val threadName = Thread.currentThread().name
        val threadId = Thread.currentThread().id

        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "🎯 [ACTIVITY-RESULT] onActivityResult called in MainActivity")
        Log.i(TAG, "📍 [ACTIVITY-RESULT] Thread: $threadName (ID: $threadId)")
        Log.i(TAG, "📍 [ACTIVITY-RESULT] Request code: $requestCode")
        Log.i(TAG, "📍 [ACTIVITY-RESULT] Result code: $resultCode")
        Log.i(TAG, "📍 [ACTIVITY-RESULT] Has data: ${data != null}")
        if (data != null) {
            Log.i(TAG, "📍 [ACTIVITY-RESULT] Data URI: ${data.data}")
        }
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        super.onActivityResult(requestCode, resultCode, data)

        when (requestCode) {
            SAF_PICK_REQUEST_CODE -> {
                Log.i(TAG, "📤 [ACTIVITY-RESULT] Forwarding to NbdBridge.handleActivityResult()...")
                NbdBridge.handleActivityResult(requestCode, resultCode, data, contentResolver)
                val elapsed = System.currentTimeMillis() - startTime
                Log.i(TAG, "✅ [ACTIVITY-RESULT] NbdBridge.handleActivityResult() returned after ${elapsed}ms")
            }
            FYNE_FILE_OPEN_REQUEST_CODE -> {
                Log.i(TAG, "📤 [ACTIVITY-RESULT] Fyne showFileOpen result — forwarding to NbdBridge...")
                NbdBridge.handleActivityResult(SAF_PICK_REQUEST_CODE, resultCode, data, contentResolver)
                val elapsed = System.currentTimeMillis() - startTime
                Log.i(TAG, "✅ [ACTIVITY-RESULT] Fyne showFileOpen handled in ${elapsed}ms")
            }
            FYNE_FILE_SAVE_REQUEST_CODE -> {
                Log.i(TAG, "📤 [ACTIVITY-RESULT] Fyne showFileSave result: code=$resultCode, uri=${data?.data}")
            }
            CAMERA_REQUEST_CODE -> {
                Log.i(TAG, "📤 [ACTIVITY-RESULT] Handling camera result...")
                handleCameraResult(resultCode, data)
                val elapsed = System.currentTimeMillis() - startTime
                Log.i(TAG, "✅ [ACTIVITY-RESULT] Camera result handled in ${elapsed}ms")
            }
            QR_SCAN_REQUEST_CODE -> {
                Log.i(TAG, "📤 [ACTIVITY-RESULT] Handling QR scan result...")
                handleQRScanResult(resultCode, data)
                val elapsed = System.currentTimeMillis() - startTime
                Log.i(TAG, "✅ [ACTIVITY-RESULT] QR scan result handled in ${elapsed}ms")
            }
            VPN_PREPARE_REQUEST_CODE -> {
                val granted = resultCode == android.app.Activity.RESULT_OK
                vpnPermissionState = if (granted) 1 else -1
                Log.i(TAG, "🔐 [VPN] VpnService permission result: granted=$granted")
            }
            else -> {
                Log.d(TAG, "⏭️ [ACTIVITY-RESULT] Ignoring unknown request code: $requestCode")
            }
        }
    }

    fun requestVpnPermission() {
        vpnPermissionState = 0
        runOnUiThread {
            try {
                val intent = VpnService.prepare(this@MainActivity)
                if (intent == null) {
                    vpnPermissionState = 1
                    Log.i(TAG, "🔐 [VPN] Permission already granted")
                    return@runOnUiThread
                }
                Log.i(TAG, "🔐 [VPN] Requesting VpnService permission")
                startActivityForResult(intent, VPN_PREPARE_REQUEST_CODE)
            } catch (e: Exception) {
                vpnPermissionState = -1
                Log.e(TAG, "❌ [VPN] Failed to request VpnService permission", e)
            }
        }
    }

    fun openExternalUrl(rawUrl: String): Boolean {
        val latch = CountDownLatch(1)
        var opened = false

        runOnUiThread {
            try {
                val intent = Intent(Intent.ACTION_VIEW, Uri.parse(rawUrl)).apply {
                    addCategory(Intent.CATEGORY_BROWSABLE)
                }
                startActivity(intent)
                opened = true
                Log.i(TAG, "🌐 [BROWSER] External URL opened: $rawUrl")
            } catch (e: Exception) {
                Log.e(TAG, "❌ [BROWSER] Failed to open external URL: $rawUrl", e)
            } finally {
                latch.countDown()
            }
        }

        latch.await(5, TimeUnit.SECONDS)
        return opened
    }

    fun getActiveNetworkInterfaceName(): String? {
        return try {
            val connectivity = getSystemService(ConnectivityManager::class.java) ?: return null
            val activeNetwork = connectivity.activeNetwork ?: return null
            val linkProperties = connectivity.getLinkProperties(activeNetwork) ?: return null
            val ifName = linkProperties.interfaceName
            Log.i(TAG, "🌐 [NETWORK] Active interface: ${ifName ?: "null"}")
            ifName
        } catch (e: Exception) {
            Log.e(TAG, "❌ [NETWORK] Failed to get active interface name", e)
            null
        }
    }

    fun getInterfacesAsString(): String {
        return try {
            val lines = mutableListOf<String>()
            val interfaces = NetworkInterface.getNetworkInterfaces() ?: return ""
            while (interfaces.hasMoreElements()) {
                val iface = interfaces.nextElement() ?: continue
                val addrs = iface.interfaceAddresses
                    ?.mapNotNull { addr -> addr?.address?.hostAddress?.let { "$it/${addr.networkPrefixLength}" } }
                    ?.joinToString(" ")
                    ?: ""
                lines += listOf(
                    iface.name ?: "",
                    iface.index.toString(),
                    iface.mtu.toString(),
                    iface.isUp.toString(),
                    iface.supportsMulticast().toString(),
                    iface.isLoopback.toString(),
                    iface.isPointToPoint.toString(),
                    iface.supportsMulticast().toString(),
                ).joinToString(" ") + "|" + addrs
            }
            val result = lines.joinToString("\n")
            Log.i(TAG, "🌐 [NETWORK] Interfaces snapshot lines=${lines.size}")
            result
        } catch (e: Exception) {
            Log.e(TAG, "❌ [NETWORK] Failed to enumerate interfaces", e)
            ""
        }
    }

    fun requestLanguageReport() {
        runOnUiThread {
            reportLanguage()
        }
    }

    fun getVpnPermissionState(): Int = vpnPermissionState

    fun getNativeLibraryDir(): String {
        return applicationInfo.nativeLibraryDir
    }

    fun getFilesDirAbsolutePath(): String {
        return filesDir.absolutePath
    }

    fun getCacheDirAbsolutePath(): String {
        return cacheDir.absolutePath
    }

    /**
     * Called by Fyne's Android driver via JNI to open a file picker.
     * Fyne caches this method ID at startup and aborts if not found.
     * We launch a real SAF intent; the result is routed through NbdBridge.
     */
    fun showFileOpen(mimeType: String) {
        Log.i(TAG, "showFileOpen: mimeType=$mimeType")
        runOnUiThread {
            val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                addCategory(Intent.CATEGORY_OPENABLE)
                type = mimeType.ifEmpty { "*/*" }
                putExtra(Intent.EXTRA_MIME_TYPES, arrayOf(
                    "application/octet-stream",
                    "application/x-iso9660-image",
                    "application/x-raw-disk-image",
                    "application/x-cd-image"
                ))
            }
            try {
                startActivityForResult(intent, FYNE_FILE_OPEN_REQUEST_CODE)
            } catch (e: Exception) {
                Log.e(TAG, "showFileOpen: failed to start picker", e)
                NbdBridge.onSAFPickerError("File picker unavailable: ${e.message}")
            }
        }
    }

    /**
     * Called by Fyne's Android driver via JNI to open a file save dialog.
     * Fyne caches this method ID at startup and aborts if not found.
     */
    fun showFileSave(mimeType: String, filename: String) {
        Log.i(TAG, "showFileSave: mimeType=$mimeType, filename=$filename")
        runOnUiThread {
            val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                addCategory(Intent.CATEGORY_OPENABLE)
                type = mimeType.ifEmpty { "*/*" }
                putExtra(Intent.EXTRA_TITLE, filename)
            }
            try {
                startActivityForResult(intent, FYNE_FILE_SAVE_REQUEST_CODE)
            } catch (e: Exception) {
                Log.e(TAG, "showFileSave: failed to start saver", e)
            }
        }
    }

    /**
     * Start SAF file picker
     * Called from Kotlin or Go via JNI
     */
    fun startSAFPicker() {
        val callerThread = Thread.currentThread().name
        val callerThreadId = Thread.currentThread().id

        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "🚀 [SAF-PICKER-START] startSAFPicker() called")
        Log.i(TAG, "📍 [SAF-PICKER-START] Caller thread: $callerThread (ID: $callerThreadId)")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        runOnUiThread {
            val uiThread = Thread.currentThread().name
            val uiThreadId = Thread.currentThread().id
            val startTime = System.currentTimeMillis()

            Log.i(TAG, "🧵 [SAF-PICKER-UI] Running on UI thread: $uiThread (ID: $uiThreadId)")

            val intent = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                addCategory(Intent.CATEGORY_OPENABLE)
                type = "*/*"
                putExtra(Intent.EXTRA_MIME_TYPES, arrayOf(
                    "application/octet-stream",
                    "application/x-iso9660-image",
                    "application/x-raw-disk-image",
                    "application/x-cd-image"
                ))
            }

            Log.i(TAG, "📄 [SAF-PICKER-INTENT] Intent created:")
            Log.i(TAG, "📄 [SAF-PICKER-INTENT]   Action: ${intent.action}")
            Log.i(TAG, "📄 [SAF-PICKER-INTENT]   Type: ${intent.type}")
            Log.i(TAG, "📄 [SAF-PICKER-INTENT]   MIME types: ${intent.getStringArrayExtra(Intent.EXTRA_MIME_TYPES)?.joinToString()}")

            try {
                Log.i(TAG, "📤 [SAF-PICKER-LAUNCH] Calling startActivityForResult()...")
                startActivityForResult(intent, SAF_PICK_REQUEST_CODE)
                val elapsed = System.currentTimeMillis() - startTime
                Log.i(TAG, "✅ [SAF-PICKER-LAUNCH] SAF picker started successfully in ${elapsed}ms")
            } catch (e: Exception) {
                val elapsed = System.currentTimeMillis() - startTime
                Log.e(TAG, "❌ [SAF-PICKER-ERROR] Failed to start SAF picker after ${elapsed}ms")
                Log.e(TAG, "❌ [SAF-PICKER-ERROR] Exception: ${e.javaClass.simpleName}: ${e.message}", e)
                // Notify Go about error
                NbdBridge.onSAFPickerError("Failed to start SAF picker: ${e.message}")
            }
        }
    }

    /**
     * Launch QR code scanner - like Telegram
     * 1. Запрашивает разрешение CAMERA если нужно (Android 6+)
     * 2. Tries ZXing Barcode Scanner (in-app camera view) if installed
     * 3. Falls back to system camera to take photo if ZXing not available
     */
    override fun launchQRScanner() {
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "📷 [QR-SCANNER] launchQRScanner() called")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        runOnUiThread {
            try {
                // Проверяем и запрашиваем разрешение камеры (Android 6+)
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    when (ContextCompat.checkSelfPermission(this@MainActivity, Manifest.permission.CAMERA)) {
                        PackageManager.PERMISSION_GRANTED -> {
                            Log.i(TAG, "✅ [QR-SCANNER] Camera permission already granted")
                            doLaunchQRScanner()
                        }
                        else -> {
                            Log.i(TAG, "📋 [QR-SCANNER] Requesting CAMERA permission...")
                            ActivityCompat.requestPermissions(
                                this@MainActivity,
                                arrayOf(Manifest.permission.CAMERA),
                                CAMERA_PERMISSION_REQUEST_CODE
                            )
                        }
                    }
                } else {
                    doLaunchQRScanner()
                }
            } catch (e: Exception) {
                Log.e(TAG, "❌ [QR-SCANNER] Exception", e)
                callGoQRCancel()
            }
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        when (requestCode) {
            CAMERA_PERMISSION_REQUEST_CODE -> {
                if (grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                    Log.i(TAG, "✅ [QR-SCANNER] Camera permission granted")
                    doLaunchQRScanner()
                } else {
                    Log.w(TAG, "⚠️ [QR-SCANNER] Camera permission denied")
                    callGoQRCancel()
                }
            }
            CAMERA_LAUNCH_PERMISSION_REQUEST_CODE -> {
                if (grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED) {
                    doLaunchCamera()
                }
            }
        }
    }

    private fun doLaunchQRScanner() {
        try {
            // Встроенное окно с live viewfinder — наводишь камеру на QR, считывает автоматически (как в Telegram)
            Log.i(TAG, "✅ [QR-SCANNER] Launching in-app QR scanner (live viewfinder)...")
            startActivityForResult(Intent(this@MainActivity, QRScannerActivity::class.java), QR_SCAN_REQUEST_CODE)
        } catch (e: Exception) {
            Log.e(TAG, "❌ [QR-SCANNER] Exception", e)
            callGoQRCancel()
        }
    }

    /** Передаёт отмену в main app через JNI (без libgojni) */
    private fun callGoQRCancel() {
        try {
            QRResultBridge.deliverQRCancel()
        } catch (e: Exception) {
            Log.e(TAG, "❌ Error calling deliverQRCancel: ${e.message}", e)
        }
    }

    private fun handleQRScanResult(resultCode: Int, data: Intent?) {
        if (resultCode == android.app.Activity.RESULT_OK && data != null) {
            val contents = data.getStringExtra(QRScannerActivity.EXTRA_QR_CONTENTS)
            if (!contents.isNullOrBlank()) {
                Log.i(TAG, "✅ [QR-RESULT] Scanned: $contents")
                // Копируем строку чтобы она не зависела от Intent
                val contentsCopy = String(contents.toCharArray())
                callGoQRSuccess(contentsCopy)
            } else {
                Log.w(TAG, "⚠️ [QR-RESULT] Empty QR contents")
                callGoQRCancel()
            }
        } else {
            Log.w(TAG, "⚠️ [QR-RESULT] User cancelled or no data")
            callGoQRCancel()
        }
    }

    /** Передаёт результат в main app через JNI (без libgojni) */
    private fun callGoQRSuccess(contents: String) {
        try {
            QRResultBridge.deliverQRResult(contents)
        } catch (e: Exception) {
            Log.e(TAG, "❌ Error calling deliverQRResult: ${e.message}", e)
        }
    }

    /**
     * Launch camera to capture image
     * Called from Go via JNI
     */
    fun launchCamera() {
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "🚀 [CAMERA-LAUNCH] launchCamera() called")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        capturedImageData = null

        runOnUiThread {
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    when (ContextCompat.checkSelfPermission(this@MainActivity, Manifest.permission.CAMERA)) {
                        PackageManager.PERMISSION_GRANTED -> doLaunchCamera()
                        else -> ActivityCompat.requestPermissions(
                            this@MainActivity,
                            arrayOf(Manifest.permission.CAMERA),
                            CAMERA_LAUNCH_PERMISSION_REQUEST_CODE
                        )
                    }
                } else {
                    doLaunchCamera()
                }
            } catch (e: Exception) {
                Log.e(TAG, "❌ [CAMERA-LAUNCH] Exception", e)
            }
        }
    }

    private fun doLaunchCamera() {
        val cameraIntent = Intent(android.provider.MediaStore.ACTION_IMAGE_CAPTURE)
        if (cameraIntent.resolveActivity(packageManager) != null) {
            startActivityForResult(cameraIntent, CAMERA_REQUEST_CODE)
        } else {
            Log.e(TAG, "❌ [CAMERA-LAUNCH] No camera app available")
        }
    }

    /**
     * Handle camera result
     */
    private fun handleCameraResult(resultCode: Int, data: android.content.Intent?) {
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")
        Log.i(TAG, "📥 [CAMERA-RESULT] handleCameraResult called")
        Log.i(TAG, "📍 [CAMERA-RESULT] Result code: $resultCode")
        Log.i(TAG, "📍 [CAMERA-RESULT] Has data: ${data != null}")
        Log.i(TAG, "═══════════════════════════════════════════════════════════════")

        if (resultCode != android.app.Activity.RESULT_OK) {
            Log.w(TAG, "⚠️ [CAMERA-RESULT] Camera cancelled or failed")
            capturedImageData = null
            return
        }

        if (data == null) {
            Log.e(TAG, "❌ [CAMERA-RESULT] No data returned")
            capturedImageData = null
            return
        }

        try {
            val extras = data.extras
            val bitmap = extras?.get("data") as? android.graphics.Bitmap
            if (bitmap == null) {
                Log.e(TAG, "❌ [CAMERA-RESULT] No bitmap in extras")
                capturedImageData = null
                return
            }

            Log.i(TAG, "✅ [CAMERA-RESULT] Bitmap received: ${bitmap.width}x${bitmap.height}")
            val stream = java.io.ByteArrayOutputStream()
            bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, stream)
            capturedImageData = stream.toByteArray()
        } catch (e: Exception) {
            Log.e(TAG, "❌ [CAMERA-RESULT] Exception processing camera result", e)
            capturedImageData = null
        }
    }

    /**
     * Get captured image data
     * Called from Go via JNI
     */
    fun getCapturedImage(): ByteArray? {
        val data = capturedImageData
        Log.i(TAG, "📍 [CAMERA-GET] getCapturedImage() called, data size: ${data?.size ?: 0}")

        // Note: Don't clear data here as Go might call this multiple times while polling
        // Data will be cleared when launchCamera() is called again

        return data
    }

    /**
     * Clear captured image data
     * Called from Go via JNI after successfully receiving the image
     */
    fun clearCapturedImage() {
        Log.i(TAG, "🗑️ [CAMERA-CLEAR] Clearing captured image data")
        capturedImageData = null
    }
}
