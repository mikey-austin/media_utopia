package com.mediautopia.app.ui.navigation

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.LibraryMusic
import androidx.compose.material.icons.outlined.MusicNote
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Speaker
import androidx.compose.material.icons.outlined.SurroundSound
import androidx.compose.ui.graphics.vector.ImageVector

sealed class Screen(val route: String, val title: String, val icon: ImageVector) {
    data object NowPlaying : Screen("now_playing", "NOW PLAYING", Icons.Outlined.MusicNote)
    data object Library : Screen("library", "LIBRARY", Icons.Outlined.LibraryMusic)
    data object Renderers : Screen("renderers", "RENDERERS", Icons.Outlined.Speaker)
    data object Zones : Screen("zones", "ZONES", Icons.Outlined.SurroundSound)
    data object Settings : Screen("settings", "SETTINGS", Icons.Outlined.Settings)
}
