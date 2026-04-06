package com.mediautopia.app.ui.navigation

import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Snackbar
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavDestination.Companion.hierarchy
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.mediautopia.app.ui.MainViewModel
import com.mediautopia.app.ui.components.TopHeader
import com.mediautopia.app.ui.screen.library.LibraryScreen
import com.mediautopia.app.ui.screen.nowplaying.NowPlayingScreen
import com.mediautopia.app.ui.screen.renderers.RenderersSheet
import com.mediautopia.app.ui.screen.settings.SettingsScreen
import com.mediautopia.app.ui.screen.queue.QueueSheet
import com.mediautopia.app.ui.screen.zones.ZonesScreen
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.SurfaceContainerHigh
import com.mediautopia.app.ui.theme.SurfaceContainerLow

private val bottomNavItems = listOf(
    Screen.NowPlaying,
    Screen.Queue,
    Screen.Library,
    Screen.Zones,
)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AppNavigation(
    mainViewModel: MainViewModel = hiltViewModel(),
) {
    val navController = rememberNavController()
    val navBackStackEntry by navController.currentBackStackEntryAsState()
    val currentDestination = navBackStackEntry?.destination
    val activeRendererName by mainViewModel.activeRendererName.collectAsStateWithLifecycle()
    val snackbarHostState = remember { SnackbarHostState() }
    var showRenderersSheet by remember { mutableStateOf(false) }
    val renderersSheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)

    // Observe snackbar messages.
    LaunchedEffect(Unit) {
        mainViewModel.snackbarManager.messages.collect { message ->
            snackbarHostState.showSnackbar(message)
        }
    }

    // Renderers bottom sheet overlay.
    if (showRenderersSheet) {
        ModalBottomSheet(
            onDismissRequest = { showRenderersSheet = false },
            sheetState = renderersSheetState,
            containerColor = MaterialTheme.colorScheme.surface,
            dragHandle = null,
        ) {
            RenderersSheet(
                onDismiss = { showRenderersSheet = false },
            )
        }
    }

    Scaffold(
        snackbarHost = {
            SnackbarHost(hostState = snackbarHostState) { data ->
                Snackbar(
                    snackbarData = data,
                    containerColor = SurfaceContainerHigh,
                    contentColor = Secondary,
                )
            }
        },
        topBar = {
            TopHeader(
                onSettingsClick = {
                    navController.navigate(Screen.Settings.route) {
                        launchSingleTop = true
                    }
                },
                onRenderersClick = { showRenderersSheet = true },
                activeRendererName = activeRendererName,
            )
        },
        bottomBar = {
            NavigationBar(
                containerColor = MaterialTheme.colorScheme.surfaceContainerLow,
            ) {
                bottomNavItems.forEach { screen ->
                    val selected = currentDestination?.hierarchy?.any { it.route == screen.route } == true
                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            navController.navigate(screen.route) {
                                popUpTo(navController.graph.findStartDestination().id) {
                                    saveState = true
                                }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = {
                            Icon(
                                imageVector = screen.icon,
                                contentDescription = screen.title,
                            )
                        },
                        label = {
                            Text(
                                text = screen.title,
                                style = MaterialTheme.typography.labelSmall,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                            )
                        },
                        colors = NavigationBarItemDefaults.colors(
                            selectedIconColor = MaterialTheme.colorScheme.primary,
                            selectedTextColor = MaterialTheme.colorScheme.primary,
                            unselectedIconColor = MaterialTheme.colorScheme.onSurfaceVariant,
                            unselectedTextColor = MaterialTheme.colorScheme.onSurfaceVariant,
                            indicatorColor = MaterialTheme.colorScheme.surfaceContainerHigh,
                        ),
                    )
                }
            }
        },
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = Screen.NowPlaying.route,
            modifier = Modifier.padding(innerPadding),
        ) {
            composable(Screen.NowPlaying.route) { NowPlayingScreen() }
            composable(Screen.Queue.route) { QueueSheet() }
            composable(Screen.Library.route) { LibraryScreen() }
            composable(Screen.Zones.route) { ZonesScreen() }
            composable(Screen.Settings.route) {
                SettingsScreen(
                    onNavigateBack = { navController.popBackStack() },
                )
            }
        }
    }
}
