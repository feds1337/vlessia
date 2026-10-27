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
import android.content.pm.PackageInstaller
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

    /** Pending PackageInstaller confirmation intent waiting for foreground resume. */
    @Volatile
    var pendingConfirmIntent: Intent? = null

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
        executor.execute {
            val file = File(path)
            if (!file.exists()) return@execute

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
                main.post {
                    if (foreground) {
                        askedInstallPermission = true
                        requestInstallPermission()
                    } else {
                        notifyUpdateReady()
                    }
                }
                return@execute
            }

            pendingInstallPath = null
            if (!commitSessionInstall(file)) {
                main.post { launchSystemInstaller(file.path) }
            }
        }
    }

    /**
     * Streams [file] into a [PackageInstaller] session. On Android 12+ (API 31+)
     * with USER_ACTION_NOT_REQUIRED, self-updates install silently without a
     * prompt when Vlessia is already the installer of record; otherwise
     * [UpdateReceiver] receives STATUS_PENDING_USER_ACTION and opens the
     * system confirmation sheet.
     */
    private fun commitSessionInstall(file: File): Boolean {
        return try {
            val pi = context.packageManager.packageInstaller
            val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
                setAppPackageName(context.packageName)
                setSize(file.length())
                if (Build.VERSION.SDK_INT >= 31) {
                    setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
                }
            }
            val sessionId = pi.createSession(params)
            pi.openSession(sessionId).use { session ->
                file.inputStream().use { input ->
                    session.openWrite("base.apk", 0, file.length()).use { out ->
                        input.copyTo(out)
                        session.fsync(out)
                    }
                }
                val intent = Intent(context, UpdateReceiver::class.java)
                    .setAction(UpdateReceiver.ACTION_INSTALL_STATUS)
                    .setPackage(context.packageName)
                    .putExtra(UpdateReceiver.EXTRA_APK_PATH, file.path)
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                    (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
                val pending = PendingIntent.getBroadcast(context, sessionId, intent, flags)
                session.commit(pending.intentSender)
            }
            true
        } catch (e: Exception) {
            Log.w(TAG, "session install fallback", e)
            false
        }
    }

    fun launchSystemInstaller(path: String) {
        val file = File(path)
        if (!file.exists()) return
        if (Build.VERSION.SDK_INT >= 26 && !context.packageManager.canRequestPackageInstalls()) {
            pendingInstallPath = path
            if (foreground) requestInstallPermission() else notifyUpdateReady()
            return
        }
        val uri = FileProvider.getUriForFile(context, context.packageName + ".files", file)
        val viewIntent = Intent(Intent.ACTION_VIEW)
            .setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        if (foreground) {
            runCatching { context.startActivity(viewIntent) }
        } else {
            pendingInstallPath = path
            notifyUpdateReady()
        }
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

        val confirm = pendingConfirmIntent
        if (confirm != null) {
            pendingConfirmIntent = null
            runCatching {
                context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                return
            }
        }

        val path = pendingInstallPath ?: return
        if (!File(path).exists()) {
            pendingInstallPath = null
            return
        }
        if (Build.VERSION.SDK_INT < 26 || context.packageManager.canRequestPackageInstalls()) {
            pendingInstallPath = null
            installPackage(path)
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
 * Handles [PackageInstaller] session commit callbacks and cleans up after
 * the package is replaced.
 */
class UpdateReceiver : BroadcastReceiver() {
    companion object {
        const val ACTION_INSTALL_STATUS = "io.github.feds1337.vlessia.INSTALL_STATUS"
        const val EXTRA_APK_PATH = "apk_path"
    }

    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            ACTION_INSTALL_STATUS -> {
                val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)
                val apkPath = intent.getStringExtra(EXTRA_APK_PATH)
                when (status) {
                    PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                        val confirm = if (Build.VERSION.SDK_INT >= 33) {
                            intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
                        } else {
                            @Suppress("DEPRECATION")
                            intent.getParcelableExtra(Intent.EXTRA_INTENT)
                        }
                        if (confirm != null) {
                            if (Core.foreground) {
                                runCatching {
                                    context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                                }.onFailure {
                                    Core.pendingConfirmIntent = confirm
                                    Core.pendingInstallPath = apkPath
                                    Core.notifyUpdateReady()
                                }
                            } else {
                                Core.pendingConfirmIntent = confirm
                                Core.pendingInstallPath = apkPath
                                Core.notifyUpdateReady()
                            }
                        } else if (!apkPath.isNullOrEmpty()) {
                            Core.launchSystemInstaller(apkPath)
                        }
                    }
                    PackageInstaller.STATUS_SUCCESS -> {
                        if (!apkPath.isNullOrEmpty()) {
                            File(apkPath).delete()
                        }
                    }
                    PackageInstaller.STATUS_FAILURE_ABORTED -> {
                        // User cancelled the install dialog; don't force-reopen.
                        File(context.filesDir, "data/updates/reconnect").delete()
                    }
                    else -> {
                        if (!apkPath.isNullOrEmpty()) {
                            Core.launchSystemInstaller(apkPath)
                        }
                    }
                }
            }
            Intent.ACTION_MY_PACKAGE_REPLACED -> {
                val dir = File(context.filesDir, "data/updates")
                dir.listFiles()?.forEach { f ->
                    if (f.name.endsWith(".apk") || f.name.endsWith(".part")) {
                        f.delete()
                    }
                }
            }
        }
    }
}
