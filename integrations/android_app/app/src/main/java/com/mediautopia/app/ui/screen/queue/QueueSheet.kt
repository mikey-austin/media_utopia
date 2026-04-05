package com.mediautopia.app.ui.screen.queue

import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ClearAll
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.DragIndicator
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.Repeat
import androidx.compose.material.icons.filled.RepeatOne
import androidx.compose.material.icons.filled.Shuffle
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.Text
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import com.mediautopia.app.ui.theme.OnSurface
import com.mediautopia.app.ui.theme.OnSurfaceVariant
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.Surface
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerHighest
import sh.calvin.reorderable.ReorderableItem
import sh.calvin.reorderable.rememberReorderableLazyListState

private val ThumbnailShape = RoundedCornerShape(8.dp)
private val ButtonShape = RoundedCornerShape(8.dp)
private val TrackRowShape = RoundedCornerShape(8.dp)

@Composable
fun QueueSheet(
    viewModel: QueueViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()

    QueueContent(
        state = uiState,
        onMoveTrack = viewModel::moveTrack,
        onRemoveTrack = viewModel::removeTrack,
        onClearQueue = viewModel::clearQueue,
        onToggleShuffle = viewModel::toggleShuffle,
        onToggleRepeat = viewModel::toggleRepeat,
        onJumpToTrack = viewModel::jumpToTrack,
    )
}

@Composable
private fun QueueContent(
    state: QueueUiState,
    onMoveTrack: (Int, Int) -> Unit,
    onRemoveTrack: (Int) -> Unit,
    onClearQueue: () -> Unit,
    onToggleShuffle: () -> Unit,
    onToggleRepeat: () -> Unit,
    onJumpToTrack: (Int) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(top = 16.dp),
    ) {
        // Header.
        QueueHeader(
            totalTracks = state.totalTracks,
            totalDuration = state.totalDuration,
            shuffle = state.shuffle,
            repeatMode = state.repeatMode,
            onToggleShuffle = onToggleShuffle,
            onToggleRepeat = onToggleRepeat,
            onClearQueue = onClearQueue,
        )

        Spacer(modifier = Modifier.height(12.dp))

        // Track list.
        if (state.entries.isEmpty() && !state.isLoading) {
            EmptyQueueMessage()
        } else {
            TrackList(
                entries = state.entries,
                onMoveTrack = onMoveTrack,
                onRemoveTrack = onRemoveTrack,
                onJumpToTrack = onJumpToTrack,
            )
        }
    }
}

@Composable
private fun QueueHeader(
    totalTracks: Int,
    totalDuration: String,
    shuffle: Boolean,
    repeatMode: String,
    onToggleShuffle: () -> Unit,
    onToggleRepeat: () -> Unit,
    onClearQueue: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 20.dp),
    ) {
        Text(
            text = "QUEUE",
            style = MaterialTheme.typography.headlineMedium,
            color = OnSurface,
        )

        Spacer(modifier = Modifier.height(4.dp))

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = "$totalTracks TRACKS",
                style = MaterialTheme.typography.labelSmall,
                color = OnSurfaceVariant,
            )
            if (totalDuration.isNotEmpty()) {
                Text(
                    text = totalDuration,
                    style = MaterialTheme.typography.labelSmall,
                    color = OnSurfaceVariant,
                )
            }
        }

        Spacer(modifier = Modifier.height(12.dp))

        // Action buttons.
        Row(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            ActionChip(
                label = "SHUFFLE",
                icon = { size, tint ->
                    Icon(
                        imageVector = Icons.Filled.Shuffle,
                        contentDescription = null,
                        tint = tint,
                        modifier = Modifier.size(size),
                    )
                },
                isActive = shuffle,
                onClick = onToggleShuffle,
            )

            ActionChip(
                label = "REPEAT",
                icon = { size, tint ->
                    Icon(
                        imageVector = if (repeatMode == "one") Icons.Filled.RepeatOne else Icons.Filled.Repeat,
                        contentDescription = null,
                        tint = tint,
                        modifier = Modifier.size(size),
                    )
                },
                isActive = repeatMode.isNotEmpty(),
                onClick = onToggleRepeat,
            )

            ActionChip(
                label = "CLEAR",
                icon = { size, tint ->
                    Icon(
                        imageVector = Icons.Filled.ClearAll,
                        contentDescription = null,
                        tint = tint,
                        modifier = Modifier.size(size),
                    )
                },
                isActive = false,
                onClick = onClearQueue,
            )
        }
    }
}

