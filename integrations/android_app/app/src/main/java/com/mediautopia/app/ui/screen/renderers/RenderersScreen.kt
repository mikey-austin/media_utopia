package com.mediautopia.app.ui.screen.renderers

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.basicMarquee
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
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
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.outlined.Smartphone
import androidx.compose.material.icons.outlined.Speaker
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.ui.components.HiResBadge
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerLow

private val CardShape = RoundedCornerShape(12.dp)

@Composable
fun RenderersSheet(
    viewModel: RenderersViewModel = hiltViewModel(),
    onDismiss: () -> Unit = {},
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()

    RenderersContent(
        state = uiState,
        onSelectRenderer = { nodeId ->
            viewModel.selectRenderer(nodeId)
            onDismiss()
        },
        onReleaseLease = viewModel::releaseLease,
        onTakeControl = viewModel::takeControl,
        onReconnect = viewModel::reconnect,
        onDismiss = onDismiss,
    )
}

@Composable
private fun RenderersContent(
    state: RenderersUiState,
    onSelectRenderer: (String) -> Unit,
    onReleaseLease: (String) -> Unit,
    onTakeControl: (String) -> Unit,
    onReconnect: () -> Unit,
    onDismiss: () -> Unit,
) {
    val localRenderer = state.renderers.firstOrNull { it.isLocal }
    val networkRenderers = state.renderers.filter { !it.isLocal }

    LazyColumn(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp),
        verticalArrangement = Arrangement.spacedBy(0.dp),
    ) {
        // Header with close button.
        item {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 16.dp, bottom = 8.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column {
                    Text(
                        text = "Renderers",
                        style = MaterialTheme.typography.headlineMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = "SELECT AN OUTPUT DESTINATION",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                IconButton(onClick = onDismiss) {
                    Icon(
                        imageVector = Icons.Filled.Close,
                        contentDescription = "Close",
                        tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Spacer(modifier = Modifier.height(8.dp))
        }

        // Connection status + reconnect button.
        item {
            ConnectionStatusBar(
                state = state.connectionState,
                onReconnect = onReconnect,
            )
            Spacer(modifier = Modifier.height(20.dp))
        }

        // Priority output section.
        item {
            SectionLabel(text = "PRIORITY OUTPUT")
            Spacer(modifier = Modifier.height(12.dp))
        }

        if (localRenderer != null) {
            item {
                LocalRendererCard(
                    item = localRenderer,
                    onClick = { onSelectRenderer(localRenderer.nodeId) },
                    onReleaseLease = { onReleaseLease(localRenderer.nodeId) },
                    onTakeControl = { onTakeControl(localRenderer.nodeId) },
                )
                Spacer(modifier = Modifier.height(28.dp))
            }
        }

        // Network renderers section.
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                SectionLabel(text = "DISCOVERED ON NETWORK")
                if (state.isScanning) {
                    Spacer(modifier = Modifier.width(12.dp))
                    ScanningIndicator()
                }
            }
            Spacer(modifier = Modifier.height(12.dp))
        }

        if (networkRenderers.isEmpty()) {
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
                        text = if (state.isScanning) "Searching for renderers..." else "No renderers found",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }

        items(
            items = networkRenderers,
            key = { it.nodeId },
        ) { renderer ->
            NetworkRendererCard(
                item = renderer,
                onClick = { onSelectRenderer(renderer.nodeId) },
                onReleaseLease = { onReleaseLease(renderer.nodeId) },
            )
            Spacer(modifier = Modifier.height(10.dp))
        }

        item { Spacer(modifier = Modifier.height(32.dp)) }
    }
}

@Composable
private fun SectionLabel(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

@Composable
private fun ScanningIndicator() {
    val infiniteTransition = rememberInfiniteTransition(label = "scanning")
    val alpha by infiniteTransition.animateFloat(
        initialValue = 0.3f,
        targetValue = 1f,
        animationSpec = infiniteRepeatable(
            animation = tween(durationMillis = 800, easing = LinearEasing),
            repeatMode = RepeatMode.Reverse,
        ),
        label = "scanPulse",
    )

    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier
                .size(6.dp)
                .clip(CircleShape)
                .background(Secondary.copy(alpha = alpha)),
        )
        Spacer(modifier = Modifier.width(6.dp))
        Text(
            text = "SCANNING...",
            style = MaterialTheme.typography.labelSmall,
            color = Secondary.copy(alpha = alpha),
        )
    }
}

