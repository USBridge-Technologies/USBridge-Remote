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
import android.view.KeyEvent
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

        /**
         * Called by Fyne's Android driver via JNI to open a file picker.
         * Fyne resolves this with GetStaticMethodID (see android.c in
         * fyne.io/fyne/v2/internal/driver/mobile/app) and aborts the whole
         * process the first time it invokes a method it couldn't resolve —
         * so this MUST be a static method matching that signature exactly,
         * not an instance method. Confirmed by device testing
         * (tests/test_android_ui_stress.sh): as an instance method the JNI
         * lookup failed silently at startup ("cannot find method showFileOpen
         * (Ljava/lang/String;)V"), which is otherwise invisible until
         * something actually triggers the file picker.
         */
        @JvmStatic
        fun showFileOpen(mimeType: String) {
            val activity = instance ?: return
            Log.i(TAG, "showFileOpen: mimeType=$mimeType")
            activity.runOnUiThread {
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
                    activity.startActivityForResult(intent, FYNE_FILE_OPEN_REQUEST_CODE)
                } catch (e: Exception) {
                    Log.e(TAG, "showFileOpen: failed to start picker", e)
                    NbdBridge.onSAFPickerError("File picker unavailable: ${e.message}")
                }
            }
        }

        /** Same static-binding requirement as showFileOpen above. */
        @JvmStatic
        fun showFileSave(mimeType: String, filename: String) {
            val activity = instance ?: return
            Log.i(TAG, "showFileSave: mimeType=$mimeType, filename=$filename")
            activity.runOnUiThread {
                val intent = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE)
                    type = mimeType.ifEmpty { "*/*" }
                    putExtra(Intent.EXTRA_TITLE, filename)
                }
                try {
                    activity.startActivityForResult(intent, FYNE_FILE_SAVE_REQUEST_CODE)
                } catch (e: Exception) {
                    Log.e(TAG, "showFileSave: failed to start saver", e)
                }
            }
        }
    }

    /**
     * Called by Fyne's Android driver via JNI (instance GetMethodID) when it
     * needs to terminate the activity — e.g. the system back button routes
     * through Fyne's native code, which calls this to close the window.
     * This method did not exist at all before this fix: Fyne's JNI lookup
     * failed silently at startup ("cannot find method finishActivity ()V")
     * and the first time anything actually invoked it (reproduced 100% of
     * the time on the first back-button press in
     * tests/test_android_ui_stress.sh, phase 4) the JNI runtime aborted the
     * whole process with "mid == null" — a hard native crash, not a graceful
     * exit.
     */
    fun finishActivity() {
        finish()
    }

    // Camera capture result
    @Volatile
    private var capturedImageData: ByteArray? = null

    @Volatile
    private var vpnPermissionState: Int = 0

    private val gyroSensorManager: GyroSensorManager by lazy { GyroSensorManager(this) }

    // Two-finger gesture tracker — mode (PAN_ZOOM vs SCROLL) is locked at gesture start.
    // Threshold is 30% of the smaller screen dimension: fingers closer than this → scroll
    // wheel only, farther apart → pan+zoom (resize) only. The two never fire together.
    private val gestureTracker: TwoFingerGestureTracker by lazy {
        val dm = resources.displayMetrics
        val minDimensionPx = minOf(dm.widthPixels, dm.heightPixels)
        TwoFingerGestureTracker(
            panZoomThresholdPx = minDimensionPx * 0.30f,
            onActiveChanged = { active ->
                GestureBridge.onViewportGestureStateChanged(active)
            },
            onPanZoom = { scale, focusX, focusY, dx, dy ->
                GestureBridge.onViewportGestureUpdate(scale, focusX, focusY, dx, dy)
            },
            onScroll = { scrollDy ->
                GestureBridge.onScrollGesture(scrollDy)
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

        gyroSensorManager.start()

        clipboardManager.addPrimaryClipChangedListener { clipChangeCount++ }
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
        gyroSensorManager.stop()
        gestureTracker.cancel()
        super.onDestroy()
        instance = null
        Log.i(TAG, "MainActivity destroyed")
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        // In GyroMouse mode intercept volume buttons as LMB/RMB instead of changing volume.
        if (GyroBridge.isGyroMouseModeActive()) {
            when (event.keyCode) {
                KeyEvent.KEYCODE_VOLUME_UP -> {
                    GyroBridge.onVolumeButton(1, event.action == KeyEvent.ACTION_DOWN)
                    return true
                }
                KeyEvent.KEYCODE_VOLUME_DOWN -> {
                    GyroBridge.onVolumeButton(2, event.action == KeyEvent.ACTION_DOWN)
                    return true
                }
            }
        }
        return super.dispatchKeyEvent(event)
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

    // Binds a raw native socket fd to whichever active network can actually
    // route to destHost. Plain source-IP binding (Go's net.Dialer.LocalAddr)
    // picks an address but Android still routes the socket through whatever
    // network ConnectivityManager assigned to this app's UID -- e.g. mobile
    // data, even with a Wi-Fi-only address bound -- so a LAN destination on
    // Wi-Fi gets ENETUNREACH the moment mobile data is also up. This is the
    // Android equivalent of macOS's IP_BOUND_IF trick in direct_dialer_darwin.go.
    //
    // FileDescriptor's backing int can only be set reflectively -- there's no
    // public constructor/setter for an arbitrary fd -- but Network.bindSocket
    // only reads that int to do the actual (kernel-level) binding, so this
    // doesn't take ownership of or duplicate the fd; the Go side still owns
    // and closes it normally.
    fun bindSocketToBestNetwork(fd: Int, destHost: String): Boolean {
        return try {
            val connectivity = getSystemService(ConnectivityManager::class.java) ?: return false
            val destAddr = try {
                java.net.InetAddress.getByName(destHost)
            } catch (e: Exception) {
                null
            }

            var target: android.net.Network? = null
            var wifiFallback: android.net.Network? = null
            for (network in connectivity.allNetworks) {
                val caps = connectivity.getNetworkCapabilities(network) ?: continue
                if (!caps.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET)) continue
                val linkProperties = connectivity.getLinkProperties(network)
                // Match against the network's own assigned subnet (address +
                // prefix length), not LinkProperties.routes -- RouteInfo.matches()
                // also matches a plain default route (0.0.0.0/0), which every
                // network with internet access has, so it would happily "match"
                // a private LAN destination on the mobile network too and pick
                // whichever network happened to be listed first.
                if (destAddr != null && linkProperties != null &&
                    linkProperties.linkAddresses.any { isInSameSubnet(it.address, it.prefixLength, destAddr) }
                ) {
                    target = network
                    break
                }
                if (wifiFallback == null && caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)) {
                    wifiFallback = network
                }
            }
            if (target == null) target = wifiFallback
            if (target == null) {
                Log.w(TAG, "🔗 [NETBIND] No network with a route to $destHost")
                return false
            }

            val fdField = java.io.FileDescriptor::class.java.getDeclaredField("descriptor")
            fdField.isAccessible = true
            val fileDescriptor = java.io.FileDescriptor()
            fdField.set(fileDescriptor, fd)

            target.bindSocket(fileDescriptor)
            Log.i(TAG, "🔗 [NETBIND] Bound fd=$fd to network for dest=$destHost")
            true
        } catch (e: Exception) {
            Log.e(TAG, "❌ [NETBIND] Failed to bind fd=$fd for dest=$destHost", e)
            false
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

    // ---- Clipboard bridge ----
    // Called from Go via JNI, see internal/clipboard/backend_android.go.
    // Note: since Android 10 (API 29) the platform blocks getPrimaryClip()
    // for apps that aren't in the foreground (privacy restriction) — reads
    // will just see an empty/unchanged clipboard while backgrounded, same as
    // every other remote-clipboard app on Android.

    private val clipboardManager: android.content.ClipboardManager by lazy {
        getSystemService(android.content.ClipboardManager::class.java)
    }

    @Volatile private var clipChangeCount: Int = 0
    @Volatile private var clipReadText: String = ""
    @Volatile private var clipReadMime: String = ""
    @Volatile private var clipReadName: String = ""
    @Volatile private var clipReadFd: Int = -1

    fun clipboardChangeCount(): Int = clipChangeCount

    /**
     * Resolves the current primary clip and returns "text"|"image"|"file"|
     * "none"|"blocked". For image/file kinds, clipboardReadFd() returns a
     * detached fd (transferred to native code, same detachFd() pattern as
     * NbdBridge.handleActivityResult) that the Go side reads to EOF and
     * closes; for text, clipboardReadText() carries it directly.
     *
     * "blocked" vs "none": Android 10+ returns a null primaryClip for any app
     * that isn't currently focused, indistinguishable from a genuinely empty
     * clipboard. clipChangeCount (see addPrimaryClipChangedListener above)
     * still increments correctly while backgrounded though, so if Go treated
     * a focus-gated null the same as "genuinely nothing to sync" it would
     * mark that change-stamp as handled and never come back to it — the
     * clipboard update that happened while USBridge was in the background
     * would be lost for good instead of picked up next time it's read
     * successfully. Reporting "blocked" instead lets the Go side (see
     * manager.go's Run loop) treat this as a transient read failure and
     * retry the same stamp later, instead of silently dropping it.
     */
    fun clipboardRead(): String {
        return try {
            val clip = clipboardManager.primaryClip
            if (clip == null || clip.itemCount == 0) {
                return if (!hasWindowFocus()) "blocked" else "none"
            }
            val item = clip.getItemAt(0)
            val uri = item.uri
            if (uri != null) {
                val mimeType = clip.description
                    ?.takeIf { it.mimeTypeCount > 0 }
                    ?.getMimeType(0)
                    ?: contentResolver.getType(uri)
                    ?: "application/octet-stream"
                val pfd = contentResolver.openFileDescriptor(uri, "r") ?: return "none"
                clipReadFd = pfd.detachFd()
                clipReadName = queryDisplayName(uri) ?: uri.lastPathSegment ?: "file"
                clipReadMime = mimeType
                return if (mimeType.startsWith("image/")) "image" else "file"
            }
            val text = item.coerceToText(this)?.toString()
            if (!text.isNullOrEmpty()) {
                clipReadText = text
                return "text"
            }
            "none"
        } catch (e: Exception) {
            Log.e(TAG, "clipboardRead failed", e)
            "none"
        }
    }

    fun clipboardReadText(): String = clipReadText
    fun clipboardReadMime(): String = clipReadMime
    fun clipboardReadFileName(): String = clipReadName
    fun clipboardReadFd(): Int = clipReadFd

    fun clipboardSetText(text: String): Boolean {
        return try {
            clipboardManager.setPrimaryClip(android.content.ClipData.newPlainText("USBridge", text))
            true
        } catch (e: Exception) {
            Log.e(TAG, "clipboardSetText failed", e)
            false
        }
    }

    /**
     * path must already be a file under this app's cache dir (the Go
     * clipboard backend writes the peer's bytes there first — see
     * file_paths.xml's cache-path covering the whole cache dir). Exposing it
     * as a content:// URI through the existing FileProvider lets Android
     * automatically extend a read-permission grant to whichever app performs
     * the paste (FileProvider's grantUriPermissions="true" in the manifest
     * is what makes that automatic grant possible).
     */
    fun clipboardSetFile(path: String, mimeType: String): Boolean {
        return try {
            val file = java.io.File(path)
            val uri = androidx.core.content.FileProvider.getUriForFile(
                this, "com.usbridge.client.fileprovider", file
            )
            val clip = android.content.ClipData(
                file.name,
                arrayOf(mimeType),
                android.content.ClipData.Item(uri)
            )
            clipboardManager.setPrimaryClip(clip)
            true
        } catch (e: Exception) {
            Log.e(TAG, "clipboardSetFile failed", e)
            false
        }
    }

    private fun queryDisplayName(uri: Uri): String? {
        return try {
            contentResolver.query(uri, null, null, null, null)?.use { cursor ->
                val idx = cursor.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME)
                if (idx >= 0 && cursor.moveToFirst()) cursor.getString(idx) else null
            }
        } catch (e: Exception) {
            null
        }
    }
}
