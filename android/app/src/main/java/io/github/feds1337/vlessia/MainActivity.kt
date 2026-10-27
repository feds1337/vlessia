package io.github.feds1337.vlessia

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.content.ClipboardManager
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import android.webkit.JavascriptInterface
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.webkit.WebViewAssetLoader
import io.github.feds1337.vlessia.mobile.Mobile
import android.util.Base64
import com.google.zxing.integration.android.IntentIntegrator
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream

/**
 * The main screen: the desktop client's web UI in a WebView. Its calls to
 * the backend (window.go.main.App.*, see assets/web/android.js) arrive at
 * [Bridge] and go to the Go core; a few that need Android itself — the VPN
 * permission, the clipboard, picking a file — are handled here first.
 */
class MainActivity : Activity() {

    companion object {
        const val EXTRA_CONNECT = "connect"
        private const val REQ_VPN = 1
        private const val REQ_FILE = 2
        private const val ORIGIN = "https://appassets.androidplatform.net"
    }

    private lateinit var web: WebView
    private var pageReady = false
    private val afterLoad = mutableListOf<String>()
    private var insetsJs = ""

    private var onVpnGranted: (() -> Unit)? = null
    private var onVpnDenied: (() -> Unit)? = null
    private var pickCallback: Int? = null
    private var scanCallback: Int? = null

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        WindowCompat.setDecorFitsSystemWindows(window, false)

