package com.walkietalkie

import android.content.Context
import android.content.Intent
import android.media.MediaPlayer
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.DrawerValue
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalDrawerSheet
import androidx.compose.material3.ModalNavigationDrawer
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDrawerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.walkietalkie.audio.ClipRecorder
import com.walkietalkie.ptt.PTTService
import com.walkietalkie.settings.NicknameStore
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

private val REQUIRED_PERMISSIONS = buildList {
    add(android.Manifest.permission.RECORD_AUDIO)
    add(android.Manifest.permission.ACCESS_FINE_LOCATION)
    add(android.Manifest.permission.ACCESS_COARSE_LOCATION)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
        add(android.Manifest.permission.BLUETOOTH_SCAN)
        add(android.Manifest.permission.BLUETOOTH_ADVERTISE)
        add(android.Manifest.permission.BLUETOOTH_CONNECT)
    }
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        add(android.Manifest.permission.POST_NOTIFICATIONS)
    }
}.toTypedArray()

class MainActivity : ComponentActivity() {

    /**
     * Optional adb deep-link for automation/screenshots. Only applied when the
     * intent includes an explicit `screen` extra — normal launcher opens and
     * onNewIntent resumes do not reset the current UI.
     *
     * adb shell am start -n com.walkietalkie/.MainActivity -f 0x20000000 \
     *   --es screen settings|about|devices|record|thread|channel|chats \
     *   [--es peerId … --es peerName … --es channelId … --es status …]
     */
    private val deepLink = mutableStateOf<DeepLink?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val requestPermissions = registerForActivityResult(
            ActivityResultContracts.RequestMultiplePermissions(),
        ) { results ->
            if (results.values.all { it }) {
                startPttService()
            }
        }

        if (REQUIRED_PERMISSIONS.all {
                ContextCompat.checkSelfPermission(this, it) == android.content.pm.PackageManager.PERMISSION_GRANTED
            }
        ) {
            startPttService()
        } else {
            requestPermissions.launch(REQUIRED_PERMISSIONS)
        }

        deepLink.value = DeepLink.from(intent)

