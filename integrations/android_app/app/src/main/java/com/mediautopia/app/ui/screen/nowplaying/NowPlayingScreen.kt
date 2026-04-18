package com.mediautopia.app.ui.screen.nowplaying

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Pause
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material.icons.filled.SkipNext
import androidx.compose.material.icons.filled.SkipPrevious
import androidx.compose.material.icons.filled.VolumeOff
import androidx.compose.material.icons.filled.VolumeUp
import androidx.compose.material.icons.outlined.Speaker
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import com.mediautopia.app.ui.components.HiResBadge
import com.mediautopia.app.ui.theme.OnSecondary
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.PrimaryContainer
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.Surface
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerHighest

private val ArtworkShape = RoundedCornerShape(12.dp)

@Composable
fun NowPlayingScreen(
    viewModel: NowPlayingViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()
    val audioSessionId by viewModel.audioSessionHolder.sessionId.collectAsStateWithLifecycle()

    // Request RECORD_AUDIO permission when visualizer is enabled.
    val context = androidx.compose.ui.platform.LocalContext.current
    val permissionGranted = remember {
        androidx.compose.runtime.mutableStateOf(
            context.checkSelfPermission(android.Manifest.permission.RECORD_AUDIO) ==
                android.content.pm.PackageManager.PERMISSION_GRANTED
        )
    }
    val permissionLauncher = androidx.activity.compose.rememberLauncherForActivityResult(
        contract = androidx.activity.result.contract.ActivityResultContracts.RequestPermission(),
    ) { granted -> permissionGranted.value = granted }

    androidx.compose.runtime.LaunchedEffect(uiState.visualizerEnabled) {
        if (uiState.visualizerEnabled && !permissionGranted.value) {
            permissionLauncher.launch(android.Manifest.permission.RECORD_AUDIO)
        }
    }

    NowPlayingContent(
        state = uiState,
        audioSessionId = if (permissionGranted.value) audioSessionId else 0,
        onTogglePlayPause = viewModel::togglePlayPause,
        onNext = viewModel::next,
        onPrevious = viewModel::previous,
        onSeek = viewModel::seek,
        onSetVolume = viewModel::setVolume,
        onToggleMute = viewModel::toggleMute,
        onToggleShuffle = viewModel::toggleShuffle,
        onToggleRepeat = viewModel::toggleRepeat,
        onSetZoneVolume = viewModel::setPanelZoneVolume,
        onToggleZoneMute = viewModel::togglePanelZoneMute,
        onToggleZoneAssignment = viewModel::togglePanelZoneAssignment,
    )
}

