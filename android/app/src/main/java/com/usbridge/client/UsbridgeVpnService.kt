package com.usbridge.client

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.IBinder
import android.system.OsConstants
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat

class UsbridgeVpnService : VpnService() {
    override fun onCreate() {
        super.onCreate()
        instance = this
        startForeground(NOTIFICATION_ID, buildNotification())
        Log.i(TAG, "UsbridgeVpnService created")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.i(TAG, "UsbridgeVpnService started")
        return START_STICKY
    }

    override fun onDestroy() {
        instance = null
        Log.i(TAG, "UsbridgeVpnService destroyed")
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? {
        return super.onBind(intent)
    }

    private fun buildNotification(): Notification {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "USBridge VPN",
                NotificationManager.IMPORTANCE_LOW
            )
            val notificationManager = getSystemService(NotificationManager::class.java)
            notificationManager?.createNotificationChannel(channel)
        }

        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_warning)
            .setContentTitle("USBridge VPN")
            .setContentText("WireGuard tunnel is active")
            .setOngoing(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .build()
    }

    companion object {
        private const val TAG = "UsbridgeVpnService"
        private const val CHANNEL_ID = "usbridge_vpn"
        private const val NOTIFICATION_ID = 2001

        @Volatile
        private var instance: UsbridgeVpnService? = null

        @JvmStatic
        fun startVpnService(context: Context) {
            val intent = Intent(context, UsbridgeVpnService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                ContextCompat.startForegroundService(context, intent)
            } else {
                context.startService(intent)
            }
        }

        @JvmStatic
        fun isServiceReady(): Boolean = instance != null

        @JvmStatic
        fun establishTunnel(sessionName: String, clientAddressCidr: String, routes: String, mtu: Int): Int {
            val service = instance ?: return -1
            val builder = service.Builder()
            builder.setSession(sessionName)
            builder.setMtu(mtu)
            builder.setBlocking(true)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                builder.setMetered(false)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                service.setUnderlyingNetworks(null)
            }

            val (clientAddress, clientPrefix) = parseCidr(clientAddressCidr) ?: return -1
            builder.addAddress(clientAddress, clientPrefix)

            var hasDefaultRoute = false
            for (rawRoute in routes.split('\n')) {
                val route = rawRoute.trim()
                if (route.isEmpty()) {
                    continue
                }
                val (address, prefix) = parseCidr(route) ?: return -1
                if (prefix == 0) {
                    hasDefaultRoute = true
                }
                builder.addRoute(address, prefix)
            }

            if (!hasDefaultRoute) {
                builder.allowFamily(OsConstants.AF_INET)
                builder.allowFamily(OsConstants.AF_INET6)
            }

            val tun = builder.establish() ?: return -1
            return tun.detachFd()
        }

        @JvmStatic
        fun protectSocket(fd: Int): Boolean {
            val service = instance ?: return false
            return service.protect(fd)
        }

        @JvmStatic
        fun stopVpnService() {
            instance?.stopSelf()
        }

        private fun parseCidr(cidr: String): Pair<String, Int>? {
            val trimmed = cidr.trim()
            val slashIndex = trimmed.lastIndexOf('/')
            if (slashIndex <= 0 || slashIndex == trimmed.length - 1) {
                Log.e(TAG, "Invalid CIDR: $trimmed")
                return null
            }
            val address = trimmed.substring(0, slashIndex)
            val prefix = trimmed.substring(slashIndex + 1).toIntOrNull()
            if (prefix == null) {
                Log.e(TAG, "Invalid CIDR prefix: $trimmed")
                return null
            }
            return address to prefix
        }
    }
}