        setContent {
            MaterialTheme {
                // Keep the top nav and bottom Talk button clear of the status
                // bar / notch and the system navigation gesture bar — without
                // this the menu is unselectable and the Talk button is clipped.
                Surface(modifier = Modifier.fillMaxSize().safeDrawingPadding()) {
                    val link by deepLink
                    AppScreen(deepLink = link)
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        // Ignore plain MAIN/LAUNCHER re-delivery; only honor explicit screen extras.
        DeepLink.from(intent)?.let { deepLink.value = it }
    }

    private fun startPttService() {
        val intent = Intent(this, PTTService::class.java)
        ContextCompat.startForegroundService(this, intent)
    }
}

/** Parsed adb/intent routing for screenshot/manual navigation. Null if no `screen` extra. */
private data class DeepLink(
    val screen: Screen,
    val openDrawer: Boolean,
    val seq: Long,
) {
    companion object {
        private var seqCounter = 0L

        fun from(intent: Intent): DeepLink? {
            val key = intent.getStringExtra("screen")?.lowercase()?.takeIf { it.isNotBlank() }
                ?: return null
            val peerId = intent.getStringExtra("peerId").orEmpty()
            val peerName = intent.getStringExtra("peerName").orEmpty().ifBlank { peerId.ifBlank { "Peer" } }
            val channelId = intent.getStringExtra("channelId").orEmpty()
            val status = intent.getStringExtra("status").orEmpty().ifBlank { "active" }
            val screen = when (key) {
                "settings" -> Screen.Settings
                "about" -> Screen.About
                "record", "record_voice" -> Screen.RecordVoice(peerId.ifBlank { "demo" }, peerName)
                "thread", "voice_thread" -> Screen.VoiceThread(peerId.ifBlank { "demo" }, peerName)
                "channel", "private_channel" -> Screen.PrivateChannel(
                    channelId.ifBlank { "demo-channel" },
                    peerId.ifBlank { "demo" },
                    peerName,
                    status,
                )
                "chats", "devices" -> Screen.Devices
                else -> Screen.Devices
            }
            return DeepLink(
                screen = screen,
                openDrawer = key == "chats",
                seq = ++seqCounter,
            )
        }
    }
}

private sealed class Screen {
    data object Devices : Screen()
    data object Settings : Screen()
    data object About : Screen()
    data class VoiceThread(val peerId: String, val peerName: String) : Screen()
    data class RecordVoice(val peerId: String, val peerName: String, val channelId: String = "") : Screen()
    data class PrivateChannel(
        val channelId: String,
        val peerId: String,
        val peerName: String,
        val status: String,
    ) : Screen()
}

@Composable
private fun AppScreen(
    deepLink: DeepLink? = null,
) {
    var screen by remember { mutableStateOf(deepLink?.screen ?: Screen.Devices) }
    val context = LocalContext.current
    val drawerState = rememberDrawerState(
        if (deepLink?.openDrawer == true) DrawerValue.Open else DrawerValue.Closed,
    )
    val scope = rememberCoroutineScope()

    // Apply adb deep-link navigation only when an explicit `screen` extra is present.
    LaunchedEffect(deepLink?.seq) {
        val link = deepLink ?: return@LaunchedEffect
        screen = link.screen
        if (link.openDrawer) {
            drawerState.open()
        } else if (drawerState.isOpen) {
            drawerState.close()
        }
    }

    var channelsJson by remember { mutableStateOf("[]") }
    var inboxJson by remember { mutableStateOf("[]") }
    var devicesJson by remember { mutableStateOf("[]") }

    LaunchedEffect(Unit) {
        while (true) {
            devicesJson = PTTService.instance?.listDevicesJSON() ?: "[]"
            channelsJson = PTTService.instance?.listChannelsJSON() ?: "[]"
            inboxJson = PTTService.instance?.listVoiceNotesJSON("") ?: "[]"
            delay(2500)
        }
    }

    val devices = remember(devicesJson) { parseDevices(devicesJson) }
    val channels = remember(channelsJson) { parseChannels(channelsJson) }
    val inboxByFrom = remember(inboxJson) { countQueuedInbox(inboxJson, PTTService.instance?.selfId() ?: "") }
    val drawerUnread = channels.sumOf { it.unread } + inboxByFrom.values.sum()

    ModalNavigationDrawer(
        drawerState = drawerState,
        drawerContent = {
            ModalDrawerSheet(modifier = Modifier.width(300.dp).safeDrawingPadding()) {
                Text(
                    "Chats",
                    style = MaterialTheme.typography.titleMedium,
                    modifier = Modifier.padding(16.dp),
                )
                Text(
                    "Private channels",
                    style = MaterialTheme.typography.labelLarge,
                    color = Color.Gray,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                )
                if (channels.isEmpty()) {
                    Text("No private channels", modifier = Modifier.padding(16.dp), color = Color.Gray)
                } else {
                    channels.forEach { ch ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    scope.launch { drawerState.close() }
                                    screen = Screen.PrivateChannel(ch.id, ch.peerId, ch.peerName, ch.status)
                                }
                                .padding(horizontal = 16.dp, vertical = 10.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(
                                buildString {
                                    append(ch.peerName.ifBlank { ch.peerId })
                                    if (ch.status == "pending") append(" (pending)")
                                },
                            )
                            if (ch.unread > 0) {
                                Text(
                                    "${ch.unread}",
                                    color = Color.White,
                                    modifier = Modifier
                                        .background(Color(0xFFDC3545), CircleShape)
                                        .padding(horizontal = 8.dp, vertical = 2.dp),
                                )
                            }
                        }
                    }
                }
                HorizontalDivider(modifier = Modifier.padding(vertical = 8.dp))
                Text(
                    "Voice messages",
                    style = MaterialTheme.typography.labelLarge,
                    color = Color.Gray,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                )
                if (inboxByFrom.isEmpty()) {
                    Text("No new voice messages", modifier = Modifier.padding(16.dp), color = Color.Gray)
                } else {
                    inboxByFrom.forEach { (fromId, count) ->
                        val name = devices.firstOrNull { it.id == fromId }?.name ?: fromId
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clickable {
                                    scope.launch { drawerState.close() }
                                    screen = Screen.VoiceThread(fromId, name)
                                }
                                .padding(horizontal = 16.dp, vertical = 10.dp),
                            horizontalArrangement = Arrangement.SpaceBetween,
                        ) {
                            Text(name)
                            Text(
                                "$count",
                                color = Color.White,
                                modifier = Modifier
                                    .background(Color(0xFFDC3545), CircleShape)
                                    .padding(horizontal = 8.dp, vertical = 2.dp),
                            )
                        }
                    }
                }
            }
        },
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(8.dp),
                horizontalArrangement = Arrangement.SpaceEvenly,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                TextButton(onClick = { scope.launch { drawerState.open() } }) {
                    Text(if (drawerUnread > 0) "Chats ($drawerUnread)" else "Chats")
                }
                TextButton(onClick = { screen = Screen.Devices }) { Text("Devices") }
                TextButton(onClick = { screen = Screen.Settings }) { Text("Settings") }
                TextButton(onClick = { screen = Screen.About }) { Text("About") }
                TextButton(onClick = { closeApp(context) }) { Text("Close") }
            }
            when (val s = screen) {
                Screen.Devices -> PttScreen(
                    devices = devices,
                    inboxByFrom = inboxByFrom,
                    onVoiceMessage = { id, name -> screen = Screen.RecordVoice(id, name) },
                    onInvite = { id, name ->
                        scope.launch(Dispatchers.IO) {
                            val json = PTTService.instance?.inviteChannel(id)
                            if (json != null) {
                                val ch = JSONObject(json)
                                withContext(Dispatchers.Main) {
                                    screen = Screen.PrivateChannel(
                                        ch.optString("id"),
                                        id,
                                        name,
                                        ch.optString("status", "pending"),
                                    )
                                }
                            }
                        }
                    },
                    onOpenThread = { id, name -> screen = Screen.VoiceThread(id, name) },
                )
                Screen.Settings -> SettingsScreen()
                Screen.About -> AboutScreen()
                is Screen.VoiceThread -> VoiceThreadScreen(
                    peerId = s.peerId,
                    peerName = s.peerName,
                    onBack = { screen = Screen.Devices },
                    onReply = { screen = Screen.RecordVoice(s.peerId, s.peerName) },
                )
                is Screen.RecordVoice -> RecordVoiceScreen(
                    peerId = s.peerId,
                    peerName = s.peerName,
                    channelId = s.channelId,
                    onDone = {
                        screen = if (s.channelId.isNotEmpty()) {
                            Screen.PrivateChannel(s.channelId, s.peerId, s.peerName, "active")
                        } else {
                            Screen.VoiceThread(s.peerId, s.peerName)
                        }
                    },
                    onCancel = { screen = Screen.Devices },
                )
                is Screen.PrivateChannel -> PrivateChannelScreen(
                    channelId = s.channelId,
                    peerId = s.peerId,
                    peerName = s.peerName,
                    status = s.status,
                    onBack = { screen = Screen.Devices },
                )
            }
        }
    }
}

