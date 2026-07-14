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
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.wear.compose.foundation.lazy.ScalingLazyColumn
import androidx.wear.compose.foundation.lazy.rememberScalingLazyListState
import androidx.wear.compose.material.Button
import androidx.wear.compose.material.MaterialTheme
import androidx.wear.compose.material.Text
import com.walkietalkie.ptt.PTTService
import com.walkietalkie.settings.NicknameStore
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
                WearApp(
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
private fun WearApp(
    hasMic: Boolean,
    onRequestPermissions: () -> Unit,
    onPressTalk: () -> Unit,
    onReleaseTalk: () -> Unit,
) {
    var peerCount by remember { mutableIntStateOf(0) }
    var baseStation by remember { mutableStateOf<String?>(null) }
    var selfId by remember { mutableStateOf("") }

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
                    selfId = svc.selfId()
                } catch (_: Exception) {
                    // Node still starting
                }
            }
            delay(1_500)
        }
    }

    val pagerState = rememberPagerState(pageCount = { 3 })
    HorizontalPager(
        state = pagerState,
        modifier = Modifier.fillMaxSize(),
    ) { page ->
        when (page) {
            0 -> WearTalkPage(
                hasMic = hasMic,
                peerCount = peerCount,
                baseStation = baseStation,
                onRequestPermissions = onRequestPermissions,
                onPressTalk = onPressTalk,
                onReleaseTalk = onReleaseTalk,
            )
            1 -> WearSettingsPage()
            else -> WearAboutPage(
                selfId = selfId,
                baseStation = baseStation,
                peerCount = peerCount,
            )
        }
    }
}

@Composable
private fun WearTalkPage(
    hasMic: Boolean,
    peerCount: Int,
    baseStation: String?,
    onRequestPermissions: () -> Unit,
    onPressTalk: () -> Unit,
    onReleaseTalk: () -> Unit,
) {
    var talking by remember { mutableStateOf(false) }

    DisposableEffect(Unit) {
        onDispose {
            if (talking) onReleaseTalk()
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
            verticalArrangement = Arrangement.spacedBy(8.dp),
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
                        .size(88.dp)
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
            Text(
                text = stringResource(R.string.swipe_hint),
                style = MaterialTheme.typography.caption3,
                color = MaterialTheme.colors.onBackground.copy(alpha = 0.5f),
            )
        }
    }
}

@Composable
private fun WearSettingsPage() {
    val context = LocalContext.current
    var nickname by remember { mutableStateOf(NicknameStore.get(context)) }
    var saved by remember { mutableStateOf(false) }
    val listState = rememberScalingLazyListState()

    ScalingLazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colors.background)
            .padding(horizontal = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        item {
            Text(
                text = stringResource(R.string.settings_title),
                style = MaterialTheme.typography.title3,
                textAlign = TextAlign.Center,
            )
        }
        item {
            Text(
                text = stringResource(R.string.nickname_label),
                style = MaterialTheme.typography.caption2,
                modifier = Modifier.padding(top = 8.dp),
            )
        }
        item {
            BasicTextField(
                value = nickname,
                onValueChange = {
                    nickname = it
                    saved = false
                },
                singleLine = true,
                cursorBrush = SolidColor(MaterialTheme.colors.primary),
                textStyle = MaterialTheme.typography.body2.copy(
                    color = MaterialTheme.colors.onBackground,
                    textAlign = TextAlign.Center,
                ),
                modifier = Modifier
                    .fillMaxWidth()
                    .background(MaterialTheme.colors.surface, RoundedCornerShape(8.dp))
                    .padding(8.dp),
            )
        }
        item {
            Button(
                onClick = {
                    val name = nickname.trim()
                    if (PTTService.instance != null) {
                        PTTService.instance?.updateName(name)
                    } else {
                        NicknameStore.set(context, name)
                    }
                    saved = true
                },
            ) {
                Text(stringResource(R.string.save))
            }
        }
        if (saved) {
            item {
                Text(
                    text = stringResource(R.string.saved),
                    style = MaterialTheme.typography.caption2,
                    modifier = Modifier.padding(top = 4.dp),
                )
            }
        }
    }
}

@Composable
private fun WearAboutPage(
    selfId: String,
    baseStation: String?,
    peerCount: Int,
) {
    val uriHandler = LocalUriHandler.current
    val listState = rememberScalingLazyListState()
    val version = BuildConfig.VERSION_NAME

    ScalingLazyColumn(
        state = listState,
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colors.background)
            .padding(horizontal = 10.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        item {
            Text(
                text = stringResource(R.string.about_title),
                style = MaterialTheme.typography.title3,
                textAlign = TextAlign.Center,
            )
        }
        item {
            Text(
                text = "v$version · wear",
                style = MaterialTheme.typography.caption2,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        item {
            Text(
                text = stringResource(R.string.peers_fmt, peerCount),
                style = MaterialTheme.typography.caption2,
            )
        }
        item {
            Text(
                text = if (selfId.isBlank()) "…" else selfId.take(8) + "…",
                style = MaterialTheme.typography.caption3,
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
        item {
            if (baseStation.isNullOrBlank()) {
                Text(
                    text = stringResource(R.string.bs_none),
                    style = MaterialTheme.typography.caption2,
                    modifier = Modifier.padding(top = 8.dp),
                )
            } else {
                Text(
                    text = baseStation,
                    style = MaterialTheme.typography.caption2,
                    color = MaterialTheme.colors.primary,
                    textAlign = TextAlign.Center,
                    modifier = Modifier
                        .padding(top = 8.dp)
                        .clickable { uriHandler.openUri(baseStation) },
                )
            }
        }
        item {
            Text(
                text = stringResource(R.string.about_body),
                style = MaterialTheme.typography.caption3,
                textAlign = TextAlign.Center,
                color = MaterialTheme.colors.onBackground.copy(alpha = 0.75f),
                modifier = Modifier.padding(top = 10.dp, bottom = 16.dp),
            )
        }
    }
}
