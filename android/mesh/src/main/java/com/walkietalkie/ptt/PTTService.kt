package com.walkietalkie.ptt

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.wifi.WifiManager
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import com.walkietalkie.audio.OpusMicSource
import com.walkietalkie.audio.OpusSpeakerSink
import com.walkietalkie.ble.BlePresenceBridge
import com.walkietalkie.location.LocationUpdater
import com.walkietalkie.mesh.MeshIdentity
import com.walkietalkie.mesh.R
import com.walkietalkie.settings.NicknameStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import mobile.Mobile
import mobile.Node
import java.io.File

/**
 * Foreground service hosting the shared Go core's [Node] for this device's
 * whole lifetime: registry, mDNS discovery/announce, and the WebRTC mesh
 * (see core/mobile). A foreground service with type "microphone" is
 * required (Android 10+) to keep the mic path alive while the screen is
 * off or the app is backgrounded — see docs/2026-07-13-implementation-plan.md
 * ("Phase 2 — Android").
 */
class PTTService : LifecycleService() {

    private var node: Node? = null
    private var multicastLock: WifiManager.MulticastLock? = null
    private var locationUpdater: LocationUpdater? = null
    private var bleBridge: BlePresenceBridge? = null
    private var micSource: OpusMicSource? = null
    private var speakerSink: OpusSpeakerSink? = null
    private var connectivityManager: ConnectivityManager? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private var restartJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
        registerNetworkCallback()
    }

    // Confirmed on real hardware: after the phone loses Wi-Fi (walks out of
    // range) and reconnects, mDNS discovery does NOT recover on its own —
    // neither the periodic mdns.Browse resolver restart (core/discovery/mdns)
    // nor simply waiting fixes it, even after 30+ minutes. Only a full app
    // restart (fresh process, fresh sockets) brought discovery back
    // immediately. Root cause wasn't pinned down further (likely the
    // multicast lock or underlying socket binding not surviving the
    // network change), but restarting the Go node whenever Wi-Fi becomes
    // available again reproduces exactly what a manual app restart does,
    // automatically.
    private fun registerNetworkCallback() {
        val cm = applicationContext.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        connectivityManager = cm
        val request = NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .build()
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                // If we haven't done the very first start yet, let the normal
                // onStartCommand -> startNodeIfNeeded path handle it instead
                // of racing with it.
                if (node == null) return
                restartJob?.cancel()
                restartJob = lifecycleScope.launch(Dispatchers.IO) {
                    delay(2000) // debounce rapid connectivity flapping
                    Log.i(TAG, "Wi-Fi available again, restarting node for fresh discovery")
                    stopNodeOnly()
                    startNodeIfNeeded()
                }
            }
        }
        networkCallback = callback
        cm.registerNetworkCallback(request, callback)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, buildNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE)
        } else {
            startForeground(NOTIFICATION_ID, buildNotification())
        }

        when (intent?.action) {
            ACTION_START_TALKING -> {
                startNodeIfNeeded()
                node?.startTalking()
            }
            ACTION_STOP_TALKING -> node?.stopTalking()
            else -> startNodeIfNeeded()
        }

        return START_STICKY
    }

    override fun onBind(intent: Intent): android.os.IBinder? {
        super.onBind(intent)
        return null
    }

    private fun startNodeIfNeeded() {
        if (node != null) return

        // Android drops incoming multicast UDP by default; mDNS (224.0.0.251:5353)
        // needs this lock held for the whole time discovery is running, or
        // Browse() will simply never see any peer.
        val wifiManager = applicationContext.getSystemService(Context.WIFI_SERVICE) as WifiManager
        multicastLock = wifiManager.createMulticastLock("walkietalkie-mdns").apply {
            setReferenceCounted(true)
            acquire()
        }

        lifecycleScope.launch(Dispatchers.IO) {
            val dataDir = File(filesDir, "walkietalkie").absolutePath
            val nickname = NicknameStore.get(applicationContext)
            val deviceName = nickname.ifBlank { "${Build.MANUFACTURER} ${Build.MODEL}".trim() }
            try {
                val source = OpusMicSource()
                val sink = OpusSpeakerSink()
                micSource = source
                speakerSink = sink

                val started = Mobile.startNode(
                    dataDir,
                    deviceName,
                    MeshIdentity.platform,
                    MeshIdentity.appVersion,
                    source,
                    sink,
                )
                node = started
                instance = this@PTTService

                locationUpdater = LocationUpdater(applicationContext, started).apply { start() }
                bleBridge = BlePresenceBridge(applicationContext, started).apply { start() }
            } catch (e: Exception) {
                Log.e(TAG, "failed to start node", e)
            }
        }
    }

    override fun onDestroy() {
        networkCallback?.let { connectivityManager?.unregisterNetworkCallback(it) }
        restartJob?.cancel()
        instance = null
        stopNodeOnly()
        super.onDestroy()
    }

    // Tears down the current Node and its supporting pieces without
    // stopping the service itself — used both by onDestroy and by the
    // network-callback-triggered restart above. instance/companion state
    // is left alone since the service itself keeps running.
    private fun stopNodeOnly() {
        locationUpdater?.stop()
        bleBridge?.stop()
        try {
            node?.stop()
        } catch (e: Exception) {
            Log.e(TAG, "error stopping node", e)
        }
        node = null
        multicastLock?.let { if (it.isHeld) it.release() }
        multicastLock = null
        micSource?.release()
        speakerSink?.release()
        micSource = null
        speakerSink = null
    }

    fun startTalking() = node?.startTalking()
    fun startTalkingTo(peerID: String) = node?.startTalkingTo(peerID)
    fun startTalkingChannel(channelID: String) = node?.startTalkingChannel(channelID)
    fun stopTalking() = node?.stopTalking()
    fun isDirectlyConnected(peerID: String): Boolean =
        try {
            node?.isDirectlyConnected(peerID) == true
        } catch (_: Exception) {
            false
        }
    fun isRelayConnected(peerID: String): Boolean =
        try {
            node?.isRelayConnected(peerID) == true
        } catch (_: Exception) {
            false
        }
    fun isLiveTalkAvailable(peerID: String): Boolean =
        try {
            node?.isLiveTalkAvailable(peerID) == true
        } catch (_: Exception) {
            false
        }
    fun listDevicesJSON(): String = node?.listDevicesJSON() ?: "[]"
    fun selfId(): String = node?.selfID() ?: ""
    fun baseStationURL(): String = node?.baseStationURL() ?: ""
    fun updateName(name: String) {
        NicknameStore.set(applicationContext, name)
        node?.updateName(name)
    }

    // --- Base Station store-and-forward voice notes / private channels ---

    fun sendVoiceNote(toDeviceID: String, audio: ByteArray): String? =
        try {
            node?.sendVoiceNote(toDeviceID, audio)
            null
        } catch (e: Exception) {
            e.message ?: "send failed"
        }

    fun sendChannelClip(channelID: String, audio: ByteArray): String? =
        try {
            node?.sendChannelClip(channelID, audio)
            null
        } catch (e: Exception) {
            e.message ?: "send failed"
        }

    fun listVoiceNotesJSON(withPeerID: String = ""): String =
        try {
            node?.listVoiceNotesJSON(withPeerID) ?: "[]"
        } catch (_: Exception) {
            "[]"
        }

    fun listChannelNotesJSON(channelID: String): String =
        try {
            node?.listChannelNotesJSON(channelID) ?: "[]"
        } catch (_: Exception) {
            "[]"
        }

    fun downloadVoiceNote(noteID: String): ByteArray? =
        try {
            node?.downloadVoiceNote(noteID)
        } catch (_: Exception) {
            null
        }

    fun ackVoiceNote(noteID: String) {
        try {
            node?.ackVoiceNote(noteID)
        } catch (_: Exception) {
        }
    }

    fun deleteVoiceNote(noteID: String) {
        try {
            node?.deleteVoiceNote(noteID)
        } catch (_: Exception) {
        }
    }

    fun inviteChannel(toDeviceID: String): String? =
        try {
            node?.inviteChannel(toDeviceID)
        } catch (e: Exception) {
            null.also { Log.w(TAG, "invite failed: ${e.message}") }
        }

    fun acceptChannel(channelID: String) {
        try {
            node?.acceptChannel(channelID)
        } catch (_: Exception) {
        }
    }

    fun listChannelsJSON(): String =
        try {
            node?.listChannelsJSON() ?: "[]"
        } catch (_: Exception) {
            "[]"
        }

    fun closeChannel(channelID: String) {
        try {
            node?.closeChannel(channelID)
        } catch (_: Exception) {
        }
    }

    fun focusChannel(channelID: String) {
        try {
            node?.focusChannel(channelID)
        } catch (_: Exception) {
        }
    }

    fun blurChannel(channelID: String) {
        try {
            node?.blurChannel(channelID)
        } catch (_: Exception) {
        }
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.ptt_service_channel_name),
            NotificationManager.IMPORTANCE_LOW,
        )
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val launch = packageManager.getLaunchIntentForPackage(packageName)
            ?: Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val openIntent = PendingIntent.getActivity(
            this, 0,
            launch,
            PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.ptt_service_notification_title))
            .setContentText(getString(R.string.ptt_service_notification_text))
            .setSmallIcon(android.R.drawable.ic_btn_speak_now)
            .setContentIntent(openIntent)
            .setOngoing(true)
            .build()
    }

    companion object {
        private const val TAG = "PTTService"
        private const val CHANNEL_ID = "walkietalkie_ptt"
        private const val NOTIFICATION_ID = 1

        const val ACTION_START_TALKING = "com.walkietalkie.action.START_TALKING"
        const val ACTION_STOP_TALKING = "com.walkietalkie.action.STOP_TALKING"

        var instance: PTTService? = null
            private set
    }
}