private fun closeApp(context: Context) {
    context.stopService(Intent(context, PTTService::class.java))
    (context as? android.app.Activity)?.finishAndRemoveTask()
}

@Composable
private fun SettingsScreen() {
    val context = LocalContext.current
    var nickname by remember { mutableStateOf(NicknameStore.get(context)) }
    var saved by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(text = "Settings", style = MaterialTheme.typography.titleMedium)
        OutlinedTextField(
            value = nickname,
            onValueChange = { nickname = it; saved = false },
            label = { Text("Nickname / User name") },
            modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
        )
        Text(
            text = "Leave blank to use this device's manufacturer/model as its display name.",
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(top = 4.dp),
        )
        Button(
            onClick = {
                PTTService.instance?.updateName(nickname)
                saved = true
            },
            modifier = Modifier.padding(top = 16.dp),
        ) { Text("Save") }
        if (saved) {
            Text(text = "Saved.", modifier = Modifier.padding(top = 8.dp))
        }
    }
}

@Composable
private fun AboutScreen() {
    var selfId by remember { mutableStateOf("") }
    var baseUrl by remember { mutableStateOf("") }
    val uriHandler = LocalUriHandler.current
    val scroll = rememberScrollState()

    LaunchedEffect(Unit) {
        while (true) {
            selfId = PTTService.instance?.selfId() ?: ""
            baseUrl = PTTService.instance?.baseStationURL() ?: ""
            delay(500)
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(scroll)
            .padding(24.dp),
    ) {
        Text(text = "About WalkieTalkie", style = MaterialTheme.typography.titleMedium)
        Text(text = "Version: ${BuildConfig.VERSION_NAME}", modifier = Modifier.padding(top = 16.dp))
        Text(text = "Platform: android", modifier = Modifier.padding(top = 8.dp))
        Text(text = "Device ID: $selfId", modifier = Modifier.padding(top = 8.dp))
        if (baseUrl.isBlank()) {
            Text(
                text = "Base Station: not discovered yet",
                modifier = Modifier.padding(top = 8.dp),
            )
        } else {
            Text(
                text = "Base Station: $baseUrl",
                color = MaterialTheme.colorScheme.primary,
                modifier = Modifier
                    .padding(top = 8.dp)
                    .clickable { uriHandler.openUri(baseUrl) },
            )
        }

        Text(
            text = "What it is",
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.padding(top = 24.dp),
        )
        Text(
            text = "WalkieTalkie is a LAN push-to-talk mesh. Hold Talk and other devices on the " +
                "same Wi‑Fi hear you live — no accounts, no manual pairing. This Android app " +
                "joins the same mesh as iPhone, Wear OS, Apple Watch (via iPhone), and a " +
                "desktop Base Station.",
            modifier = Modifier.padding(top = 8.dp),
            style = MaterialTheme.typography.bodyMedium,
        )
        Text(
            text = "How discovery works",
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.padding(top = 16.dp),
        )
        Text(
            text = "Peers find each other with mDNS over Wi‑Fi. Off the LAN, nearby phones and " +
                "watches can still appear over Bluetooth LE as presence-only (id and name, " +
                "no live audio until you're back on Wi‑Fi together).",
            modifier = Modifier.padding(top = 8.dp),
            style = MaterialTheme.typography.bodyMedium,
        )
        Text(
            text = "The Base Station",
            style = MaterialTheme.typography.titleSmall,
            modifier = Modifier.padding(top = 16.dp),
        )
        Text(
            text = "The Base Station is the desktop companion: web dashboard, mesh hub, and " +
                "store-and-forward for voice notes and private channels. Tap the Base Station " +
                "URL above to open its web UI in a browser. Group Hold-to-talk works peer-to-peer " +
                "without one; voice messages and private chats need a Base Station on the LAN.",
            modifier = Modifier.padding(top = 8.dp),
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
private fun PttScreen(
    devices: List<DeviceRow>,
    inboxByFrom: Map<String, Int>,
    onVoiceMessage: (id: String, name: String) -> Unit,
    onInvite: (id: String, name: String) -> Unit,
    onOpenThread: (id: String, name: String) -> Unit,
) {
    var talking by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.SpaceBetween,
    ) {
        Column(modifier = Modifier.fillMaxWidth().weight(1f, fill = false)) {
            Text(text = "Devices", style = MaterialTheme.typography.titleMedium)
            if (devices.isEmpty()) {
                Text(text = "No devices seen yet.", modifier = Modifier.padding(top = 8.dp))
            } else {
                LazyColumn(modifier = Modifier.padding(top = 8.dp).fillMaxHeight(0.55f)) {
                    items(devices, key = { it.id }) { d ->
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(text = d.name, color = statusColor(d.status))
                                Text(
                                    text = d.status,
                                    style = MaterialTheme.typography.bodySmall,
                                    color = Color.Gray,
                                )
                                val pending = inboxByFrom[d.id] ?: 0
                                if (pending > 0) {
                                    Text(
                                        text = "$pending new voice note${if (pending == 1) "" else "s"}",
                                        color = Color(0xFF0D6EFD),
                                        style = MaterialTheme.typography.bodySmall,
                                        modifier = Modifier.clickable { onOpenThread(d.id, d.name) },
                                    )
                                }
                            }
                            TextButton(
                                onClick = { onVoiceMessage(d.id, d.name) },
                            ) { Text("🎤") }
                            if (d.status == "connected") {
                                TextButton(
                                    onClick = { onInvite(d.id, d.name) },
                                ) { Text("💬") }
                            }
                        }
                    }
                }
            }
        }

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(140.dp)
                .padding(bottom = 16.dp)
                .background(
                    color = if (talking) Color(0xFFC0392B) else Color(0xFF1F6F43),
                    shape = CircleShape,
                )
                .pointerInput(Unit) {
                    detectTapGestures(
                        onPress = {
                            talking = true
                            PTTService.instance?.startTalking()
                            tryAwaitRelease()
                            talking = false
                            PTTService.instance?.stopTalking()
                        },
                    )
                },
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = if (talking) "Talking…" else "Hold to talk",
                color = Color.White,
                style = MaterialTheme.typography.headlineSmall,
            )
        }
    }
}

