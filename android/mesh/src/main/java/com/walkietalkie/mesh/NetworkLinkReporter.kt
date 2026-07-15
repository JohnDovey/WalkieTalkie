package com.walkietalkie.mesh

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.WifiManager
import android.telephony.TelephonyManager
import android.util.Log
import androidx.core.content.ContextCompat
import mobile.Node

/**
 * Reports the phone's active uplink (Wi‑Fi SSID or cellular carrier) into the
 * Go Node so MeshSniff can draw a cellular cloud edge when not on LAN Wi‑Fi.
 */
class NetworkLinkReporter(
    private val context: Context,
    private val node: Node,
) {
    private val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    private var callback: ConnectivityManager.NetworkCallback? = null

    fun start() {
        reportNow()
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        val cb = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) = reportNow()
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) = reportNow()
            override fun onLost(network: Network) = reportNow()
        }
        callback = cb
        try {
            cm.registerNetworkCallback(request, cb)
        } catch (e: Exception) {
            Log.w(TAG, "registerNetworkCallback failed", e)
        }
    }

    fun stop() {
        callback?.let {
            try {
                cm.unregisterNetworkCallback(it)
            } catch (_: Exception) {
            }
        }
        callback = null
    }

    private fun reportNow() {
        val (type, name) = readActiveLink()
        if (type.isEmpty()) return
        try {
            node.setNetworkLink(type, name)
        } catch (e: Exception) {
            Log.w(TAG, "setNetworkLink failed", e)
        }
    }

    private fun readActiveLink(): Pair<String, String> {
        val network = cm.activeNetwork ?: return "" to ""
        val caps = cm.getNetworkCapabilities(network) ?: return "" to ""
        return when {
            caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> {
                "wifi" to wifiSsid()
            }
            caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> {
                "cellular" to carrierName()
            }
            else -> "" to ""
        }
    }

    private fun wifiSsid(): String {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.ACCESS_FINE_LOCATION)
            != PackageManager.PERMISSION_GRANTED
        ) {
            return ""
        }
        return try {
            val wifi = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
            @Suppress("DEPRECATION")
            val raw = wifi.connectionInfo?.ssid ?: return ""
            raw.trim('"').takeIf { it.isNotBlank() && !it.equals("<unknown ssid>", ignoreCase = true) }.orEmpty()
        } catch (_: Exception) {
            ""
        }
    }

    private fun carrierName(): String {
        return try {
            val tm = context.getSystemService(Context.TELEPHONY_SERVICE) as TelephonyManager
            tm.networkOperatorName?.trim().orEmpty()
        } catch (_: Exception) {
            ""
        }
    }

    companion object {
        private const val TAG = "NetworkLinkReporter"
    }
}
