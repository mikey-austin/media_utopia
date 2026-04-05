package com.mediautopia.app.ui.screen.library

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Album
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.MusicNote
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import coil.compose.AsyncImage
import com.mediautopia.app.data.repository.BrowseItem
import com.mediautopia.app.ui.theme.OnSurface
import com.mediautopia.app.ui.theme.OnSurfaceVariant
import com.mediautopia.app.ui.theme.OutlineVariant
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerLow

private val CardShape = RoundedCornerShape(8.dp)
private val SearchBarShape = RoundedCornerShape(4.dp)
private val ThumbnailShape = RoundedCornerShape(4.dp)

@Composable
fun LibraryScreen(
    viewModel: LibraryViewModel = hiltViewModel(),
) {
    val uiState by viewModel.uiState.collectAsStateWithLifecycle()

    // Handle system back: navigate up the breadcrumb stack before leaving the screen.
    BackHandler(enabled = uiState.breadcrumbs.size > 1 || uiState.isSearching) {
        if (uiState.isSearching) {
            viewModel.clearSearch()
        } else {
            viewModel.navigateBack()
        }
    }

    LibraryContent(
        state = uiState,
        onNavigateTo = viewModel::navigateTo,
        onNavigateToBreadcrumb = viewModel::navigateToBreadcrumb,
        onSearch = viewModel::search,
        onClearSearch = viewModel::clearSearch,
        onLoadMore = viewModel::loadMore,
        onPlayItem = viewModel::playItem,
        onAddToQueue = viewModel::addToQueue,
        onPlayContainer = viewModel::playContainer,
    )
}

@Composable
private fun LibraryContent(
    state: LibraryUiState,
    onNavigateTo: (String, String) -> Unit,
    onNavigateToBreadcrumb: (Int) -> Unit,
    onSearch: (String) -> Unit,
    onClearSearch: () -> Unit,
    onLoadMore: () -> Unit,
    onPlayItem: (String) -> Unit,
    onAddToQueue: (String) -> Unit,
    onPlayContainer: (String) -> Unit,
) {
    Column(
        modifier = Modifier.fillMaxSize(),
    ) {
        // Search bar.
        SearchBar(
            query = state.searchQuery,
            onQueryChange = onSearch,
            onClear = onClearSearch,
        )

        // Breadcrumb navigation (hidden during search).
        if (!state.isSearching) {
            BreadcrumbRow(
                breadcrumbs = state.breadcrumbs,
                onBreadcrumbClick = onNavigateToBreadcrumb,
            )
        }

        // Content area.
        when {
            state.isLoading && state.items.isEmpty() -> {
                LoadingIndicator()
            }

            state.error != null && state.items.isEmpty() -> {
                EmptyState(message = state.error!!)
            }

            state.items.isEmpty() -> {
                EmptyState(
                    message = if (state.isSearching) "NO RESULTS" else "EMPTY",
                )
            }

            state.isSearching -> {
                SearchResultsList(
                    items = state.items,
                    hasMore = state.hasMore,
                    onLoadMore = onLoadMore,
                    onItemClick = { item ->
                        if (item.isContainer) {
                            onNavigateTo(item.id, item.title)
                        } else {
                            onPlayItem(item.id)
                        }
                    },
                    onAddToQueue = onAddToQueue,
                )
            }

            else -> {
                val containers = state.items.filter { it.isContainer }
                val tracks = state.items.filter { !it.isContainer }

                if (tracks.isEmpty() && containers.isNotEmpty()) {
                    // Pure container view (albums, artists, folders).
                    ContainerGrid(
                        items = containers,
                        hasMore = state.hasMore,
                        onLoadMore = onLoadMore,
                        onItemClick = { item -> onNavigateTo(item.id, item.title) },
                        onPlayContainer = onPlayContainer,
                    )
                } else if (containers.isEmpty() && tracks.isNotEmpty()) {
                    // Pure track list (inside an album).
                    TrackList(
                        tracks = tracks,
                        hasMore = state.hasMore,
                        onLoadMore = onLoadMore,
                        onPlayItem = onPlayItem,
                        onAddToQueue = onAddToQueue,
                    )
                } else {
                    // Mixed content: containers first, then tracks.
                    MixedContentList(
                        containers = containers,
                        tracks = tracks,
                        hasMore = state.hasMore,
                        onLoadMore = onLoadMore,
                        onContainerClick = { item -> onNavigateTo(item.id, item.title) },
                        onPlayContainer = onPlayContainer,
                        onPlayItem = onPlayItem,
                        onAddToQueue = onAddToQueue,
                    )
                }
            }
        }
    }
}

