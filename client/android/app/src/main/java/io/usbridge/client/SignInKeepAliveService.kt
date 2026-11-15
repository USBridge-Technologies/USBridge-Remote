package io.usbridge.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import androidx.core.app.NotificationCompat

/**
 * Keeps the app running while the Tailscale sign-in is open in a Custom Tab.
 * With the Custom Tab on top the app is in the background, and the OS
 * (Samsung's freezer in particular) froze it and cut its network: tsnet never
 * heard the sign-in finish, so the tab stayed open on the Tailscale dashboard
 * until the user came back by hand. Stopped once the sign-in is done or the
 * user returns to the app; gives up on its own after 10 minutes.
 */
class SignInKeepAliveService : Service() {

    companion object {
        private const val TAG = "SignInKeepAlive"
        private const val NOTIFICATION_ID = 1002
        private const val CHANNEL_ID = "sign_in_channel"
        private const val MAX_MS = 10 * 60 * 1000L

        // Set by start, cleared once in the foreground: stopping a service
        // started with startForegroundService before it called
        // startForeground crashes the app, so such a stop waits for it.
        @Volatile private var starting = false
        @Volatile private var stopRequested = false

        fun start(context: Context) {
            starting = true
            stopRequested = false
            try {
                context.startForegroundService(Intent(context, SignInKeepAliveService::class.java))
            } catch (e: Exception) {
                Log.w(TAG, "could not start", e)
            }
        }

        fun stop(context: Context) {
            if (starting) {
                stopRequested = true
                return
            }
            try {
                context.stopService(Intent(context, SignInKeepAliveService::class.java))
            } catch (e: Exception) {
                Log.w(TAG, "could not stop", e)
            }
        }
    }

    private val handler = Handler(Looper.getMainLooper())

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "Sign-in", NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            }
        )
        val notification: Notification = NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Signing in…")
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .build()
        try {
            if (Build.VERSION.SDK_INT >= 29) {
                startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(NOTIFICATION_ID, notification)
            }
            Log.i(TAG, "keeping the app alive during the sign-in")
        } catch (e: Exception) {
            Log.w(TAG, "startForeground failed", e)
            starting = false
            stopSelf()
            return START_NOT_STICKY
        }
        starting = false
        if (stopRequested) {
            stopSelf()
            return START_NOT_STICKY
        }
        handler.removeCallbacksAndMessages(null)
        handler.postDelayed({ stopSelf() }, MAX_MS)
        return START_NOT_STICKY
    }

    override fun onTimeout(startId: Int, fgsType: Int) {
        stopSelf()
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        Log.i(TAG, "stopped")
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null
}