@Composable
private fun VoiceThreadScreen(
    peerId: String,
    peerName: String,
    onBack: () -> Unit,
    onReply: () -> Unit,
) {
    val context = LocalContext.current
    var notesJson by remember { mutableStateOf("[]") }
    val scope = rememberCoroutineScope()
    var player by remember { mutableStateOf<MediaPlayer?>(null) }

    DisposableEffect(Unit) {
        onDispose {
            player?.release()
            player = null
        }
    }

    LaunchedEffect(peerId) {
        while (true) {
            notesJson = PTTService.instance?.listVoiceNotesJSON(peerId) ?: "[]"
            delay(2000)
        }
    }

    val notes = remember(notesJson) { parseNotes(notesJson) }
    val selfId = PTTService.instance?.selfId() ?: ""

    Column(modifier = Modifier.fillMaxSize().padding(16.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text("← Back") }
            Text("Voice notes with $peerName", style = MaterialTheme.typography.titleMedium)
        }
        LazyColumn(modifier = Modifier.weight(1f).padding(top = 8.dp)) {
            items(notes, key = { it.id }) { n ->
                val dir = if (n.fromId == selfId) "You" else peerName
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 6.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("$dir · ${n.status}", modifier = Modifier.weight(1f))
                    TextButton(onClick = {
                        scope.launch(Dispatchers.IO) {
                            val bytes = PTTService.instance?.downloadVoiceNote(n.id) ?: return@launch
                            PTTService.instance?.ackVoiceNote(n.id)
                            val tmp = File(context.cacheDir, "play-${n.id}.ogg")
                            tmp.writeBytes(bytes)
                            withContext(Dispatchers.Main) {
                                player?.release()
                                player = MediaPlayer().apply {
                                    setDataSource(tmp.absolutePath)
                                    prepare()
                                    start()
                                    setOnCompletionListener {
                                        tmp.delete()
                                    }
                                }
                            }
                        }
                    }) { Text("Play") }
                    TextButton(onClick = {
                        scope.launch(Dispatchers.IO) {
                            PTTService.instance?.deleteVoiceNote(n.id)
                            notesJson = PTTService.instance?.listVoiceNotesJSON(peerId) ?: "[]"
                        }
                    }) { Text("Del") }
                }
            }
        }
        Button(onClick = onReply, modifier = Modifier.fillMaxWidth()) { Text("Reply") }
    }
}

