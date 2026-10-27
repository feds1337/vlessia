package io.github.feds1337.vlessia

import android.app.Application
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.ClipData
import android.content.ClipboardManager
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.media.MediaPlayer
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.service.quicksettings.TileService
import android.util.Log
import android.widget.Toast
import androidx.core.content.ContextCompat
import androidx.core.content.FileProvider
import io.github.feds1337.vlessia.mobile.Host
import io.github.feds1337.vlessia.mobile.Mobile
import org.json.JSONObject
import java.io.File
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

class VlessiaApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Core.start(this)
    }
}

/**
 * The bridge to the Go core: starts it, implements its [Host] callbacks
 * and fans its events out to the UI, the VPN service and the tile.
 */
object Core : Host {
    private const val TAG = "Vlessia"
    private const val UPDATE_CHANNEL = "updates"
    private const val UPDATE_NOTIFICATION_ID = 2

    lateinit var context: Context
        private set
    val executor: ExecutorService = Executors.newCachedThreadPool()
    private val main = Handler(Looper.getMainLooper())

    /** Receives UI events while the main screen exists. */
    @Volatile
    var ui: ((name: String, payload: String) -> Unit)? = null

    /** True while [MainActivity] is in the foreground (resumed). */
    @Volatile
    var foreground = false
        private set

    /** Connection state from the last "state" event. */
    @Volatile
    var state = "disconnected"
        private set

    /** Pending APK path waiting for install permission or foreground resume. */
    @Volatile
    var pendingInstallPath: String? = null

    @Volatile
    private var askedInstallPermission = false

    fun start(app: Application) {
        context = app
        val abi = Build.SUPPORTED_ABIS.firstOrNull() ?: ""
        try {
            Mobile.start(File(app.filesDir, "data").path, BuildConfig.VERSION_NAME, abi, this)
        } catch (e: Exception) {
            Log.e(TAG, "core start", e)
            return
        }
        // If VPN was active right before an OTA update replaced the package,
        // automatically reconnect.
        val reconnectMarker = File(app.filesDir, "data/updates/reconnect")
        if (reconnectMarker.exists()) {
            reconnectMarker.delete()
            if (VpnService.prepare(app) == null && Mobile.state() == "disconnected") {
                runCatching {
                    ContextCompat.startForegroundService(
                        app, Intent(app, VlessiaVpnService::class.java).setAction(VlessiaVpnService.ACTION_CONNECT)
                    )
                }.onFailure {
                    executor.execute { runCatching { Mobile.connect() } }
                }
            }
        }
    }

    override fun emit(name: String, payload: String) {
        main.post {
            if (name == "state") onState(payload)
            ui?.invoke(name, payload)
        }
    }

    private fun onState(payload: String) {
        val status = runCatching { JSONObject(payload).getJSONObject("status") }.getOrNull() ?: return
        val next = status.optString("state", "disconnected")
        if (next == state) return
        state = next
        VlessiaVpnService.onState(next)
        TileService.requestListeningState(context, ComponentName(context, VlessiaTile::class.java))
    }

    override fun openTun(dns: String): Int = VlessiaVpnService.establish(context, dns)

    override fun tunClosed() = VlessiaVpnService.stop()

    override fun openURL(url: String) {
        runCatching {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }

    override fun setClipboard(text: String) {
        main.post {
            val cm = context.getSystemService(ClipboardManager::class.java)
            cm.setPrimaryClip(ClipData.newPlainText("Vlessia", text))
        }
    }

    override fun playSound(path: String) {
        main.post {
            runCatching {
                MediaPlayer().apply {
                    setDataSource(path)
                    setOnCompletionListener { it.release() }
                    prepare()
                    start()
                }
            }
        }
    }

    override fun installPackage(path: String) {
        askedInstallPermission = false
        main.post {
            val file = File(path)
            if (!file.exists()) return@post

            if (state == "connected" || state == "connecting") {
                runCatching {
                    File(context.filesDir, "data/updates/reconnect").apply {
                        parentFile?.mkdirs()
                        writeText("1")
                    }
                }
            }

            if (Build.VERSION.SDK_INT >= 26 && !context.packageManager.canRequestPackageInstalls()) {
                pendingInstallPath = path
                if (foreground) {
                    askedInstallPermission = true
                    requestInstallPermission()
                } else {
                    notifyUpdateReady()
                }
                return@post
            }

            if (foreground) {
                pendingInstallPath = null
                launchSystemInstaller(file)
            } else {
                pendingInstallPath = path
                notifyUpdateReady()
            }
        }
    }

    private fun launchSystemInstaller(file: File) {
        val uri = FileProvider.getUriForFile(context, context.packageName + ".files", file)
        val viewIntent = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        runCatching { context.startActivity(viewIntent) }
    }

    private fun requestInstallPermission() {
        if (Build.VERSION.SDK_INT < 26) return
        Toast.makeText(context, "Разрешите установку обновлений для Vlessia", Toast.LENGTH_LONG).show()
        runCatching {
            context.startActivity(
                Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + context.packageName))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            )
        }
    }

    /**
     * Called when [MainActivity] comes to the foreground so a downloaded OTA
     * update or newly granted install permission resumes automatically.
     */
    fun onAppResumed() {
        foreground = true
        val nm = context.getSystemService(NotificationManager::class.java)
        nm?.cancel(UPDATE_NOTIFICATION_ID)

        val path = pendingInstallPath ?: return
        val file = File(path)
        if (!file.exists()) {
            pendingInstallPath = null
            return
        }
        if (Build.VERSION.SDK_INT < 26 || context.packageManager.canRequestPackageInstalls()) {
            pendingInstallPath = null
            launchSystemInstaller(file)
        } else if (!askedInstallPermission) {
            askedInstallPermission = true
            requestInstallPermission()
        }
    }

    fun onAppPaused() {
        foreground = false
    }

    fun notifyUpdateReady() {
        main.post {
            val nm = context.getSystemService(NotificationManager::class.java) ?: return@post
            if (Build.VERSION.SDK_INT >= 26 && nm.getNotificationChannel(UPDATE_CHANNEL) == null) {
                nm.createNotificationChannel(
                    NotificationChannel(UPDATE_CHANNEL, "Обновления", NotificationManager.IMPORTANCE_HIGH)
                )
            }
            val openApp = PendingIntent.getActivity(
                context, 3,
                Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
            @Suppress("DEPRECATION")
            val builder = if (Build.VERSION.SDK_INT >= 26) {
                Notification.Builder(context, UPDATE_CHANNEL)
            } else {
                Notification.Builder(context)
            }
            val notification = builder
                .setSmallIcon(R.drawable.ic_shield)
                .setColor(context.getColor(R.color.lagoon))
                .setContentTitle("Обновление Vlessia скачано")
                .setContentText("Нажмите, чтобы завершить установку")
                .setContentIntent(openApp)
                .setAutoCancel(true)
                .build()
            runCatching { nm.notify(UPDATE_NOTIFICATION_ID, notification) }
        }
    }

    override fun openVpnSettings() {
        runCatching {
            context.startActivity(Intent(Settings.ACTION_VPN_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
    }
}

/**
 * Cleans up downloaded APK files after the package is replaced.
 */
class UpdateReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Intent.ACTION_MY_PACKAGE_REPLACED) {
            val dir = File(context.filesDir, "data/updates")
            dir.listFiles()?.forEach { f ->
                if (f.name.endsWith(".apk") || f.name.endsWith(".part")) {
                    f.delete()
                }
            }
        }
    }
}
