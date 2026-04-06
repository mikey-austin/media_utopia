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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
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
        onDismiss = onDismiss,
    )
}

@Composable
private fun RenderersContent(
    state: RenderersUiState,
    onSelectRenderer: (String) -> Unit,
    onReleaseLease: (String) -> Unit,
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
            Spacer(modifier = Modifier.height(16.dp))
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

        RendererMenu(
            hasLease = item.leaseOwner != null,
            onRelease = onReleaseLease,
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
    when {
        item.isOwnLease -> {
            Text(
                text = "CONTROLLED",
                style = MaterialTheme.typography.labelSmall,
                color = Primary,
                modifier = Modifier
                    .clip(RoundedCornerShape(4.dp))
                    .background(Primary.copy(alpha = 0.12f))
                    .padding(horizontal = 6.dp, vertical = 2.dp),
            )
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

@Composable
private fun RendererMenu(
    hasLease: Boolean,
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
            DropdownMenuItem(
                text = { Text(if (hasLease) "Release lease" else "Force release") },
                onClick = {
                    showMenu = false
                    onRelease()
                },
            )
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