@Composable
private fun ActionChip(
    label: String,
    icon: @Composable (size: Dp, tint: Color) -> Unit,
    isActive: Boolean,
    onClick: () -> Unit,
) {
    val textColor = if (isActive) Secondary else OnSurfaceVariant

    Row(
        modifier = Modifier
            .clip(ButtonShape)
            .background(SurfaceContainerHigh)
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        icon(14.dp, textColor)
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = textColor,
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TrackList(
    entries: List<QueueEntryUi>,
    onMoveTrack: (Int, Int) -> Unit,
    onRemoveTrack: (Int) -> Unit,
    onJumpToTrack: (Int) -> Unit,
) {
    val lazyListState = rememberLazyListState()
    val reorderableLazyListState = rememberReorderableLazyListState(lazyListState) { from, to ->
        onMoveTrack(from.index, to.index)
    }

    LazyColumn(
        state = lazyListState,
        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        itemsIndexed(
            items = entries,
            key = { _, entry -> entry.queueEntryId },
        ) { index, entry ->
            ReorderableItem(
                reorderableLazyListState,
                key = entry.queueEntryId,
            ) { isDragging ->
                val elevation by animateDpAsState(
                    targetValue = if (isDragging) 8.dp else 0.dp,
                    label = "dragElevation",
                )

                SwipeToDismissTrackRow(
                    entry = entry,
                    elevation = elevation,
                    onRemove = { onRemoveTrack(index) },
                    onTap = { onJumpToTrack(index) },
                    dragModifier = Modifier.draggableHandle(),
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SwipeToDismissTrackRow(
    entry: QueueEntryUi,
    elevation: Dp,
    onRemove: () -> Unit,
    onTap: () -> Unit,
    dragModifier: Modifier,
) {
    val dismissState = rememberSwipeToDismissBoxState(
        confirmValueChange = { value ->
            if (value == SwipeToDismissBoxValue.EndToStart) {
                onRemove()
                true
            } else {
                false
            }
        },
    )

    SwipeToDismissBox(
        state = dismissState,
        backgroundContent = {
            // Swipe background: delete indicator.
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .clip(TrackRowShape)
                    .background(MaterialTheme.colorScheme.error.copy(alpha = 0.3f)),
                contentAlignment = Alignment.CenterEnd,
            ) {
                Icon(
                    imageVector = Icons.Filled.Delete,
                    contentDescription = "Remove",
                    tint = MaterialTheme.colorScheme.error,
                    modifier = Modifier.padding(end = 20.dp),
                )
            }
        },
        enableDismissFromStartToEnd = false,
        enableDismissFromEndToStart = true,
    ) {
        TrackRow(
            entry = entry,
            elevation = elevation,
            onTap = onTap,
            dragModifier = dragModifier,
        )
    }
}

@Composable
private fun TrackRow(
    entry: QueueEntryUi,
    elevation: Dp = 0.dp,
    onTap: () -> Unit,
    dragModifier: Modifier,
) {
    val bgColor by animateColorAsState(
        targetValue = if (entry.isActive) SurfaceContainerHighest else Surface,
        label = "trackBg",
    )
    val numberColor = if (entry.isActive) Primary else OnSurfaceVariant
    val titleColor = if (entry.isActive) Primary else OnSurface

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .shadow(elevation, TrackRowShape)
            .clip(TrackRowShape)
            .background(bgColor)
            .then(
                if (entry.isActive) {
                    Modifier.drawActiveAccent()
                } else {
                    Modifier
                }
            )
            .clickable(onClick = onTap)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Track number.
        Text(
            text = "${entry.index + 1}",
            style = MaterialTheme.typography.labelMedium,
            color = numberColor,
            modifier = Modifier.width(28.dp),
        )

        // Album art thumbnail.
        if (entry.artworkUrl != null) {
            AsyncImage(
                model = entry.artworkUrl,
                contentDescription = "Artwork",
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .size(48.dp)
                    .clip(ThumbnailShape),
            )
        } else {
            Box(
                modifier = Modifier
                    .size(48.dp)
                    .clip(ThumbnailShape)
                    .background(SurfaceContainerHigh),
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.Filled.MusicNote,
                    contentDescription = null,
                    tint = OnSurfaceVariant.copy(alpha = 0.4f),
                    modifier = Modifier.size(24.dp),
                )
            }
        }

        Spacer(modifier = Modifier.width(12.dp))

        // Title and artist.
        Column(
            modifier = Modifier.weight(1f),
        ) {
            Text(
                text = entry.title,
                style = MaterialTheme.typography.bodyMedium,
                color = titleColor,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (entry.artist.isNotEmpty()) {
                Text(
                    text = entry.artist,
                    style = MaterialTheme.typography.bodySmall,
                    color = OnSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        Spacer(modifier = Modifier.width(8.dp))

        // Drag handle.
        Icon(
            imageVector = Icons.Filled.DragIndicator,
            contentDescription = "Reorder",
            tint = OnSurfaceVariant.copy(alpha = 0.5f),
            modifier = Modifier
                .size(24.dp)
                .then(dragModifier),
        )
    }
}

@Composable
private fun EmptyQueueMessage() {
    Box(
        modifier = Modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Icon(
                imageVector = Icons.Filled.MusicNote,
                contentDescription = null,
                tint = OnSurfaceVariant.copy(alpha = 0.3f),
                modifier = Modifier.size(48.dp),
            )
            Spacer(modifier = Modifier.height(12.dp))
            Text(
                text = "QUEUE EMPTY",
                style = MaterialTheme.typography.labelMedium,
                color = OnSurfaceVariant,
            )
        }
    }
}

/**
 * Draw a 3dp left accent bar in [Primary] for the active track.
 * This is the one permitted accent edge per the design rules.
 */
private fun Modifier.drawActiveAccent(): Modifier = this.then(
    Modifier.drawBehind {
        drawRect(
            color = Primary,
            topLeft = Offset.Zero,
            size = Size(3.dp.toPx(), size.height),
        )
    }
)
