package com.walkietalkie.ptt

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import com.walkietalkie.MainActivity
import com.walkietalkie.R
import com.walkietalkie.audio.OpusMicSource
import com.walkietalkie.audio.OpusSpeakerSink
import com.walkietalkie.ble.BlePresenceBridge
import com.walkietalkie.location.LocationUpdater
import kotlinx.coroutines.Dispatchers
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

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, buildNotification(), ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE)
        } else {
            startForeground(NOTIFICATION_ID, buildNotification())
        }

        when (intent?.action) {
            ACTION_START_TALKING -> node?.startTalking()
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
            val deviceName = "${Build.MANUFACTURER} ${Build.MODEL}".trim()
            try {
                val source = OpusMicSource()
                val sink = OpusSpeakerSink()
                micSource = source
                speakerSink = sink

                val started = Mobile.startNode(
                    dataDir,
                    deviceName,
                    "android",
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
        instance = null
        locationUpdater?.stop()
        bleBridge?.stop()
        try {
            node?.stop()
        } catch (e: Exception) {
            Log.e(TAG, "error stopping node", e)
        }
        multicastLock?.let { if (it.isHeld) it.release() }
        micSource?.release()
        speakerSink?.release()
        super.onDestroy()
    }

    fun startTalking() = node?.startTalking()
    fun stopTalking() = node?.stopTalking()
    fun listDevicesJSON(): String = node?.listDevicesJSON() ?: "[]"

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.ptt_service_channel_name),
            NotificationManager.IMPORTANCE_LOW,
        )
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val openIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java),
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
