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

    private var viewportGestureActive = false
    private var lastViewportCentroidX = 0f
    private var lastViewportCentroidY = 0f
    private var lastViewportDistance = 0f

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


    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        instance = this
        Log.i(TAG, "MainActivity created")
        setupIMEListener()
        
        connectivityManager = getSystemService(ConnectivityManager::class.java)
        connectivityManager.registerDefaultNetworkCallback(networkCallback)
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

            val imeHeight = (screenHeight - rect.bottom).coerceAtLeast(0)

            if (imeHeight != lastImeHeightPx) {
                val wasVisible = lastImeHeightPx > 0
                lastImeHeightPx = imeHeight
                Log.d(TAG, "⌨️ [IME] height changed: imeHeight=$imeHeight screenHeight=$screenHeight")

                if (imeHeight == 0 && wasVisible) {
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
            connectivityManager.unregisterNetworkCallback(networkCallback)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to unregister network callback", e)
        }
        endViewportGesture()
        super.onDestroy()
        instance = null
        Log.i(TAG, "MainActivity destroyed")
    }

    override fun dispatchTouchEvent(ev: MotionEvent): Boolean {
        handleViewportGesture(ev)
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

    private fun handleViewportGesture(ev: MotionEvent) {
        when (ev.actionMasked) {
            MotionEvent.ACTION_DOWN, MotionEvent.ACTION_CANCEL, MotionEvent.ACTION_UP -> {
                if (ev.pointerCount < 2) {
                    endViewportGesture()
                }
            }
            MotionEvent.ACTION_POINTER_DOWN -> {
                if (ev.pointerCount >= 2) {
                    startViewportGesture(ev)
                }
            }
            MotionEvent.ACTION_MOVE -> {
                if (ev.pointerCount >= 2) {
                    updateViewportGesture(ev)
                } else {
                    endViewportGesture()
                }
            }
            MotionEvent.ACTION_POINTER_UP -> {
                if (ev.pointerCount <= 2) {
                    endViewportGesture()
                } else {
                    startViewportGesture(ev)
                }
            }
        }
    }

    private fun startViewportGesture(ev: MotionEvent) {
        val currentDistance = distanceBetweenPointers(ev)
        val currentCentroidX = centroidX(ev)
        val currentCentroidY = centroidY(ev)
        lastViewportDistance = currentDistance
        lastViewportCentroidX = currentCentroidX
        lastViewportCentroidY = currentCentroidY
        if (!viewportGestureActive) {
            viewportGestureActive = true
            GestureBridge.onViewportGestureStateChanged(true)
        }
    }

    private fun updateViewportGesture(ev: MotionEvent) {
        if (!viewportGestureActive) {
            startViewportGesture(ev)
            return
        }

        val currentDistance = distanceBetweenPointers(ev)
        val currentCentroidX = centroidX(ev)
        val currentCentroidY = centroidY(ev)

        val scaleFactor = if (lastViewportDistance > 0f && currentDistance > 0f) {
            currentDistance / lastViewportDistance
        } else {
            1f
        }
        val panDx = currentCentroidX - lastViewportCentroidX
        val panDy = currentCentroidY - lastViewportCentroidY

        lastViewportDistance = currentDistance
        lastViewportCentroidX = currentCentroidX
        lastViewportCentroidY = currentCentroidY

        GestureBridge.onViewportGestureUpdate(
            scaleFactor,
            currentCentroidX,
            currentCentroidY,
            panDx,
            panDy,
        )
    }

    private fun endViewportGesture() {
        lastViewportDistance = 0f
        lastViewportCentroidX = 0f
        lastViewportCentroidY = 0f
        if (viewportGestureActive) {
            viewportGestureActive = false
            GestureBridge.onViewportGestureStateChanged(false)
        }
    }

    private fun distanceBetweenPointers(ev: MotionEvent): Float {
        if (ev.pointerCount < 2) {
            return 0f
        }
        val dx = ev.getX(1) - ev.getX(0)
        val dy = ev.getY(1) - ev.getY(0)
        return kotlin.math.sqrt(dx * dx + dy * dy)
    }

    private fun centroidX(ev: MotionEvent): Float {
        if (ev.pointerCount < 2) {
            return ev.x
        }
        return (ev.getX(0) + ev.getX(1)) / 2f
    }

    private fun centroidY(ev: MotionEvent): Float {
        if (ev.pointerCount < 2) {
            return ev.y
        }
        return (ev.getY(0) + ev.getY(1)) / 2f
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