@Composable
private fun RecordVoiceScreen(
    peerId: String,
    peerName: String,
    channelId: String,
    onDone: () -> Unit,
    onCancel: () -> Unit,
) {
    val context = LocalContext.current
    val recorder = remember { ClipRecorder(context) }
    var recording by remember { mutableStateOf(false) }
    var status by remember { mutableStateOf("Press Start to record.") }
    val scope = rememberCoroutineScope()

    DisposableEffect(Unit) {
        onDispose { recorder.cancel() }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text("Voice Message", style = MaterialTheme.typography.titleMedium)
        Text("To: $peerName", modifier = Modifier.padding(top = 8.dp))
        Text(status, modifier = Modifier.padding(top = 16.dp), color = Color.Gray)
        Spacer(Modifier.height(24.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Button(
                onClick = {
                    try {
                        recorder.start()
                        recording = true
                        status = "Recording…"
                    } catch (e: Exception) {
                        status = "Mic error: ${e.message}"
                    }
                },
                enabled = !recording,
            ) { Text("Start") }
            Button(
                onClick = {
                    scope.launch(Dispatchers.IO) {
                        try {
                            val bytes = recorder.stopAndRead()
                            withContext(Dispatchers.Main) {
                                recording = false
                                status = "Sending…"
                            }
                            val err = if (channelId.isNotEmpty()) {
                                PTTService.instance?.sendChannelClip(channelId, bytes)
                            } else {
                                PTTService.instance?.sendVoiceNote(peerId, bytes)
                            }
                            withContext(Dispatchers.Main) {
                                if (err == null) {
                                    status = "Sent."
                                    onDone()
                                } else {
                                    status = "Send failed: $err"
                                }
                            }
                        } catch (e: Exception) {
                            withContext(Dispatchers.Main) {
                                recording = false
                                status = "Error: ${e.message}"
                            }
                        }
                    }
                },
                enabled = recording,
            ) { Text("Stop & Send") }
            TextButton(onClick = {
                recorder.cancel()
                recording = false
                onCancel()
            }) { Text("Cancel") }
        }
    }
}

@Composable
@Suppress("UNUSED_PARAMETER")
private fun PrivateChannelScreen(
    channelId: String,
    peerId: String,
    peerName: String,
    status: String,
    onBack: () -> Unit,
) {
    val context = LocalContext.current
    var notesJson by remember { mutableStateOf("[]") }
    var talking by remember { mutableStateOf(false) }
    val recorder = remember { ClipRecorder(context) }
    val scope = rememberCoroutineScope()
    var player by remember { mutableStateOf<MediaPlayer?>(null) }

    DisposableEffect(channelId) {
        if (status == "pending") {
            PTTService.instance?.acceptChannel(channelId)
        }
        PTTService.instance?.focusChannel(channelId)
        onDispose {
            PTTService.instance?.blurChannel(channelId)
            player?.release()
            player = null
            recorder.cancel()
        }
    }

    LaunchedEffect(channelId) {
        while (true) {
            notesJson = PTTService.instance?.listChannelNotesJSON(channelId) ?: "[]"
            val notes = parseNotes(notesJson)
            val selfId = PTTService.instance?.selfId() ?: ""
            notes.filter { it.toId == selfId && it.status == "queued" }.forEach { n ->
                // Auto-ack when focused so the queue clears (auto-play on open).
                val bytes = PTTService.instance?.downloadVoiceNote(n.id)
                if (bytes != null) {
                    PTTService.instance?.ackVoiceNote(n.id)
                    // Play latest only once per poll cycle if not already playing
                }
            }
            delay(2000)
        }
    }

    val notes = remember(notesJson) { parseNotes(notesJson) }
    val selfId = PTTService.instance?.selfId() ?: ""

    Column(
        modifier = Modifier.fillMaxSize().padding(16.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            TextButton(onClick = onBack) { Text("← Group") }
            Text("Private: $peerName", style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.width(48.dp))
        }

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(140.dp)
                .padding(vertical = 16.dp)
                .background(
                    color = if (talking) Color(0xFFC0392B) else Color(0xFF1F6F43),
                    shape = CircleShape,
                )
                .pointerInput(Unit) {
                    detectTapGestures(
                        onPress = {
                            talking = true
                            val live = PTTService.instance?.isDirectlyConnected(peerId) == true
                            if (live) {
                                PTTService.instance?.startTalkingTo(peerId)
                                try {
                                    tryAwaitRelease()
                                } finally {
                                    talking = false
                                    PTTService.instance?.stopTalking()
                                }
                                return@detectTapGestures
                            }
                            try {
                                recorder.start()
                            } catch (_: Exception) {
                                talking = false
                                return@detectTapGestures
                            }
                            tryAwaitRelease()
                            talking = false
                            scope.launch(Dispatchers.IO) {
                                try {
                                    val bytes = recorder.stopAndRead()
                                    PTTService.instance?.sendChannelClip(channelId, bytes)
                                    notesJson = PTTService.instance?.listChannelNotesJSON(channelId) ?: "[]"
                                } catch (e: Exception) {
                                    recorder.cancel()
                                }
                            }
                        },
                    )
                },
            contentAlignment = Alignment.Center,
        ) {
            Text(
                text = if (talking) "Talking…" else "Hold to talk",
                color = Color.White,
                style = MaterialTheme.typography.headlineSmall,
            )
        }

        Text(
            "Live when the peer is on Wi‑Fi mesh; otherwise clips queue until they focus this channel.",
            style = MaterialTheme.typography.bodySmall,
            color = Color.Gray,
        )

        LazyColumn(modifier = Modifier.fillMaxWidth().weight(1f).padding(top = 8.dp)) {
            items(notes, key = { it.id }) { n ->
                val dir = if (n.fromId == selfId) "You" else peerName
                Row(
                    modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                ) {
                    Text(dir)
                    TextButton(onClick = {
                        scope.launch(Dispatchers.IO) {
                            val bytes = PTTService.instance?.downloadVoiceNote(n.id) ?: return@launch
                            PTTService.instance?.ackVoiceNote(n.id)
                            val tmp = File(context.cacheDir, "play-${n.id}.ogg")
                            tmp.writeBytes(bytes)
                            withContext(Dispatchers.Main) {
                                player?.release()
                                player = MediaPlayer().apply {
                                    setDataSource(tmp.absolutePath)
                                    prepare()
                                    start()
                                }
                            }
                        }
                    }) { Text("Play") }
                }
            }
        }
    }
}

