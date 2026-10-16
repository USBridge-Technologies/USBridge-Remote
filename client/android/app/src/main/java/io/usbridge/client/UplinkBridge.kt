package io.usbridge.client

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.midi.MidiDevice
import android.media.midi.MidiDeviceInfo
import android.media.midi.MidiManager
import android.media.midi.MidiOutputPort
import android.media.midi.MidiReceiver
import android.net.Uri
import android.os.Handler
import android.os.HandlerThread
import android.provider.Settings
import android.util.Log
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import java.util.concurrent.ConcurrentHashMap

/**
 * The Android half of the microphone and MIDI uplink (Go:
 * internal/platform/uplink_android.go). The microphone itself is AAudio in
 * native code; here only its permission. MIDI inputs come through
 * android.media.midi: what a device plays is handed to native code by
 * nativeMidiData under the handle the capture was opened with.
 */
object UplinkBridge {
    private const val TAG = "UplinkBridge"
    private const val MIC_PERMISSION_REQUEST_CODE = 5101

    init {
        try {
            System.loadLibrary("USBridge_Client")
        } catch (e: UnsatisfiedLinkError) {
            Log.e(TAG, "Failed to load USBridge_Client", e)
        }
    }

    @JvmStatic
    external fun nativeMidiData(handle: Int, data: ByteArray, offset: Int, count: Int)

    @JvmStatic
    external fun nativeMicPermissionResult(granted: Boolean)

    private class OpenInput(val device: MidiDevice, val port: MidiOutputPort)

    private val inputs = ConcurrentHashMap<Int, OpenInput>()
    private val wanted = ConcurrentHashMap.newKeySet<Int>()
    private val handler: Handler by lazy {
        Handler(HandlerThread("usbridge-midi").apply { start() }.looper)
    }

    /** Whether RECORD_AUDIO is granted; asks for it when not and [request]. */
    @JvmStatic
    fun micPermission(activity: Activity, request: Boolean): Boolean {
        if (ContextCompat.checkSelfPermission(activity, Manifest.permission.RECORD_AUDIO) ==
            PackageManager.PERMISSION_GRANTED
        ) return true
        if (request) {
            activity.runOnUiThread {
                ActivityCompat.requestPermissions(
                    activity, arrayOf(Manifest.permission.RECORD_AUDIO), MIC_PERMISSION_REQUEST_CODE
                )
            }
        }
        return false
    }

    /** MainActivity.onRequestPermissionsResult: true when it was the microphone's request. */
    @JvmStatic
    fun onPermissionResult(requestCode: Int, grantResults: IntArray): Boolean {
        if (requestCode != MIC_PERMISSION_REQUEST_CODE) return false
        val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
        Log.i(TAG, "RECORD_AUDIO granted=$granted")
        nativeMicPermissionResult(granted)
        return true
    }

    /** The app's page in Settings, where a microphone refused for good is allowed again. */
    @JvmStatic
    fun openAppSettings(activity: Activity) {
        activity.runOnUiThread {
            try {
                activity.startActivity(
                    Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", activity.packageName, null))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                )
            } catch (e: Exception) {
                Log.w(TAG, "app settings could not be opened", e)
            }
        }
    }

    private fun midiManager(ctx: Context): MidiManager? {
        if (!ctx.packageManager.hasSystemFeature(PackageManager.FEATURE_MIDI)) return null
        return ctx.getSystemService(Context.MIDI_SERVICE) as? MidiManager
    }

    /** Every MIDI output port (what a device plays), one "id\tname" line each; id is "device:port". */
    @JvmStatic
    fun listMidiInputs(ctx: Context): String {
        val mm = midiManager(ctx) ?: return ""
        val sb = StringBuilder()
        @Suppress("DEPRECATION")
        for (info in mm.devices) {
            val name = info.properties.getString(MidiDeviceInfo.PROPERTY_NAME)
                ?: info.properties.getString(MidiDeviceInfo.PROPERTY_PRODUCT)
                ?: "MIDI ${info.id}"
            for (port in info.ports) {
                if (port.type != MidiDeviceInfo.PortInfo.TYPE_OUTPUT) continue
                val label = when {
                    info.outputPortCount <= 1 -> name
                    port.name.isNotBlank() -> "$name ${port.name}"
                    else -> "$name ${port.portNumber + 1}"
                }
                sb.append(info.id).append(':').append(port.portNumber).append('\t')
                    .append(label.replace('\t', ' ').replace('\n', ' ')).append('\n')
            }
        }
        return sb.toString()
    }

    /** Opens the port [id] (see listMidiInputs); false when it is gone. The device opens asynchronously. */
    @JvmStatic
    fun openMidiInput(ctx: Context, id: String, handle: Int): Boolean {
        val mm = midiManager(ctx) ?: return false
        val deviceId = id.substringBefore(':').toIntOrNull() ?: return false
        val portNumber = id.substringAfter(':').toIntOrNull() ?: return false
        @Suppress("DEPRECATION")
        val info = mm.devices.firstOrNull { it.id == deviceId } ?: return false
        wanted.add(handle)
        mm.openDevice(info, { device ->
            if (device == null) {
                Log.w(TAG, "MIDI device $id could not be opened")
                return@openDevice
            }
            val port = device.openOutputPort(portNumber)
            if (port == null || !wanted.contains(handle)) {
                port?.close()
                device.close()
                return@openDevice
            }
            port.connect(object : MidiReceiver() {
                override fun onSend(msg: ByteArray, offset: Int, count: Int, timestamp: Long) {
                    if (count > 0) nativeMidiData(handle, msg, offset, count)
                }
            })
            inputs[handle] = OpenInput(device, port)
        }, handler)
        return true
    }

    @JvmStatic
    fun closeMidiInput(handle: Int) {
        wanted.remove(handle)
        // Runs after a pending openDevice callback, which then sees it unwanted.
        handler.post {
            inputs.remove(handle)?.let {
                it.port.close()
                it.device.close()
            }
        }
    }
}