@Composable
private fun LocalRendererCard(
    item: RendererItem,
    onClick: () -> Unit,
    onReleaseLease: () -> Unit,
    onTakeControl: () -> Unit,
) {
    val accentColor = Secondary
    val backgroundColor by animateColorAsState(
        targetValue = if (item.isActive) SurfaceContainerHigh else SurfaceContainerLow,
        label = "localBg",
    )

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(CardShape)
            .then(
                if (item.isActive) {
                    Modifier.drawBehind {
                        drawRect(accentColor, Offset.Zero, Size(4.dp.toPx(), size.height))
                    }
                } else Modifier
            )
            .background(backgroundColor)
            .clickable(onClick = onClick)
            .padding(start = 16.dp, top = 16.dp, bottom = 16.dp, end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Smartphone,
            contentDescription = "Phone",
            tint = if (item.isActive) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(32.dp),
        )

        Spacer(modifier = Modifier.width(16.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = item.name,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                text = "LOCAL PLAYBACK",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            LeaseIndicator(item)
        }

        if (item.isActive) {
            Spacer(modifier = Modifier.width(4.dp))
            Text(
                text = "ACTIVE",
                style = MaterialTheme.typography.labelSmall,
                color = Secondary,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .background(Secondary.copy(alpha = 0.12f))
                    .padding(horizontal = 8.dp, vertical = 4.dp),
            )
        }

        LocalRendererMenu(
            item = item,
            onRelease = onReleaseLease,
            onTakeControl = onTakeControl,
            onSelect = onClick,
        )
    }
}