// --- data helpers ---

private data class DeviceRow(val id: String, val name: String, val status: String)

private data class ChannelRow(
    val id: String,
    val peerId: String,
    val peerName: String,
    val status: String,
    val unread: Int,
)

private data class NoteRow(
    val id: String,
    val fromId: String,
    val toId: String,
    val status: String,
    val channelId: String,
)

private fun parseDevices(json: String): List<DeviceRow> {
    return try {
        val arr = JSONArray(json)
        (0 until arr.length()).map { i ->
            val obj = arr.getJSONObject(i)
            DeviceRow(
                id = obj.optString("id", ""),
                name = obj.optString("name", "unknown"),
                status = obj.optString("status", ""),
            )
        }.sortedByDescending { /* keep registry order; name sort fallback */ it.name }
            .sortedWith(compareByDescending<DeviceRow> { it.status == "connected" })
    } catch (_: Exception) {
        emptyList()
    }
}

private fun parseChannels(json: String): List<ChannelRow> {
    return try {
        val arr = JSONArray(json)
        (0 until arr.length()).map { i ->
            val obj = arr.getJSONObject(i)
            ChannelRow(
                id = obj.optString("id"),
                peerId = obj.optString("peerId"),
                peerName = obj.optString("peerName").ifBlank { obj.optString("peerId") },
                status = obj.optString("status"),
                unread = obj.optInt("unreadFor", 0),
            )
        }
    } catch (_: Exception) {
        emptyList()
    }
}