// =============================================================================
// Search bar
// =============================================================================

@Composable
private fun SearchBar(
    query: String,
    onQueryChange: (String) -> Unit,
    onClear: () -> Unit,
) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp)
            .clip(SearchBarShape)
            .background(SurfaceContainerLow),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Icon(
                imageVector = Icons.Filled.Search,
                contentDescription = "Search",
                tint = OnSurfaceVariant,
                modifier = Modifier.size(20.dp),
            )

            Spacer(modifier = Modifier.width(12.dp))

            Box(modifier = Modifier.weight(1f)) {
                if (query.isEmpty()) {
                    Text(
                        text = "SEARCH LIBRARY",
                        style = MaterialTheme.typography.labelMedium,
                        color = OnSurfaceVariant.copy(alpha = 0.5f),
                    )
                }
                BasicTextField(
                    value = query,
                    onValueChange = onQueryChange,
                    textStyle = MaterialTheme.typography.bodyMedium.copy(
                        color = OnSurface,
                    ),
                    singleLine = true,
                    cursorBrush = SolidColor(Secondary),
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            if (query.isNotEmpty()) {
                Spacer(modifier = Modifier.width(8.dp))
                IconButton(
                    onClick = onClear,
                    modifier = Modifier.size(20.dp),
                ) {
                    Icon(
                        imageVector = Icons.Filled.Clear,
                        contentDescription = "Clear search",
                        tint = OnSurfaceVariant,
                        modifier = Modifier.size(16.dp),
                    )
                }
            }
        }

        // Bottom border.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(1.dp)
                .align(Alignment.BottomCenter)
                .background(OutlineVariant.copy(alpha = 0.3f)),
        )
    }
}

// =============================================================================
// Breadcrumb navigation
// =============================================================================

