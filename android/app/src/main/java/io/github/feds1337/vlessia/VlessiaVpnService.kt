package io.github.feds1337.vlessia

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import io.github.feds1337.vlessia.mobile.Mobile
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Holds the VPN interface while connected. The Go core asks for the
 * interface through [establish] and hands it to Xray; the app itself is
 * left out of the VPN, so Xray's connections to the servers bypass it.
 *
 * The system also starts this service on its own for always-on VPN, and
 * then it connects to the selected server.
 */
class VlessiaVpnService : VpnService() {

    companion object {
        const val ACTION_RUN = "io.github.feds1337.vlessia.RUN"
        const val ACTION_CONNECT = "io.github.feds1337.vlessia.CONNECT"
        const val ACTION_STOP = "io.github.feds1337.vlessia.STOP"
        private const val CHANNEL = "vpn"
        private const val NOTIFICATION_ID = 1

        @Volatile
        private var instance: VlessiaVpnService? = null

        @Volatile
        private var started: CountDownLatch? = null

        private val main = Handler(Looper.getMainLooper())

        /**
         * Starts the service in the foreground if needed and establishes the
         * VPN interface. Blocks; called from the Go core, never the main thread.
         * Returns the interface's file descriptor, which then belongs to Go.
         */
        fun establish(context: Context, dns: String): Int {
            val service = instance ?: run {
                val latch = CountDownLatch(1)
                started = latch
                ContextCompat.startForegroundService(
                    context, Intent(context, VlessiaVpnService::class.java).setAction(ACTION_RUN)
                )
                if (!latch.await(10, TimeUnit.SECONDS)) throw IOException("служба VPN не запустилась")
                instance ?: throw IOException("служба VPN остановлена")
            }
            return service.openInterface(dns)
        }

        /** The VPN interface was closed by the core. */
        fun stop() {
            main.post { instance?.finish() }
        }

        fun onState(state: String) {
            main.post {
                when (state) {
                    // A reconnect passes through "disconnected"; only stop if
                    // nothing reconnects shortly.
                    "disconnected" -> main.postDelayed({
                        if (Core.state == "disconnected") instance?.finish()
                    }, 1500)
                    else -> instance?.notifyState(state)
                }
            }
        }
    }

    override fun onCreate() {
        super.onCreate()
        goForeground("connecting")
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        instance = this
        goForeground(Core.state.takeIf { it != "disconnected" } ?: "connecting")
        when (intent?.action) {
            ACTION_RUN -> started?.countDown()
            ACTION_STOP -> Core.executor.execute { Mobile.disconnect() }
            // ACTION_CONNECT from the tile; no action (or SERVICE_INTERFACE)
            // when the system starts the service for always-on VPN.
            else -> if (Mobile.state() == "disconnected") {
                Core.executor.execute {
                    try {
                        Mobile.connect()
                    } catch (e: Exception) {
                        main.post { if (Core.state == "disconnected") finish() }
                    }
                }
            }
        }
        return START_NOT_STICKY
    }

    private fun openInterface(dns: String): Int {
        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(1500)
            .addAddress("172.19.0.1", 30)
            .addRoute("0.0.0.0", 0)
            .addDnsServer(dns)
            .addDisallowedApplication(packageName)
            .setConfigureIntent(openAppIntent())
        // IPv6 is captured too, so it cannot leak around the VPN.
        runCatching {
            builder.addAddress("fdfe:dcba:9876::1", 126)
            builder.addRoute("::", 0)
        }
        if (Build.VERSION.SDK_INT >= 29) builder.setMetered(false)
        val pfd = builder.establish()
            ?: throw IOException("нет разрешения на VPN — откройте Vlessia и подключитесь")
        return pfd.detachFd()
    }

    override fun onRevoke() {
        // Another VPN took over, or the user turned ours off in the settings.
        Core.executor.execute { Mobile.disconnect() }
        finish()
    }

    override fun onDestroy() {
        if (instance === this) instance = null
        super.onDestroy()
    }

    private fun finish() {
        if (instance === this) instance = null
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    // ---------- notification ----------

    private fun goForeground(state: String) {
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(state), type)
    }

    private fun notifyState(state: String) {
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(state))
    }

    private fun notification(state: String): Notification {
        val nm = getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= 26 && nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(
                NotificationChannel(CHANNEL, getString(R.string.channel_vpn), NotificationManager.IMPORTANCE_LOW)
                    .apply { setShowBadge(false) }
            )
        }
        val title = getString(
            when (state) {
                "connected" -> R.string.state_connected
                "disconnecting" -> R.string.state_disconnecting
                else -> R.string.state_connecting
            }
        )
        val stop = PendingIntent.getService(
            this, 1, Intent(this, VlessiaVpnService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        @Suppress("DEPRECATION")
        val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, CHANNEL) else Notification.Builder(this)
        return builder
            .setSmallIcon(R.drawable.ic_shield)
            .setColor(getColor(R.color.lagoon))
            .setContentTitle(title)
            .setContentText(Mobile.serverName())
            .setContentIntent(openAppIntent())
            .setOngoing(true)
            .setShowWhen(false)
            .addAction(Notification.Action.Builder(null as android.graphics.drawable.Icon?, getString(R.string.action_disconnect), stop).build())
            .build()
    }

    private fun openAppIntent(): PendingIntent = PendingIntent.getActivity(
        this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
    )
}