@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun NetworkRendererCard(
    item: RendererItem,
    onClick: () -> Unit,
    onReleaseLease: () -> Unit,
) {
    val backgroundColor by animateColorAsState(
        targetValue = if (item.isActive) SurfaceContainerHigh else SurfaceContainerLow,
        label = "networkBg",
    )

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(CardShape)
            .background(backgroundColor)
            .clickable(onClick = onClick)
            .padding(start = 16.dp, top = 16.dp, bottom = 16.dp, end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(
            imageVector = Icons.Outlined.Speaker,
            contentDescription = "Renderer",
            tint = if (item.isActive) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(32.dp),
        )

        Spacer(modifier = Modifier.width(16.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = item.name,
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Spacer(modifier = Modifier.height(2.dp))

            val isAnimating = item.currentTrack != null
            Text(
                text = item.status,
                style = MaterialTheme.typography.bodyMedium,
                color = if (item.isActive) Secondary else MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = if (isAnimating) TextOverflow.Clip else TextOverflow.Ellipsis,
                modifier = if (isAnimating) {
                    Modifier.basicMarquee(
                        iterations = Int.MAX_VALUE,
                        initialDelayMillis = 2000,
                        velocity = 50.dp,
                    )
                } else Modifier,
            )

            LeaseIndicator(item)
        }

        if (item.formatBadge != null) {
            Spacer(modifier = Modifier.width(4.dp))
            HiResBadge(text = item.formatBadge)
        }

        if (item.isActive) {
            Spacer(modifier = Modifier.width(4.dp))
            Text(
                text = "ACTIVE",
                style = MaterialTheme.typography.labelSmall,
                color = Secondary,
                modifier = Modifier
                    .clip(RoundedCornerShape(6.dp))
                    .background(Secondary.copy(alpha = 0.12f))
                    .padding(horizontal = 8.dp, vertical = 4.dp),
            )
        }

        RendererMenu(
            hasLease = item.leaseOwner != null,
            showRelease = true,
            isOwnLease = item.isOwnLease,
            onRelease = onReleaseLease,
            onSelect = onClick,
        )
    }
}

// =============================================================================
// Shared components
// =============================================================================

@Composable
private fun LeaseIndicator(item: RendererItem) {
    val countdown = item.leaseExpiresAtMs?.let { formatLeaseCountdown(it) }
    when {
        item.isOwnLease -> {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = "CONTROLLED",
                    style = MaterialTheme.typography.labelSmall,
                    color = Primary,
                    modifier = Modifier
                        .clip(RoundedCornerShape(4.dp))
                        .background(Primary.copy(alpha = 0.12f))
                        .padding(horizontal = 6.dp, vertical = 2.dp),
                )
                if (countdown != null) {
                    Spacer(modifier = Modifier.width(6.dp))
                    Text(
                        text = countdown,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.outline,
                        maxLines = 1,
                    )
                }
            }
        }
        item.leaseOwner != null -> {
            Text(
                text = "LEASE: ${item.leaseOwner.uppercase()}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.outline,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/**
 * Format "ttl remaining" as a compact mm:ss countdown. Local time is compared
 * to the server's unix-millis expiry — slightly out-of-sync clocks yield a
 * best-effort display, which is fine for visibility.
 */
private fun formatLeaseCountdown(expiresAtMs: Long): String? {
    val remainingMs = expiresAtMs - System.currentTimeMillis()
    if (remainingMs <= 0) return "EXPIRED"
    val totalSec = remainingMs / 1000
    val mins = totalSec / 60
    val secs = totalSec % 60
    return "%dm %02ds".format(mins, secs)
}

@Composable
private fun RendererMenu(
    hasLease: Boolean,
    showRelease: Boolean,
    isOwnLease: Boolean,
    onRelease: () -> Unit,
    onSelect: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { showMenu = true }, modifier = Modifier.size(36.dp)) {
            Icon(
                imageVector = Icons.Filled.MoreVert,
                contentDescription = "Options",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
        }
        DropdownMenu(
            expanded = showMenu,
            onDismissRequest = { showMenu = false },
        ) {
            if (showRelease && hasLease && isOwnLease) {
                DropdownMenuItem(
                    text = { Text("Release lease") },
                    onClick = {
                        showMenu = false
                        onRelease()
                    },
                )
            }
            DropdownMenuItem(
                text = { Text("Select") },
                onClick = {
                    showMenu = false
                    onSelect()
                },
            )
        }
    }
}

@Composable
private fun LocalRendererMenu(
    item: RendererItem,
    onRelease: () -> Unit,
    onTakeControl: () -> Unit,
    onSelect: () -> Unit,
) {
    var showMenu by remember { mutableStateOf(false) }
    var showConfirmTakeControl by remember { mutableStateOf(false) }

    Box {
        IconButton(onClick = { showMenu = true }, modifier = Modifier.size(36.dp)) {
            Icon(
                imageVector = Icons.Filled.MoreVert,
                contentDescription = "Options",
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )
        }
        DropdownMenu(
            expanded = showMenu,
            onDismissRequest = { showMenu = false },
        ) {
            when {
                item.isOwnLease -> {
                    DropdownMenuItem(
                        text = { Text("Release lease") },
                        onClick = {
                            showMenu = false
                            onRelease()
                        },
                    )
                }
                item.leaseOwner != null -> {
                    DropdownMenuItem(
                        text = { Text("Take control") },
                        onClick = {
                            showMenu = false
                            showConfirmTakeControl = true
                        },
                    )
                }
                else -> {
                    // No lease held; nothing lease-related to offer.
                }
            }
            DropdownMenuItem(
                text = { Text("Select") },
                onClick = {
                    showMenu = false
                    onSelect()
                },
            )
        }
    }

    if (showConfirmTakeControl) {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = { showConfirmTakeControl = false },
            title = { Text("Take control?") },
            text = {
                val owner = item.leaseOwner ?: "another device"
                Text("Take control from ${owner.uppercase()}?")
            },
            confirmButton = {
                androidx.compose.material3.TextButton(
                    onClick = {
                        showConfirmTakeControl = false
                        onTakeControl()
                    },
                ) { Text("Take control") }
            },
            dismissButton = {
                androidx.compose.material3.TextButton(
                    onClick = { showConfirmTakeControl = false },
                ) { Text("Cancel") }
            },
        )
    }
}

@Composable
private fun ConnectionStatusBar(
    state: ConnectionState,
    onReconnect: () -> Unit,
) {
    val (dotColor, label) = when (state) {
        ConnectionState.CONNECTED -> Color(0xFF4CAF50) to "CONNECTED"
        ConnectionState.CONNECTING -> Color(0xFFFF9800) to "CONNECTING..."
        ConnectionState.DISCONNECTED -> Color(0xFFF44336) to "DISCONNECTED"
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(10.dp))
            .background(SurfaceContainerLow)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(10.dp)
                .clip(CircleShape)
                .background(dotColor),
        )
        Spacer(modifier = Modifier.width(10.dp))
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = "MQTT BROKER",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .background(SurfaceContainerHigh)
                .clickable(onClick = onReconnect)
                .padding(horizontal = 12.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Filled.Refresh,
                contentDescription = "Reconnect",
                tint = Secondary,
                modifier = Modifier.size(16.dp),
            )
            Spacer(modifier = Modifier.width(6.dp))
            Text(
                text = "RECONNECT",
                style = MaterialTheme.typography.labelSmall,
                color = Secondary,
            )
        }
    }
}
