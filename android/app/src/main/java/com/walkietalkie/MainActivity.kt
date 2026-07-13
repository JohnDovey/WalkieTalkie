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
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import com.walkietalkie.ptt.PTTService
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
                    PttScreen()
                }
            }
        }
    }

    private fun startPttService() {
        val intent = Intent(this, PTTService::class.java)
        ContextCompat.startForegroundService(this, intent)
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
            val names = parseDeviceNames(devicesJson)
            if (names.isEmpty()) {
                Text(text = "No devices seen yet.", modifier = Modifier.padding(top = 8.dp))
            } else {
                LazyColumn(modifier = Modifier.padding(top = 8.dp)) {
                    items(names) { name -> Text(text = name) }
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

private fun parseDeviceNames(json: String): List<String> {
    return try {
        val arr = JSONArray(json)
        (0 until arr.length()).map { i ->
            val obj = arr.getJSONObject(i)
            val name = obj.optString("name", "unknown")
            val status = obj.optString("status", "")
            "$name ($status)"
        }
    } catch (e: Exception) {
        emptyList()
    }
}
