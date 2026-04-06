package com.mediautopia.app.ui.screen.zones

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.VolumeOff
import androidx.compose.material.icons.outlined.VolumeUp
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mediautopia.app.data.repository.ZoneSource
import com.mediautopia.app.ui.theme.OnSurfaceVariant
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerHighest
import com.mediautopia.app.ui.theme.SurfaceContainerLow

private val CardShape = RoundedCornerShape(8.dp)

@Composable
fun ZonesScreen(
    viewModel: ZonesViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()

    ZonesContent(
        state = uiState,
        onTabChange = viewModel::switchTab,
        onLocalVolumeChange = viewModel::setLocalVolume,
        onZoneVolumeChange = viewModel::setZoneVolume,
        onToggleMute = viewModel::toggleZoneMute,
        onSelectSource = viewModel::selectSource,
    )
}

@Composable
private fun ZonesContent(
    state: ZonesUiState,
    onTabChange: (ZonesTab) -> Unit,
    onLocalVolumeChange: (Float) -> Unit,
    onZoneVolumeChange: (String, Float) -> Unit,
    onToggleMute: (String) -> Unit,
    onSelectSource: (String, String) -> Unit,
) {
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 20.dp),
        verticalArrangement = Arrangement.spacedBy(0.dp),
    ) {
        // Local volume header.
        item {
            Spacer(modifier = Modifier.height(24.dp))
            LocalVolumeHeader(
                volume = state.localVolume,
                onVolumeChange = onLocalVolumeChange,
            )
            Spacer(modifier = Modifier.height(32.dp))
        }

        // Zones section header with count badge.
        item {
            Row(
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "Zones",
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.width(12.dp))
                Text(
                    text = "${state.activeCount} ACTIVE",
                    style = MaterialTheme.typography.labelSmall,
                    color = Secondary,
                    modifier = Modifier
                        .clip(RoundedCornerShape(6.dp))
                        .background(Secondary.copy(alpha = 0.12f))
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                )
            }
            Spacer(modifier = Modifier.height(12.dp))
        }

        // Tab selector.
        item {
            Row(
                modifier = Modifier.padding(bottom = 12.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                ZonesTab.entries.forEach { tab ->
                    val isActive = tab == state.activeTab
                    Text(
                        text = tab.name,
                        style = MaterialTheme.typography.labelMedium,
                        color = if (isActive) Secondary else OnSurfaceVariant,
                        modifier = Modifier
                            .clip(RoundedCornerShape(6.dp))
                            .background(if (isActive) Secondary.copy(alpha = 0.12f) else SurfaceContainerLow)
                            .clickable { onTabChange(tab) }
                            .padding(horizontal = 14.dp, vertical = 8.dp),
                    )
                }
            }
        }

        when (state.activeTab) {
            ZonesTab.ZONES -> {
                // Zone cards.
                if (state.zones.isEmpty()) {
                    item {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(CardShape)
                                .background(SurfaceContainerLow)
                                .padding(24.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                text = "No zones discovered",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }

                items(
                    items = state.zones,
                    key = { it.nodeId },
                ) { zone ->
                    ZoneCard(
                        zone = zone,
                        availableSources = state.availableSources,
                        onVolumeChange = { volume -> onZoneVolumeChange(zone.nodeId, volume) },
                        onToggleMute = { onToggleMute(zone.nodeId) },
                        onSelectSource = { sourceId -> onSelectSource(zone.nodeId, sourceId) },
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                }
            }

            ZonesTab.SOURCES -> {
                if (state.availableSources.isEmpty()) {
                    item {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(CardShape)
                                .background(SurfaceContainerLow)
                                .padding(24.dp),
                            contentAlignment = Alignment.Center,
                        ) {
                            Text(
                                text = "No sources available",
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }

                items(
                    items = state.availableSources,
                    key = { it.id },
                ) { source ->
                    SourceCard(
                        source = source,
                        zones = state.zones,
                        onToggleZone = { zoneNodeId, assign ->
                            if (assign) onSelectSource(zoneNodeId, source.id)
                        },
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                }
            }
        }

        // Bottom padding.
        item {
            Spacer(modifier = Modifier.height(24.dp))
        }
    }
}

// ---------------------------------------------------------------------------
// Source Card (source-centric view with zone checkboxes)
// ---------------------------------------------------------------------------

@Composable
private fun SourceCard(
    source: ZoneSource,
    zones: List<ZoneUiItem>,
    onToggleZone: (String, Boolean) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(CardShape)
            .background(SurfaceContainerLow)
            .padding(16.dp),
    ) {
        Text(
            text = source.name,
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(modifier = Modifier.height(8.dp))

        zones.forEach { zone ->
            val isAssigned = zone.sourceId == source.id
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { onToggleZone(zone.nodeId, !isAssigned) }
                    .padding(vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Checkbox(
                    checked = isAssigned,
                    onCheckedChange = { onToggleZone(zone.nodeId, it) },
                    colors = CheckboxDefaults.colors(
                        checkedColor = Secondary,
                        uncheckedColor = OnSurfaceVariant,
                    ),
                    modifier = Modifier.size(24.dp),
                )
                Spacer(modifier = Modifier.width(12.dp))
                Text(
                    text = zone.name,
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (isAssigned) MaterialTheme.colorScheme.onSurface
                        else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier
                        .alpha(if (zone.isOnline) 1f else 0.4f),
                )
                if (!zone.isOnline) {
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = "OFFLINE",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

// ---------------------------------------------------------------------------
// Local Volume (replaces Master Volume)
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LocalVolumeHeader(
    volume: Float,
    onVolumeChange: (Float) -> Unit,
) {
    Column {
        Text(
            text = "ACTIVE RENDERER",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.height(4.dp))

        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "Volume",
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )

            Icon(
                imageVector = Icons.Outlined.VolumeUp,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
            Spacer(modifier = Modifier.width(6.dp))
            Text(
                text = "${(volume * 100).toInt()}%",
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }

        Spacer(modifier = Modifier.height(12.dp))

        Slider(
            value = volume,
            onValueChange = onVolumeChange,
            modifier = Modifier.fillMaxWidth(),
            colors = SliderDefaults.colors(
                thumbColor = Primary,
                activeTrackColor = Primary,
                inactiveTrackColor = SurfaceContainerHighest,
            ),
        )
    }
}

// ---------------------------------------------------------------------------
// Zone Card
// ---------------------------------------------------------------------------

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ZoneCard(
    zone: ZoneUiItem,
    availableSources: List<ZoneSource>,
    onVolumeChange: (Float) -> Unit,
    onToggleMute: () -> Unit,
    onSelectSource: (String) -> Unit,
) {
    val isOffline = !zone.isOnline
    val cardAlpha = if (isOffline) 0.4f else 1f

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .alpha(cardAlpha)
            .clip(CardShape)
            .background(SurfaceContainerLow)
            .padding(16.dp),
    ) {
        // Top row: zone name + status indicator.
        Row(
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // Active amber dot with glow.
            if (zone.isOnline) {
                Box(
                    modifier = Modifier
                        .size(8.dp)
                        .drawBehind {
                            drawCircle(
                                color = Primary.copy(alpha = 0.35f),
                                radius = size.minDimension / 2 + 3.dp.toPx(),
                                center = Offset(size.width / 2, size.height / 2),
                            )
                        }
                        .clip(CircleShape)
                        .background(Primary),
                )
                Spacer(modifier = Modifier.width(10.dp))
            }

            Text(
                text = zone.name,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )

            if (isOffline) {
                Text(
                    text = "OFFLINE",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        // Source selector.
        if (availableSources.isNotEmpty() && !isOffline) {
            Spacer(modifier = Modifier.height(8.dp))
            SourceSelector(
                currentSourceId = zone.sourceId,
                currentSourceName = zone.source,
                availableSources = availableSources,
                onSelectSource = onSelectSource,
            )
        } else if (zone.source.isNotEmpty()) {
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = "SOURCE: ${zone.source.uppercase()}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        Spacer(modifier = Modifier.height(12.dp))

        // Volume row: mute button + slider + percentage.
        Row(
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(
                onClick = onToggleMute,
                modifier = Modifier.size(36.dp),
                enabled = !isOffline,
            ) {
                Icon(
                    imageVector = if (zone.isMuted) {
                        Icons.Outlined.VolumeOff
                    } else {
                        Icons.Outlined.VolumeUp
                    },
                    contentDescription = if (zone.isMuted) "Unmute" else "Mute",
                    tint = if (zone.isMuted) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.size(20.dp),
                )
            }

            Spacer(modifier = Modifier.width(4.dp))

            Slider(
                value = zone.volume,
                onValueChange = onVolumeChange,
                modifier = Modifier.weight(1f),
                enabled = !isOffline,
                colors = SliderDefaults.colors(
                    thumbColor = Primary,
                    activeTrackColor = Primary,
                    inactiveTrackColor = SurfaceContainerHighest,
                    disabledThumbColor = Primary.copy(alpha = 0.4f),
                    disabledActiveTrackColor = Primary.copy(alpha = 0.4f),
                    disabledInactiveTrackColor = SurfaceContainerHighest.copy(alpha = 0.4f),
                ),
            )

            Spacer(modifier = Modifier.width(8.dp))

            Text(
                text = "${(zone.volume * 100).toInt()}%",
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.width(36.dp),
            )
        }
    }
}

// ---------------------------------------------------------------------------
// Source Selector
// ---------------------------------------------------------------------------

@Composable
private fun SourceSelector(
    currentSourceId: String,
    currentSourceName: String,
    availableSources: List<ZoneSource>,
    onSelectSource: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }

    val displayName = if (currentSourceId.isNotEmpty()) {
        availableSources.find { it.id == currentSourceId }?.name
            ?: currentSourceName.ifEmpty { "No source" }
    } else {
        currentSourceName.ifEmpty { "No source" }
    }

    Box {
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .background(SurfaceContainerHigh)
                .clickable { expanded = true }
                .padding(horizontal = 10.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "SOURCE:",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.width(6.dp))
            Text(
                text = displayName.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = Secondary,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }

        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            availableSources.forEach { source ->
                val isSelected = source.id == currentSourceId
                DropdownMenuItem(
                    text = {
                        Text(
                            text = source.name,
                            color = if (isSelected) Secondary else MaterialTheme.colorScheme.onSurface,
                        )
                    },
                    onClick = {
                        expanded = false
                        onSelectSource(source.id)
                    },
                )
            }
        }
    }
}
