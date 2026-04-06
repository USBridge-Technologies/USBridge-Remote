package org.golang.app

import android.app.NativeActivity
import android.os.Bundle
import android.system.Os
import android.util.Log
import java.io.File

// Compatibility shim for builds where gomobile bind does not package the
// org.golang.app runtime classes into the consumed AAR.
open class GoNativeActivity : NativeActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        configureGStreamerRuntime()
        preloadNativeRuntime()
        super.onCreate(savedInstanceState)
    }

    open fun launchQRScanner() = Unit

    private fun configureGStreamerRuntime() {
        val nativeDir = applicationInfo?.nativeLibraryDir ?: return
        val registryPath = File(codeCacheDir, "gstreamer-registry.bin").absolutePath
        setEnvIfPossible("GST_PLUGIN_PATH", nativeDir)
        setEnvIfPossible("GST_PLUGIN_SYSTEM_PATH", nativeDir)
        setEnvIfPossible("GST_REGISTRY", registryPath)
        Log.i(TAG, "Configured GStreamer runtime in $nativeDir")
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
            try {
                System.load(file.absolutePath)
                Log.d(TAG, "Preloaded ${file.name}")
            } catch (e: UnsatisfiedLinkError) {
                Log.w(TAG, "Failed to preload ${file.name}", e)
            }
        }
    }

    private fun setEnvIfPossible(name: String, value: String) {
        try {
            Os.setenv(name, value, true)
        } catch (t: Throwable) {
            Log.w(TAG, "Failed to set $name", t)
        }
    }

    private fun libraryLoadRank(name: String): Int = when (name) {
        "libc++_shared.so" -> 0
        "libintl.so", "libffi.so", "libsqlite3.so", "libxml2.so", "libpsl.so", "libjpeg.so", "libopenjp2.so", "libmicrodns.so" -> 10
        "libglib-2.0.so", "libgmodule-2.0.so", "libgobject-2.0.so", "libgthread-2.0.so", "libgio-2.0.so" -> 20
        "libgraphene-1.0.so", "libsoup-2.4.so", "libsoup-gnome-2.4.so" -> 30
        "libavutil.so", "libswresample.so", "libswscale.so", "libavcodec.so", "libavformat.so", "libavfilter.so", "libavdevice.so", "libpostproc.so", "libavresample.so" -> 40
        "libgstreamer-1.0.so", "libgstbase-1.0.so", "libgstallocators-1.0.so", "libgstcontroller-1.0.so", "libgstnet-1.0.so", "libgstapp-1.0.so", "libgstvideo-1.0.so", "libgstaudio-1.0.so", "libgstpbutils-1.0.so", "libgstrtp-1.0.so", "libgstrtsp-1.0.so", "libgstsdp-1.0.so", "libgsttag-1.0.so", "libgstgl-1.0.so" -> 50
        "libgojni.so" -> 80
        else -> when {
            name.startsWith("libgst") -> 60
            name.startsWith("libav") || name.startsWith("libsw") -> 45
            else -> 70
        }
    }

    companion object {
        private const val TAG = "GoNativeActivity"
        private const val APP_LIBRARY = "libUSBridge_Client.so"
    }
}
