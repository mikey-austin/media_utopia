package com.mediautopia.app.ui.components

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.mediautopia.app.R
import com.mediautopia.app.ui.theme.Secondary
import com.mediautopia.app.ui.theme.SurfaceContainerHigh

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
        Image(
            painter = painterResource(id = R.drawable.mu_logo),
            contentDescription = "Media Utopia",
            contentScale = ContentScale.FillHeight,
            modifier = Modifier
                .height(32.dp)
                .padding(start = 8.dp),
        )

        Row(verticalAlignment = Alignment.CenterVertically) {
            // Renderer selector chip.
            Row(
                modifier = Modifier
                    .clip(RoundedCornerShape(8.dp))
                    .background(SurfaceContainerHigh)
                    .clickable(onClick = onRenderersClick)
                    .padding(horizontal = 10.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(
                    imageVector = Icons.Outlined.Cast,
                    contentDescription = "Renderers",
                    tint = Secondary,
                    modifier = Modifier.size(18.dp),
                )
                if (activeRendererName != null) {
                    Spacer(modifier = Modifier.width(6.dp))
                    Text(
                        text = activeRendererName,
                        style = MaterialTheme.typography.labelSmall,
                        color = Secondary,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
            }

            Spacer(modifier = Modifier.width(4.dp))

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
