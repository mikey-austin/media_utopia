package com.mediautopia.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Cast
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.mediautopia.app.ui.theme.Secondary

@Composable
fun TopHeader(
    onSettingsClick: () -> Unit,
    onRenderersClick: () -> Unit = {},
    activeRendererName: String? = null,
) {
    val statusBarPadding = WindowInsets.statusBars.asPaddingValues()
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .padding(top = statusBarPadding.calculateTopPadding())
            .padding(horizontal = 8.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "MEDIA UTOPIA",
            style = MaterialTheme.typography.headlineSmall,
            color = MaterialTheme.colorScheme.primary,
            letterSpacing = 2.sp,
            modifier = Modifier.padding(start = 8.dp),
        )

        Row(verticalAlignment = Alignment.CenterVertically) {
            // Renderer selector (cast icon + name).
            IconButton(onClick = onRenderersClick) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    if (activeRendererName != null) {
                        Text(
                            text = activeRendererName,
                            style = MaterialTheme.typography.labelSmall,
                            color = Secondary,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.width(80.dp),
                        )
                        Spacer(modifier = Modifier.width(2.dp))
                    }
                    Icon(
                        imageVector = Icons.Outlined.Cast,
                        contentDescription = "Renderers",
                        tint = Secondary,
                        modifier = Modifier.size(22.dp),
                    )
                }
            }

            // Settings gear.
            IconButton(onClick = onSettingsClick) {
                Icon(
                    imageVector = Icons.Outlined.Settings,
                    contentDescription = "Settings",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