        web = WebView(this)
        web.setBackgroundColor(getColor(R.color.paper))
        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = false
            allowContentAccess = false
            setSupportZoom(false)
        }
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        web.addJavascriptInterface(Bridge(), "VlessiaNative")
        web.webViewClient = Client()
        setContentView(web)

        // The page is drawn under the system bars; it pads itself by these.
        ViewCompat.setOnApplyWindowInsetsListener(web) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            val ime = insets.getInsets(WindowInsetsCompat.Type.ime())
            val d = resources.displayMetrics.density
            insetsJs = "(function(s){s.setProperty('--inset-top','${bars.top / d}px');" +
                "s.setProperty('--inset-bottom','${maxOf(bars.bottom, ime.bottom) / d}px');" +
                "s.setProperty('--inset-left','${bars.left / d}px');" +
                "s.setProperty('--inset-right','${bars.right / d}px');})(document.documentElement.style)"
            if (pageReady) web.evaluateJavascript(insetsJs, null)
            WindowInsetsCompat.CONSUMED
        }

        Core.ui = { name, payload ->
            if (pageReady) web.evaluateJavascript("window.__vlessiaEmit&&__vlessiaEmit(${JSONObject.quote(name)},$payload)", null)
            if (name == "state") updateBars()
        }
        updateBars()
        web.loadUrl("$ORIGIN/assets/web/index.html")

        handleIntent(intent, firstStart = savedInstanceState == null)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent, firstStart = false)
    }

    private fun handleIntent(intent: Intent?, firstStart: Boolean) {
        if (intent?.action == Intent.ACTION_SEND) {
            intent.getStringExtra(Intent.EXTRA_TEXT)?.let { text ->
                runInPage("call('ImportText', ${JSONObject.quote(text)}).then(added).catch(()=>{})")
            }
            return
        }
        val wantConnect = intent?.getBooleanExtra(EXTRA_CONNECT, false) == true || (firstStart && Mobile.autoConnect())
        if (wantConnect && Mobile.state() == "disconnected") {
            withVpnPermission({ Core.executor.execute { runCatching { Mobile.connect() } } }, {})
        }
    }

    override fun onResume() {
        super.onResume()
        Mobile.setVisible(true)
        Core.onAppResumed()
    }

    override fun onPause() {
        Core.onAppPaused()
        Mobile.setVisible(false)
        super.onPause()
    }

    override fun onDestroy() {
        Core.ui = null
        web.destroy()
        super.onDestroy()
    }

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        // The page closes a dialog or returns to the server list first.
        web.evaluateJavascript("window.__vlessiaBack?__vlessiaBack():false") { handled ->
            if (handled != "true") moveTaskToBack(true)
        }
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        updateBars() // the theme may have switched between light and dark
    }

    /** Status bar icons follow the colour under them: the dock lights up when connected. */
    private fun updateBars() {
        val dark = (resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
        val connected = Core.state == "connected"
        WindowInsetsControllerCompat(window, window.decorView).apply {
            isAppearanceLightStatusBars = connected == dark
            isAppearanceLightNavigationBars = !dark
        }
    }

    private fun runInPage(js: String) {
        if (pageReady) web.evaluateJavascript(js, null) else afterLoad += js
    }

    // ---------- calls from the page ----------

    private inner class Bridge {
        @JavascriptInterface
        fun call(id: Int, method: String, args: String) {
            runOnUiThread { dispatch(id, method, args) }
        }
    }

    private fun dispatch(id: Int, method: String, args: String) {
        when (method) {
            "Connect" -> withVpnPermission({ exec(id, method, args) }, { reject(id, "Нет разрешения на VPN") })
            "Toggle" ->
                if (Mobile.state() == "disconnected") {
                    withVpnPermission({ exec(id, method, args) }, { reject(id, "Нет разрешения на VPN") })
                } else {
                    exec(id, method, args)
                }
            "ImportClipboard" -> {
                val clip = getSystemService(ClipboardManager::class.java).primaryClip
                val text = clip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(this)?.toString().orEmpty()
                exec(id, "ImportText", JSONArray().put(text).toString())
            }
            "ImportFile" -> pickFile(id)
            "ScanQR" -> scanQR(id)
            else -> exec(id, method, args)
        }
    }

    private fun exec(id: Int, method: String, args: String) {
        Core.executor.execute {
            try {
                val result = Mobile.call(method, args)
                runOnUiThread { resolve(id, result) }
            } catch (e: Exception) {
                runOnUiThread { reject(id, e.message ?: e.toString()) }
            }
        }
    }

    private fun resolve(id: Int, json: String) {
        web.evaluateJavascript("__vlessiaResolve($id,true,${JSONObject.quote(json)})", null)
    }

    private fun reject(id: Int, message: String) {
        web.evaluateJavascript("__vlessiaResolve($id,false,${JSONObject.quote(message)})", null)
    }

    // ---------- VPN permission ----------

    private fun withVpnPermission(granted: () -> Unit, denied: () -> Unit) {
        val intent = VpnService.prepare(this)
        if (intent == null) {
            askNotifications()
            granted()
            return
        }
        // The notification request waits until the VPN one is answered.
        onVpnGranted = { askNotifications(); granted() }
        onVpnDenied = denied
        startActivityForResult(intent, REQ_VPN)
    }

    /** The VPN notification needs this on Android 13+; asked once, not required. */
    private fun askNotifications() {
        if (Build.VERSION.SDK_INT >= 33 &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), 0)
        }
    }

    // ---------- files ----------

    private fun pickFile(id: Int) {
        pickCallback = id
        val intent = Intent(Intent.ACTION_OPEN_DOCUMENT)
            .addCategory(Intent.CATEGORY_OPENABLE)
            .setType("*/*")
        try {
            startActivityForResult(intent, REQ_FILE)
        } catch (e: Exception) {
            pickCallback = null
            reject(id, "Не удалось открыть выбор файла")
        }
    }

    private fun importFile(id: Int, uri: Uri) {
        Core.executor.execute {
            try {
                var name = "config"
                contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                    if (c.moveToFirst()) name = c.getString(0)?.substringBeforeLast('.') ?: name
                }
                val bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() }
                    ?: throw Exception("не удалось прочитать файл")
                if (bytes.size > 4 shl 20) throw Exception("файл слишком большой")
                val data = Base64.encodeToString(bytes, Base64.NO_WRAP)
                val result = Mobile.call("ImportFileData", JSONArray().put(data).put(name).toString())
                runOnUiThread { resolve(id, result) }
            } catch (e: Exception) {
                runOnUiThread { reject(id, e.message ?: e.toString()) }
            }
        }
    }

    // ---------- QR codes ----------

    /** Opens the camera; a scanned link or subscription URL is imported. */
    private fun scanQR(id: Int) {
        scanCallback = id
        IntentIntegrator(this)
            .setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            .setPrompt(getString(R.string.scan_prompt))
            .setBeepEnabled(false)
            .setOrientationLocked(false)
            .initiateScan()
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        when (requestCode) {
            IntentIntegrator.REQUEST_CODE -> {
                val id = scanCallback ?: return
                scanCallback = null
                val text = IntentIntegrator.parseActivityResult(requestCode, resultCode, data)?.contents
                if (text.isNullOrBlank()) resolve(id, "0") else exec(id, "ImportText", JSONArray().put(text).toString())
            }
            REQ_VPN -> {
                val granted = onVpnGranted
                val denied = onVpnDenied
                onVpnGranted = null
                onVpnDenied = null
                if (resultCode == RESULT_OK) granted?.invoke() else denied?.invoke()
            }
            REQ_FILE -> {
                val id = pickCallback ?: return
                pickCallback = null
                val uri = data?.data
                if (resultCode != RESULT_OK || uri == null) resolve(id, "0") else importFile(id, uri)
            }
        }
    }

    // ---------- page loading ----------

    private inner class Client : WebViewClient() {
        private val loader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this@MainActivity))
            .build()

        override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
            val url = request.url
            if (url.path == "/assets/web/index.html") {
                // The desktop page plus the Android bridge, loaded before app.js.
                val html = assets.open("web/index.html").bufferedReader().use { it.readText() }
                    .replace("<script src=\"app.js\"></script>", "<script src=\"android.js\"></script><script src=\"app.js\"></script>")
                return WebResourceResponse("text/html", "utf-8", ByteArrayInputStream(html.toByteArray()))
            }
            return loader.shouldInterceptRequest(url)
        }

        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
            // Links never navigate the app's page; they open in the browser.
            Core.openURL(request.url.toString())
            return true
        }

        override fun onPageFinished(view: WebView, url: String) {
            pageReady = true
            if (insetsJs.isNotEmpty()) view.evaluateJavascript(insetsJs, null)
            afterLoad.forEach { view.evaluateJavascript(it, null) }
            afterLoad.clear()
        }
    }
}
