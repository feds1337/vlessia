package io.github.feds1337.vlessia

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Intent
import android.graphics.drawable.Icon
import android.net.VpnService
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.core.content.ContextCompat
import io.github.feds1337.vlessia.mobile.Mobile

/** Quick settings tile: connects to the selected server or disconnects. */
class VlessiaTile : TileService() {

    override fun onStartListening() {
        val tile = qsTile ?: return
        val state = Mobile.state()
        // The kill switch keeps the VPN interface up while the VPN is off;
        // the tile shows the VPN as active then, with a note.
        val blocked = Mobile.killSwitch()
        tile.state = when (state) {
            "connected" -> Tile.STATE_ACTIVE
            "disconnected" -> if (blocked) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
            else -> Tile.STATE_UNAVAILABLE
        }
        tile.icon = Icon.createWithResource(this, R.drawable.ic_shield)
        tile.label = getString(R.string.app_name)
        if (Build.VERSION.SDK_INT >= 29) {
            tile.subtitle = when {
                blocked -> getString(R.string.state_blocked)
                state == "connected" -> Mobile.serverName()
                state == "connecting" -> getString(R.string.state_connecting)
                state == "disconnecting" -> getString(R.string.state_disconnecting)
                else -> null
            }
        }
        tile.updateTile()
    }

    override fun onClick() {
        when (Mobile.state()) {
            "connected" -> Core.executor.execute { Mobile.disconnect() }
            "disconnected" ->
                if (VpnService.prepare(this) != null) {
                    openApp() // the VPN permission is asked there
                } else {
                    try {
                        ContextCompat.startForegroundService(
                            this, Intent(this, VlessiaVpnService::class.java).setAction(VlessiaVpnService.ACTION_CONNECT)
                        )
                    } catch (e: Exception) {
                        // Starting a service from the background may be refused.
                        openApp()
                    }
                }
        }
    }

    @SuppressLint("StartActivityAndCollapseDeprecated")
    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java)
            .putExtra(MainActivity.EXTRA_CONNECT, true)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= 34) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 2, intent, PendingIntent.FLAG_IMMUTABLE))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }
}
