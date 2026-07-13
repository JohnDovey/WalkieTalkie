package com.walkietalkie

import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.walkietalkie.ptt.PTTService
import com.walkietalkie.settings.NicknameStore
import kotlinx.coroutines.delay
import org.json.JSONArray

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

        setContent {
            MaterialTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    AppScreen()
                }
            }
        }
    }

    private fun startPttService() {
        val intent = Intent(this, PTTService::class.java)
        ContextCompat.startForegroundService(this, intent)
    }
}

private enum class Screen { Devices, Settings, About }

@Composable
private fun AppScreen() {
    var screen by remember { mutableStateOf(Screen.Devices) }
    val context = LocalContext.current

    Column(modifier = Modifier.fillMaxSize()) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(8.dp),
            horizontalArrangement = Arrangement.SpaceEvenly,
        ) {
            TextButton(onClick = { screen = Screen.Devices }) { Text("Devices") }
            TextButton(onClick = { screen = Screen.Settings }) { Text("Settings") }
            TextButton(onClick = { screen = Screen.About }) { Text("About") }
            TextButton(onClick = { closeApp(context) }) { Text("Close") }
        }
        when (screen) {
            Screen.Devices -> PttScreen()
            Screen.Settings -> SettingsScreen()
            Screen.About -> AboutScreen()
        }
    }
}

// Stops the foreground service (releasing the mic/mesh/mDNS entirely,
// not just backgrounding) and closes the app — the explicit way to fully
// quit, rather than relying on swiping away from Recents.
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

    androidx.compose.runtime.LaunchedEffect(Unit) {
        while (true) {
            selfId = PTTService.instance?.selfId() ?: ""
            if (selfId.isNotEmpty()) break
            delay(500)
        }
    }

    Column(modifier = Modifier.fillMaxSize().padding(24.dp)) {
        Text(text = "About WalkieTalkie", style = MaterialTheme.typography.titleMedium)
        Text(text = "Version: ${BuildConfig.VERSION_NAME}", modifier = Modifier.padding(top = 16.dp))
        Text(text = "Platform: android", modifier = Modifier.padding(top = 8.dp))
        Text(text = "Device ID: $selfId", modifier = Modifier.padding(top = 8.dp))
    }
}

@Composable
private fun PttScreen() {
    var talking by remember { mutableStateOf(false) }
    var devicesJson by remember { mutableStateOf("[]") }

    androidx.compose.runtime.LaunchedEffect(Unit) {
        while (true) {
            devicesJson = PTTService.instance?.listDevicesJSON() ?: "[]"
            delay(2000)
        }
    }

    Column(
        modifier = Modifier.fillMaxSize().padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.SpaceBetween,
    ) {
        Column(modifier = Modifier.fillMaxWidth()) {
            Text(text = "Devices", style = MaterialTheme.typography.titleMedium)
            val devices = parseDevices(devicesJson)
            if (devices.isEmpty()) {
                Text(text = "No devices seen yet.", modifier = Modifier.padding(top = 8.dp))
            } else {
                LazyColumn(modifier = Modifier.padding(top = 8.dp)) {
                    items(devices) { d ->
                        Row {
                            Text(text = d.name, color = statusColor(d.status))
                            Text(text = " (${d.status})")
                        }
                    }
                }
            }
        }

        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(160.dp)
                .padding(bottom = 32.dp)
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

private data class DeviceRow(val name: String, val status: String)

private fun parseDevices(json: String): List<DeviceRow> {
    return try {
        val arr = JSONArray(json)
        (0 until arr.length()).map { i ->
            val obj = arr.getJSONObject(i)
            DeviceRow(obj.optString("name", "unknown"), obj.optString("status", ""))
        }
    } catch (e: Exception) {
        emptyList()
    }
}

// Matches the web UI's name coloring (server/web/static/app.js: nameColorClass) —
// Bootstrap's text-success/text-secondary colors.
private fun statusColor(status: String): Color =
    if (status == "connected") Color(0xFF198754) else Color(0xFF6C757D)