@Composable
private fun NowPlayingContent(
    state: NowPlayingUiState,
    audioSessionId: Int = 0,
    onTogglePlayPause: () -> Unit,
    onNext: () -> Unit,
    onPrevious: () -> Unit,
    onSeek: (Long) -> Unit,
    onSetVolume: (Float) -> Unit,
    onToggleMute: () -> Unit,
    onToggleShuffle: () -> Unit,
    onToggleRepeat: () -> Unit,
    onSetZoneVolume: (String, Float) -> Unit,
    onToggleZoneMute: (String) -> Unit,
    onToggleZoneAssignment: (String, Boolean) -> Unit,
) {
    val blockedByLease = state.leaseOwner != null && !state.isOwnLease
    // Peek-bar reserve so the main scroll content isn't hidden under the panel.
    val peekReserve = 72.dp

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(
                Brush.verticalGradient(
                    colors = listOf(
                        PrimaryContainer.copy(alpha = 0.7f),
                        Surface,
                    ),
                )
            ),
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 24.dp)
                .padding(bottom = peekReserve),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(modifier = Modifier.height(12.dp))

            ArtworkSection(
                artworkUrl = state.artworkUrl,
                hiResInfo = state.hiResInfo,
            )

            if (state.visualizerEnabled && state.isLocalRenderer && state.playbackStatus == "playing" && audioSessionId != 0) {
                Spacer(modifier = Modifier.height(8.dp))
                com.mediautopia.app.ui.components.AudioVisualizer(
                    audioSessionId = audioSessionId,
                    modifier = Modifier.padding(horizontal = 8.dp),
                )
            }

            Spacer(modifier = Modifier.height(20.dp))

            MetadataSection(
                title = state.trackTitle,
                artist = state.artist,
                album = state.album,
            )

            Spacer(modifier = Modifier.height(16.dp))

            SeekSection(
                positionMs = state.positionMs,
                durationMs = state.durationMs,
                onSeek = onSeek,
            )

            Spacer(modifier = Modifier.height(8.dp))

            TransportControls(
                playbackStatus = state.playbackStatus,
                shuffle = state.shuffle,
                repeatMode = state.repeatMode,
                blocked = blockedByLease,
                onTogglePlayPause = onTogglePlayPause,
                onNext = onNext,
                onPrevious = onPrevious,
                onToggleShuffle = onToggleShuffle,
                onToggleRepeat = onToggleRepeat,
            )

            if (blockedByLease) {
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = "Controlled by ${state.leaseOwner!!.uppercase()} — take control in the renderers menu",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }

        PlaybackPanel(
            rendererVolume = state.volume,
            rendererIsMuted = state.isMuted,
            rendererName = state.rendererName,
            blocked = blockedByLease,
            zones = state.panelZones,
            assignedZoneCount = state.assignedZoneCount,
            canToggleZoneAssignment = state.zoneAssignmentSupported,
            onSetRendererVolume = if (blockedByLease) ({ /* no-op */ }) else onSetVolume,
            onToggleRendererMute = if (blockedByLease) ({}) else onToggleMute,
            onSetZoneVolume = onSetZoneVolume,
            onToggleZoneMute = onToggleZoneMute,
            onToggleZoneAssignment = onToggleZoneAssignment,
            modifier = Modifier.align(Alignment.BottomCenter),
        )
    }
}

@Composable
private fun RendererIndicator(name: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Speaker,
            contentDescription = null,
            tint = Secondary,
            modifier = Modifier.size(16.dp),
        )
        Spacer(modifier = Modifier.width(6.dp))
        Text(
            text = name.uppercase(),
            style = MaterialTheme.typography.labelSmall,
            color = Secondary,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

@Composable
private fun ArtworkSection(
    artworkUrl: String?,
    hiResInfo: String?,
) {
    Box(
        contentAlignment = Alignment.TopEnd,
    ) {
        if (artworkUrl != null) {
            AsyncImage(
                model = artworkUrl,
                contentDescription = "Album artwork",
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .fillMaxWidth(0.85f)
                    .aspectRatio(1f)
                    .clip(ArtworkShape),
            )
        } else {
            Box(
                modifier = Modifier
                    .fillMaxWidth(0.85f)
                    .aspectRatio(1f)
                    .clip(ArtworkShape)
                    .background(SurfaceContainerHigh),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Filled.MusicNote,
                    contentDescription = "No artwork",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f),
                    modifier = Modifier.size(80.dp),
                )
            }
        }

        if (hiResInfo != null) {
            HiResBadge(
                text = hiResInfo,
                modifier = Modifier.padding(12.dp),
            )
        }
    }
}

