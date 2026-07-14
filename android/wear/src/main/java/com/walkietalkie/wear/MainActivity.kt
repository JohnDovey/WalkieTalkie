package com.walkietalkie.wear

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
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
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.wear.compose.material.Button
import androidx.wear.compose.material.MaterialTheme
import androidx.wear.compose.material.Text
import com.walkietalkie.ptt.PTTService
import kotlinx.coroutines.delay
import org.json.JSONArray

class MainActivity : ComponentActivity() {
    private val permissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { /* UI re-reads grants via hasRequiredPermissions */ }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        maybeRequestPermissions()
        if (hasRequiredPermissions()) {
            startPttService()
        }
        setContent {
            MaterialTheme {
                WearTalkScreen(
                    hasMic = hasRequiredPermissions(),
                    onRequestPermissions = { maybeRequestPermissions() },
                    onPressTalk = { sendTalk(true) },
                    onReleaseTalk = { sendTalk(false) },
                )
            }
        }
    }

    override fun onResume() {
        super.onResume()
        if (hasRequiredPermissions()) {
            startPttService()
        }
    }

    private fun startPttService() {
        ContextCompat.startForegroundService(
            this,
            Intent(this, PTTService::class.java),
        )
    }

    private fun sendTalk(start: Boolean) {
        val action = if (start) PTTService.ACTION_START_TALKING else PTTService.ACTION_STOP_TALKING
        startService(Intent(this, PTTService::class.java).setAction(action))
    }

    private fun maybeRequestPermissions() {
        if (!hasRequiredPermissions()) {
            permissionLauncher.launch(REQUIRED_PERMISSIONS)
        }
    }

    private fun hasRequiredPermissions(): Boolean =
        REQUIRED_PERMISSIONS.all {
            ContextCompat.checkSelfPermission(this, it) == PackageManager.PERMISSION_GRANTED
        }

    companion object {
        private val REQUIRED_PERMISSIONS = buildList {
            add(Manifest.permission.RECORD_AUDIO)
            add(Manifest.permission.ACCESS_FINE_LOCATION)
            add(Manifest.permission.ACCESS_COARSE_LOCATION)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                add(Manifest.permission.BLUETOOTH_SCAN)
                add(Manifest.permission.BLUETOOTH_ADVERTISE)
                add(Manifest.permission.BLUETOOTH_CONNECT)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                add(Manifest.permission.POST_NOTIFICATIONS)
            }
        }.toTypedArray()
    }
}

@Composable
private fun WearTalkScreen(
    hasMic: Boolean,
    onRequestPermissions: () -> Unit,
    onPressTalk: () -> Unit,
    onReleaseTalk: () -> Unit,
) {
    var talking by remember { mutableStateOf(false) }
    var peerCount by remember { mutableIntStateOf(0) }
    var baseStation by remember { mutableStateOf<String?>(null) }

    DisposableEffect(Unit) {
        onDispose {
            if (talking) onReleaseTalk()
        }
    }

    LaunchedEffect(Unit) {
        while (true) {
            val svc = PTTService.instance
            if (svc != null) {
                try {
                    val raw = svc.listDevicesJSON()
                    val arr = JSONArray(raw)
                    var connected = 0
                    for (i in 0 until arr.length()) {
                        val o = arr.getJSONObject(i)
                        if (o.optString("status") == "connected") {
                            connected++
                        }
                    }
                    peerCount = connected
                    baseStation = svc.baseStationURL().ifBlank { null }
                } catch (_: Exception) {
                    // Node still starting
                }
            }
            delay(1_500)
        }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colors.background)
            .padding(8.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(
                text = stringResource(R.string.peers_fmt, peerCount),
                style = MaterialTheme.typography.caption1,
                textAlign = TextAlign.Center,
            )
            Text(
                text = baseStation?.let { "BS" } ?: "no BS",
                style = MaterialTheme.typography.caption2,
                color = MaterialTheme.colors.onBackground.copy(alpha = 0.7f),
            )
            if (!hasMic) {
                Button(onClick = onRequestPermissions) {
                    Text(stringResource(R.string.grant_mic))
                }
            } else {
                val fill = if (talking) Color(0xFF2E7D32) else Color(0xFF1565C0)
                Box(
                    modifier = Modifier
                        .size(96.dp)
                        .background(fill, CircleShape)
                        .pointerInput(Unit) {
                            detectTapGestures(
                                onPress = {
                                    talking = true
                                    onPressTalk()
                                    try {
                                        tryAwaitRelease()
                                    } finally {
                                        talking = false
                                        onReleaseTalk()
                                    }
                                },
                            )
                        },
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = stringResource(
                            if (talking) R.string.ptt_talking else R.string.ptt_idle,
                        ),
                        style = MaterialTheme.typography.button,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.padding(8.dp),
                    )
                }
            }
        }
    }
}
