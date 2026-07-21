package io.usbridge.client

import android.app.*
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import android.os.PowerManager
import android.util.Log
import androidx.core.app.NotificationCompat

/**
 * Foreground service for running NBD server in background
 * Keeps the NBD server alive even when app is in background
 */
class NbdForegroundService : Service() {

    companion object {
        private const val TAG = "NbdForegroundService"
        private const val NOTIFICATION_ID = 1001
        private const val CHANNEL_ID = "nbd_service_channel"
        private const val CHANNEL_NAME = "NBD Server"

        // Intent extras
        const val EXTRA_FD = "fd"
        const val EXTRA_SIZE = "size"
        const val EXTRA_ADDR = "addr"
        const val EXTRA_READ_ONLY = "read_only"

        // Actions
        const val ACTION_START_NBD = "io.usbridge.client.START_NBD"
        const val ACTION_STOP_NBD = "io.usbridge.client.STOP_NBD"

        /**
         * Start NBD service
         */
        fun startNbdService(context: Context, fd: Int, size: Long, addr: String, readOnly: Boolean = true) {
            val intent = Intent(context, NbdForegroundService::class.java).apply {
                action = ACTION_START_NBD
                putExtra(EXTRA_FD, fd)
                putExtra(EXTRA_SIZE, size)
                putExtra(EXTRA_ADDR, addr)
                putExtra(EXTRA_READ_ONLY, readOnly)
            }

            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        /**
         * Stop NBD service
         */
        fun stopNbdService(context: Context) {
            val intent = Intent(context, NbdForegroundService::class.java).apply {
                action = ACTION_STOP_NBD
            }
            context.startService(intent)
        }
    }

    private var wakeLock: PowerManager.WakeLock? = null

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "Service created")
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START_NBD -> {
                val fd = intent.getIntExtra(EXTRA_FD, -1)
                val size = intent.getLongExtra(EXTRA_SIZE, -1L)
                val addr = intent.getStringExtra(EXTRA_ADDR) ?: "127.0.0.1:10809"
                val readOnly = intent.getBooleanExtra(EXTRA_READ_ONLY, true)

                if (fd < 0 || size < 0) {
                    Log.e(TAG, "Invalid parameters: fd=$fd, size=$size")
                    stopSelf()
                    return START_NOT_STICKY
                }

                startNbd(fd, size, addr, readOnly)
            }
            ACTION_STOP_NBD -> {
                stopNbd()
                stopSelf()
            }
        }

        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onDestroy() {
        Log.i(TAG, "Service destroying")
        stopNbd()
        releaseWakeLock()
        super.onDestroy()
    }

    /**
     * Start NBD server and move to foreground
     */
    private fun startNbd(fd: Int, size: Long, addr: String, readOnly: Boolean) {
        Log.i(TAG, "Starting NBD server: fd=$fd, size=$size, addr=$addr, readOnly=$readOnly")

        // Acquire wake lock to keep CPU running
        acquireWakeLock()

        // Start foreground notification
        val notification = createNotification(addr, "running")
        startForeground(NOTIFICATION_ID, notification)

        // Start NBD in background thread
        Thread {
            try {
                val result = NbdBridge.startNbd(fd, size, addr, readOnly)
                if (result == 0) {
                    Log.i(TAG, "NBD server started successfully")
                    updateNotification(addr, "serving")
                } else {
                    Log.e(TAG, "Failed to start NBD server: error code $result")
                    updateNotification(addr, "error")
                    // Stop service after short delay
                    Thread.sleep(3000)
                    stopSelf()
                }
            } catch (e: Exception) {
                Log.e(TAG, "Exception starting NBD: $e", e)
                updateNotification(addr, "error: $e")
                Thread.sleep(3000)
                stopSelf()
            }
        }.start()
    }

    /**
     * Stop NBD server
     */
    private fun stopNbd() {
        Log.i(TAG, "Stopping NBD server")
        try {
            NbdBridge.stopNbd()
            Log.i(TAG, "NBD server stopped")
        } catch (e: Exception) {
            Log.e(TAG, "Error stopping NBD: $e", e)
        }
        releaseWakeLock()
    }

    /**
     * Acquire wake lock to keep CPU running in background
     */
    private fun acquireWakeLock() {
        if (wakeLock == null) {
            val powerManager = getSystemService(Context.POWER_SERVICE) as PowerManager
            wakeLock = powerManager.newWakeLock(
                PowerManager.PARTIAL_WAKE_LOCK,
                "NbdService::WakeLock"
            )
            wakeLock?.acquire(10 * 60 * 60 * 1000L) // 10 hours max
            Log.i(TAG, "Wake lock acquired")
        }
    }

    /**
     * Release wake lock
     */
    private fun releaseWakeLock() {
        wakeLock?.let {
            if (it.isHeld) {
                it.release()
                Log.i(TAG, "Wake lock released")
            }
            wakeLock = null
        }
    }

    /**
     * Create notification channel (Android O+)
     */
    private fun createNotificationChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                CHANNEL_NAME,
                NotificationManager.IMPORTANCE_LOW
            ).apply {
                description = "NBD Server Status"
                setShowBadge(false)
            }

            val notificationManager = getSystemService(NotificationManager::class.java)
            notificationManager.createNotificationChannel(channel)
            Log.i(TAG, "Notification channel created")
        }
    }

    /**
     * Create notification
     */
    private fun createNotification(addr: String, status: String): Notification {
        val port = addr.substringAfterLast(':')

        // Intent to open app when notification is clicked
        val pendingIntent = packageManager.getLaunchIntentForPackage(packageName)?.let { intent ->
            PendingIntent.getActivity(
                this,
                0,
                intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
        }

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("NBD Server")
            .setContentText("$status on port $port")
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .setContentIntent(pendingIntent)
            .build()
    }

    /**
     * Update notification with new status
     */
    private fun updateNotification(addr: String, status: String) {
        val notification = createNotification(addr, status)
        val notificationManager = getSystemService(NotificationManager::class.java)
        notificationManager.notify(NOTIFICATION_ID, notification)
    }
}