@Composable
private fun MetadataSection(
    title: String?,
    artist: String?,
    album: String?,
) {
    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = title ?: "No track",
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.Center,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )

        if (artist != null) {
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = artist,
                style = MaterialTheme.typography.bodyLarge,
                color = Primary,
                textAlign = TextAlign.Center,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        if (album != null) {
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = album.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                textAlign = TextAlign.Center,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SeekSection(
    positionMs: Long,
    durationMs: Long,
    onSeek: (Long) -> Unit,
) {
    val sliderPosition = if (durationMs > 0) {
        positionMs.toFloat() / durationMs.toFloat()
    } else {
        0f
    }

    var isSeeking by remember { mutableStateOf(false) }
    var seekValue by remember { mutableStateOf(0f) }
    val interactionSource = remember { MutableInteractionSource() }

    Column(modifier = Modifier.fillMaxWidth()) {
        Slider(
            value = if (isSeeking) seekValue else sliderPosition,
            onValueChange = { value ->
                isSeeking = true
                seekValue = value
            },
            onValueChangeFinished = {
                isSeeking = false
                onSeek((seekValue * durationMs).toLong())
            },
            colors = SliderDefaults.colors(
                thumbColor = Primary,
                activeTrackColor = Primary,
                inactiveTrackColor = SurfaceContainerHighest,
            ),
            interactionSource = interactionSource,
            modifier = Modifier.fillMaxWidth(),
        )

        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 4.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Text(
                text = formatDuration(if (isSeeking) (seekValue * durationMs).toLong() else positionMs),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = "-${formatDuration(durationMs - (if (isSeeking) (seekValue * durationMs).toLong() else positionMs))}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun TransportControls(
    playbackStatus: String,
    shuffle: Boolean,
    repeatMode: String,
    blocked: Boolean,
    onTogglePlayPause: () -> Unit,
    onNext: () -> Unit,
    onPrevious: () -> Unit,
    onToggleShuffle: () -> Unit,
    onToggleRepeat: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        IconButton(onClick = onToggleShuffle, enabled = !blocked) {
            Icon(
                imageVector = Icons.Filled.Shuffle,
                contentDescription = "Shuffle",
                tint = if (shuffle) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        }

        Spacer(modifier = Modifier.width(12.dp))

        IconButton(onClick = onPrevious, enabled = !blocked) {
            Icon(
                imageVector = Icons.Filled.SkipPrevious,
                contentDescription = "Previous",
                tint = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.size(32.dp),
            )
        }

        Spacer(modifier = Modifier.width(12.dp))

        PlayPauseButton(
            isPlaying = playbackStatus == "playing",
            enabled = !blocked,
            onClick = onTogglePlayPause,
        )

        Spacer(modifier = Modifier.width(12.dp))

        IconButton(onClick = onNext, enabled = !blocked) {
            Icon(
                imageVector = Icons.Filled.SkipNext,
                contentDescription = "Next",
                tint = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.size(32.dp),
            )
        }

        Spacer(modifier = Modifier.width(12.dp))

        IconButton(onClick = onToggleRepeat, enabled = !blocked) {
            Icon(
                imageVector = if (repeatMode == "one") Icons.Filled.RepeatOne else Icons.Filled.Repeat,
                contentDescription = "Repeat",
                tint = if (repeatMode == "all" || repeatMode == "one") Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(24.dp),
            )
        }
    }
}

@Composable
private fun PlayPauseButton(
    isPlaying: Boolean,
    enabled: Boolean = true,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(64.dp)
            .clip(CircleShape)
            .background(if (enabled) Secondary else Secondary.copy(alpha = 0.4f))
            .then(if (enabled) Modifier.clickable(onClick = onClick) else Modifier),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = if (isPlaying) Icons.Filled.Pause else Icons.Filled.PlayArrow,
            contentDescription = if (isPlaying) "Pause" else "Play",
            tint = OnSecondary,
            modifier = Modifier.size(36.dp),
        )
    }
}

// ---------------------------------------------------------------------------
// PlaybackPanel — slide-up panel containing renderer volume + zones list.
// ---------------------------------------------------------------------------

@Composable
private fun PlaybackPanel(
    rendererVolume: Float,
    rendererIsMuted: Boolean,
    rendererName: String,
    blocked: Boolean,
    zones: List<com.mediautopia.app.ui.screen.nowplaying.PanelZone>,
    assignedZoneCount: Int,
    canToggleZoneAssignment: Boolean,
    onSetRendererVolume: (Float) -> Unit,
    onToggleRendererMute: () -> Unit,
    onSetZoneVolume: (String, Float) -> Unit,
    onToggleZoneMute: (String) -> Unit,
    onToggleZoneAssignment: (String, Boolean) -> Unit,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    val chevronRotation by animateFloatAsState(
        targetValue = if (expanded) 180f else 0f,
        animationSpec = tween(durationMillis = 200),
        label = "panelChevron",
    )

    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(topStart = 18.dp, topEnd = 18.dp))
            .background(SurfaceContainerHigh.copy(alpha = 0.96f)),
    ) {
        // Peek bar — always visible, tap anywhere to toggle.
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .clickable { expanded = !expanded }
                .padding(horizontal = 20.dp, vertical = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Drag indicator bar on top — visually suggests a pullable sheet.
            Column(modifier = Modifier.weight(1f)) {
                Box(
                    modifier = Modifier
                        .size(width = 36.dp, height = 4.dp)
                        .clip(RoundedCornerShape(2.dp))
                        .background(MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f)),
                )
                Spacer(modifier = Modifier.height(10.dp))

                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(
                        imageVector = if (rendererIsMuted) {
                            @Suppress("DEPRECATION") Icons.Filled.VolumeOff
                        } else {
                            @Suppress("DEPRECATION") Icons.Filled.VolumeUp
                        },
                        contentDescription = null,
                        tint = if (rendererIsMuted) Secondary else MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.size(18.dp),
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = if (rendererIsMuted) "MUTED" else "${(rendererVolume * 100).toInt()}%",
                        style = MaterialTheme.typography.labelMedium,
                        color = if (rendererIsMuted) Secondary else MaterialTheme.colorScheme.onSurface,
                    )
                    if (zones.isNotEmpty()) {
                        Spacer(modifier = Modifier.width(12.dp))
                        Box(
                            modifier = Modifier
                                .size(4.dp)
                                .clip(CircleShape)
                                .background(MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.4f)),
                        )
                        Spacer(modifier = Modifier.width(12.dp))
                        Text(
                            text = if (assignedZoneCount > 0) {
                                "$assignedZoneCount / ${zones.size} ZONES"
                            } else {
                                "${zones.size} ZONES"
                            },
                            style = MaterialTheme.typography.labelMedium,
                            color = if (assignedZoneCount > 0) Secondary
                                else MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            Icon(
                imageVector = Icons.Filled.ExpandMore,
                contentDescription = if (expanded) "Collapse" else "Expand",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier
                    .size(22.dp)
                    .rotate(180f - chevronRotation),
            )
        }

        AnimatedVisibility(
            visible = expanded,
            enter = expandVertically() + fadeIn(),
            exit = shrinkVertically() + fadeOut(),
        ) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(max = 420.dp)
                    .padding(start = 20.dp, end = 20.dp, bottom = 20.dp),
            ) {
                PanelRendererVolume(
                    rendererName = rendererName,
                    volume = rendererVolume,
                    isMuted = rendererIsMuted,
                    enabled = !blocked,
                    onSetVolume = onSetRendererVolume,
                    onToggleMute = onToggleRendererMute,
                )

                if (zones.isNotEmpty()) {
                    Spacer(modifier = Modifier.height(20.dp))
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(1.dp)
                            .background(MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.12f)),
                    )
                    Spacer(modifier = Modifier.height(16.dp))

                    Text(
                        text = "ZONES",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Spacer(modifier = Modifier.height(12.dp))

                    // Scrollable zone list.
                    LazyColumn(
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(
                            items = zones,
                            key = { it.nodeId },
                        ) { zone ->
                            PanelZoneRow(
                                zone = zone,
                                canToggleAssignment = canToggleZoneAssignment,
                                onSetVolume = { v -> onSetZoneVolume(zone.nodeId, v) },
                                onToggleMute = { onToggleZoneMute(zone.nodeId) },
                                onToggleAssignment = { a ->
                                    onToggleZoneAssignment(zone.nodeId, a)
                                },
                            )
                        }
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PanelRendererVolume(
    rendererName: String,
    volume: Float,
    isMuted: Boolean,
    enabled: Boolean,
    onSetVolume: (Float) -> Unit,
    onToggleMute: () -> Unit,
) {
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = rendererName.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            Text(
                text = if (isMuted) "MUTED" else "${(volume * 100).toInt()}%",
                style = MaterialTheme.typography.labelMedium,
                color = if (isMuted) Secondary else MaterialTheme.colorScheme.onSurface,
            )
        }
        Spacer(modifier = Modifier.height(8.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(
                onClick = onToggleMute,
                enabled = enabled,
                modifier = Modifier.size(36.dp),
            ) {
                Icon(
                    imageVector = if (isMuted) {
                        @Suppress("DEPRECATION") Icons.Filled.VolumeOff
                    } else {
                        @Suppress("DEPRECATION") Icons.Filled.VolumeUp
                    },
                    contentDescription = if (isMuted) "Unmute" else "Mute",
                    tint = if (isMuted) Secondary else MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.size(20.dp),
                )
            }
            Slider(
                value = volume,
                onValueChange = onSetVolume,
                enabled = enabled,
                colors = SliderDefaults.colors(
                    thumbColor = Primary,
                    activeTrackColor = Primary,
                    inactiveTrackColor = SurfaceContainerHighest,
                ),
                modifier = Modifier.weight(1f),
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun PanelZoneRow(
    zone: com.mediautopia.app.ui.screen.nowplaying.PanelZone,
    canToggleAssignment: Boolean,
    onSetVolume: (Float) -> Unit,
    onToggleMute: () -> Unit,
    onToggleAssignment: (Boolean) -> Unit,
) {
    val rowAlpha = if (zone.isOnline) 1f else 0.4f
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(
                if (zone.assignedToCurrent) Secondary.copy(alpha = 0.08f)
                else MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.04f)
            )
            .alpha(rowAlpha)
            .padding(horizontal = 12.dp, vertical = 10.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (canToggleAssignment) {
                androidx.compose.material3.Checkbox(
                    checked = zone.assignedToCurrent,
                    onCheckedChange = { onToggleAssignment(it) },
                    enabled = zone.isOnline,
                    colors = androidx.compose.material3.CheckboxDefaults.colors(
                        checkedColor = Secondary,
                        uncheckedColor = MaterialTheme.colorScheme.onSurfaceVariant,
                    ),
                    modifier = Modifier
                        .size(24.dp)
                        .padding(end = 12.dp),
                )
            }
            Text(
                text = zone.name,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (!zone.isOnline) {
                Text(
                    text = "OFFLINE",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                Text(
                    text = if (zone.isMuted) "MUTED" else "${(zone.volume * 100).toInt()}%",
                    style = MaterialTheme.typography.labelSmall,
                    color = if (zone.isMuted) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Spacer(modifier = Modifier.height(6.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(
                onClick = onToggleMute,
                enabled = zone.isOnline,
                modifier = Modifier.size(32.dp),
            ) {
                Icon(
                    imageVector = if (zone.isMuted) {
                        @Suppress("DEPRECATION") Icons.Filled.VolumeOff
                    } else {
                        @Suppress("DEPRECATION") Icons.Filled.VolumeUp
                    },
                    contentDescription = if (zone.isMuted) "Unmute" else "Mute",
                    tint = if (zone.isMuted) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(18.dp),
                )
            }
            Slider(
                value = zone.volume,
                onValueChange = onSetVolume,
                enabled = zone.isOnline,
                colors = SliderDefaults.colors(
                    thumbColor = Primary,
                    activeTrackColor = Primary,
                    inactiveTrackColor = SurfaceContainerHighest,
                ),
                modifier = Modifier.weight(1f),
            )
        }
    }
}

private fun formatDuration(ms: Long): String {
    val totalSeconds = (ms.coerceAtLeast(0) / 1000).toInt()
    val minutes = totalSeconds / 60
    val seconds = totalSeconds % 60
    return "%d:%02d".format(minutes, seconds)
}