@Composable
private fun BreadcrumbRow(
    breadcrumbs: List<BreadcrumbItem>,
    onBreadcrumbClick: (Int) -> Unit,
) {
    if (breadcrumbs.size <= 1) return

    val scrollState = rememberScrollState()

    // Auto-scroll to end when breadcrumbs change.
    LaunchedEffect(breadcrumbs.size) {
        scrollState.animateScrollTo(scrollState.maxValue)
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(scrollState)
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        breadcrumbs.forEachIndexed { index, crumb ->
            if (index > 0) {
                Text(
                    text = ">",
                    style = MaterialTheme.typography.labelSmall,
                    color = OnSurfaceVariant.copy(alpha = 0.4f),
                    modifier = Modifier.padding(horizontal = 6.dp),
                )
            }

            val isLast = index == breadcrumbs.lastIndex
            Text(
                text = crumb.label.uppercase(),
                style = MaterialTheme.typography.labelSmall,
                color = if (isLast) Primary else OnSurfaceVariant,
                modifier = Modifier.clickable { onBreadcrumbClick(index) },
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

// =============================================================================
// Container grid (albums, artists, folders)
// =============================================================================

@Composable
private fun ContainerGrid(
    items: List<BrowseItem>,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
    onItemClick: (BrowseItem) -> Unit,
    onPlayContainer: (String) -> Unit,
) {
    val gridState = rememberLazyGridState()

    // Trigger load more near the end.
    val shouldLoadMore by remember {
        derivedStateOf {
            val layoutInfo = gridState.layoutInfo
            val lastVisibleIndex = layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            hasMore && lastVisibleIndex >= layoutInfo.totalItemsCount - 6
        }
    }

    LaunchedEffect(shouldLoadMore) {
        if (shouldLoadMore) onLoadMore()
    }

    LazyVerticalGrid(
        columns = GridCells.Fixed(2),
        state = gridState,
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(
            items = items,
            key = { it.id },
        ) { item ->
            ContainerCard(
                item = item,
                onClick = { onItemClick(item) },
                onPlay = { onPlayContainer(item.id) },
            )
        }

        if (hasMore) {
            item(span = { GridItemSpan(2) }) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(
                        color = Primary,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun ContainerCard(
    item: BrowseItem,
    onClick: () -> Unit,
    onPlay: () -> Unit,
) {
    Column(
        modifier = Modifier
            .clip(CardShape)
            .background(SurfaceContainerLow)
            .clickable(onClick = onClick),
    ) {
        // Artwork.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .aspectRatio(1f)
                .clip(RoundedCornerShape(topStart = 8.dp, topEnd = 8.dp)),
        ) {
            if (item.artworkUrl != null) {
                AsyncImage(
                    model = item.artworkUrl,
                    contentDescription = item.title,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            } else {
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .background(SurfaceContainerHigh),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = containerIcon(item.type),
                        contentDescription = null,
                        tint = OnSurfaceVariant.copy(alpha = 0.3f),
                        modifier = Modifier.size(48.dp),
                    )
                }
            }

            // Play overlay.
            IconButton(
                onClick = onPlay,
                modifier = Modifier
                    .align(Alignment.BottomEnd)
                    .padding(6.dp)
                    .size(32.dp)
                    .clip(RoundedCornerShape(8.dp))
                    .background(Secondary.copy(alpha = 0.9f)),
            ) {
                Icon(
                    imageVector = Icons.Filled.PlayArrow,
                    contentDescription = "Play",
                    tint = MaterialTheme.colorScheme.surface,
                    modifier = Modifier.size(18.dp),
                )
            }
        }

        // Metadata.
        Column(
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp),
        ) {
            Text(
                text = item.title,
                style = MaterialTheme.typography.bodyMedium,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )

            if (item.subtitle.isNotEmpty()) {
                Spacer(modifier = Modifier.height(2.dp))
                Text(
                    text = item.subtitle.uppercase(),
                    style = MaterialTheme.typography.labelSmall,
                    color = OnSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

// =============================================================================
// Track list
// =============================================================================

@Composable
private fun TrackList(
    tracks: List<BrowseItem>,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
    onPlayItem: (String) -> Unit,
    onAddToQueue: (String) -> Unit,
) {
    val listState = rememberLazyListState()

    val shouldLoadMore by remember {
        derivedStateOf {
            val layoutInfo = listState.layoutInfo
            val lastVisibleIndex = layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            hasMore && lastVisibleIndex >= layoutInfo.totalItemsCount - 6
        }
    }

    LaunchedEffect(shouldLoadMore) {
        if (shouldLoadMore) onLoadMore()
    }

    LazyColumn(
        state = listState,
        contentPadding = PaddingValues(vertical = 4.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(
            items = tracks,
            key = { it.id },
        ) { track ->
            TrackRow(
                item = track,
                trackNumber = tracks.indexOf(track) + 1,
                onPlay = { onPlayItem(track.id) },
                onAddToQueue = { onAddToQueue(track.id) },
            )
        }

        if (hasMore) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(
                        color = Primary,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun TrackRow(
    item: BrowseItem,
    trackNumber: Int,
    onPlay: () -> Unit,
    onAddToQueue: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onPlay)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Track number.
        Text(
            text = trackNumber.toString(),
            style = MaterialTheme.typography.labelMedium,
            color = OnSurfaceVariant.copy(alpha = 0.5f),
            modifier = Modifier.width(28.dp),
            textAlign = TextAlign.End,
        )

        Spacer(modifier = Modifier.width(12.dp))

        // Thumbnail.
        if (item.artworkUrl != null) {
            AsyncImage(
                model = item.artworkUrl,
                contentDescription = null,
                contentScale = ContentScale.Crop,
                modifier = Modifier
                    .size(40.dp)
                    .clip(ThumbnailShape),
            )
            Spacer(modifier = Modifier.width(12.dp))
        }

        // Title and artist.
        Column(
            modifier = Modifier.weight(1f),
        ) {
            Text(
                text = item.title,
                style = MaterialTheme.typography.bodyMedium,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )

            if (item.subtitle.isNotEmpty()) {
                Text(
                    text = item.subtitle.uppercase(),
                    style = MaterialTheme.typography.labelSmall,
                    color = OnSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        // Duration.
        val durationMs = item.metadata["durationMs"] as? Long ?: 0L
        if (durationMs > 0) {
            Text(
                text = formatDuration(durationMs),
                style = MaterialTheme.typography.labelSmall,
                color = OnSurfaceVariant,
                modifier = Modifier.padding(horizontal = 8.dp),
            )
        }

        // Add to queue.
        IconButton(
            onClick = onAddToQueue,
            modifier = Modifier.size(32.dp),
        ) {
            Icon(
                imageVector = Icons.Filled.Add,
                contentDescription = "Add to queue",
                tint = OnSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        }
    }
}

// =============================================================================
// Mixed content (containers + tracks)
// =============================================================================

@Composable
private fun MixedContentList(
    containers: List<BrowseItem>,
    tracks: List<BrowseItem>,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
    onContainerClick: (BrowseItem) -> Unit,
    onPlayContainer: (String) -> Unit,
    onPlayItem: (String) -> Unit,
    onAddToQueue: (String) -> Unit,
) {
    val listState = rememberLazyListState()

    val shouldLoadMore by remember {
        derivedStateOf {
            val layoutInfo = listState.layoutInfo
            val lastVisibleIndex = layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            hasMore && lastVisibleIndex >= layoutInfo.totalItemsCount - 6
        }
    }

    LaunchedEffect(shouldLoadMore) {
        if (shouldLoadMore) onLoadMore()
    }

    LazyColumn(
        state = listState,
        contentPadding = PaddingValues(vertical = 4.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        // Container rows as compact cards.
        items(
            items = containers,
            key = { "c:${it.id}" },
        ) { container ->
            ContainerRow(
                item = container,
                onClick = { onContainerClick(container) },
                onPlay = { onPlayContainer(container.id) },
            )
        }

        if (containers.isNotEmpty() && tracks.isNotEmpty()) {
            item {
                Spacer(modifier = Modifier.height(8.dp))
            }
        }

        // Track rows.
        items(
            items = tracks,
            key = { "t:${it.id}" },
        ) { track ->
            TrackRow(
                item = track,
                trackNumber = tracks.indexOf(track) + 1,
                onPlay = { onPlayItem(track.id) },
                onAddToQueue = { onAddToQueue(track.id) },
            )
        }

        if (hasMore) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(
                        color = Primary,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun ContainerRow(
    item: BrowseItem,
    onClick: () -> Unit,
    onPlay: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Thumbnail.
        Box(
            modifier = Modifier
                .size(48.dp)
                .clip(ThumbnailShape),
        ) {
            if (item.artworkUrl != null) {
                AsyncImage(
                    model = item.artworkUrl,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            } else {
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .background(SurfaceContainerHigh),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = containerIcon(item.type),
                        contentDescription = null,
                        tint = OnSurfaceVariant.copy(alpha = 0.3f),
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
        }

        Spacer(modifier = Modifier.width(12.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = item.title,
                style = MaterialTheme.typography.bodyMedium,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (item.subtitle.isNotEmpty()) {
                Text(
                    text = item.subtitle.uppercase(),
                    style = MaterialTheme.typography.labelSmall,
                    color = OnSurfaceVariant,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }

        IconButton(
            onClick = onPlay,
            modifier = Modifier.size(32.dp),
        ) {
            Icon(
                imageVector = Icons.Filled.PlayArrow,
                contentDescription = "Play all",
                tint = Secondary,
                modifier = Modifier.size(20.dp),
            )
        }
    }
}

// =============================================================================
// Search results (flat list)
// =============================================================================

@Composable
private fun SearchResultsList(
    items: List<BrowseItem>,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
    onItemClick: (BrowseItem) -> Unit,
    onAddToQueue: (String) -> Unit,
) {
    val listState = rememberLazyListState()

    val shouldLoadMore by remember {
        derivedStateOf {
            val layoutInfo = listState.layoutInfo
            val lastVisibleIndex = layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            hasMore && lastVisibleIndex >= layoutInfo.totalItemsCount - 6
        }
    }

    LaunchedEffect(shouldLoadMore) {
        if (shouldLoadMore) onLoadMore()
    }

    LazyColumn(
        state = listState,
        contentPadding = PaddingValues(vertical = 4.dp),
        modifier = Modifier.fillMaxSize(),
    ) {
        items(
            items = items,
            key = { it.id },
        ) { item ->
            SearchResultRow(
                item = item,
                onClick = { onItemClick(item) },
                onAddToQueue = { onAddToQueue(item.id) },
            )
        }

        if (hasMore) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(
                        color = Primary,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(24.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun SearchResultRow(
    item: BrowseItem,
    onClick: () -> Unit,
    onAddToQueue: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // Thumbnail.
        Box(
            modifier = Modifier
                .size(44.dp)
                .clip(ThumbnailShape),
        ) {
            if (item.artworkUrl != null) {
                AsyncImage(
                    model = item.artworkUrl,
                    contentDescription = null,
                    contentScale = ContentScale.Crop,
                    modifier = Modifier.fillMaxSize(),
                )
            } else {
                Box(
                    modifier = Modifier
                        .fillMaxSize()
                        .background(SurfaceContainerHigh),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = if (item.isContainer) containerIcon(item.type) else Icons.Filled.MusicNote,
                        contentDescription = null,
                        tint = OnSurfaceVariant.copy(alpha = 0.3f),
                        modifier = Modifier.size(22.dp),
                    )
                }
            }
        }

        Spacer(modifier = Modifier.width(12.dp))

        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = item.title,
                style = MaterialTheme.typography.bodyMedium,
                color = OnSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )

            Row(verticalAlignment = Alignment.CenterVertically) {
                // Type badge.
                Text(
                    text = item.type.uppercase(),
                    style = MaterialTheme.typography.labelSmall,
                    color = Secondary,
                )

                if (item.subtitle.isNotEmpty()) {
                    Text(
                        text = "  ",
                        style = MaterialTheme.typography.labelSmall,
                        color = OnSurfaceVariant,
                    )
                    Text(
                        text = item.subtitle.uppercase(),
                        style = MaterialTheme.typography.labelSmall,
                        color = OnSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f, fill = false),
                    )
                }
            }
        }

        if (!item.isContainer) {
            IconButton(
                onClick = onAddToQueue,
                modifier = Modifier.size(32.dp),
            ) {
                Icon(
                    imageVector = Icons.Filled.Add,
                    contentDescription = "Add to queue",
                    tint = OnSurfaceVariant,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

// =============================================================================
// Loading & empty states
// =============================================================================

@Composable
private fun LoadingIndicator() {
    Box(
        modifier = Modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(
            color = Primary,
            strokeWidth = 2.dp,
            modifier = Modifier.size(32.dp),
        )
    }
}

@Composable
private fun EmptyState(message: String) {
    Box(
        modifier = Modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = message,
            style = MaterialTheme.typography.labelMedium,
            color = OnSurfaceVariant,
        )
    }
}

// =============================================================================
// Helpers
// =============================================================================

private fun containerIcon(type: String) = when {
    type.lowercase().contains("album") -> Icons.Filled.Album
    type.lowercase().contains("artist") -> Icons.Filled.MusicNote
    else -> Icons.Filled.Folder
}

private fun formatDuration(ms: Long): String {
    val totalSeconds = (ms.coerceAtLeast(0) / 1000).toInt()
    val minutes = totalSeconds / 60
    val seconds = totalSeconds % 60
    return "%d:%02d".format(minutes, seconds)
}