private fun parseNotes(json: String): List<NoteRow> {
    return try {
        val arr = JSONArray(json)
        (0 until arr.length()).map { i ->
            val obj = arr.getJSONObject(i)
            NoteRow(
                id = obj.optString("id"),
                fromId = obj.optString("fromId"),
                toId = obj.optString("toId"),
                status = obj.optString("status"),
                channelId = obj.optString("channelId"),
            )
        }
    } catch (_: Exception) {
        emptyList()
    }
}

/** Queued direct (non-channel) notes addressed to self, grouped by sender. */
private fun countQueuedInbox(json: String, selfId: String): Map<String, Int> {
    if (selfId.isEmpty()) return emptyMap()
    val out = mutableMapOf<String, Int>()
    try {
        val arr = JSONArray(json)
        for (i in 0 until arr.length()) {
            val obj = arr.getJSONObject(i)
            if (obj.optString("status") != "queued") continue
            if (obj.optString("channelId").isNotEmpty()) continue
            if (obj.optString("toId") != selfId) continue
            val from = obj.optString("fromId")
            out[from] = (out[from] ?: 0) + 1
        }
    } catch (_: Exception) {
    }
    return out
}

private fun statusColor(status: String): Color =
    if (status == "connected") Color(0xFF198754) else Color(0xFF6C757D)
