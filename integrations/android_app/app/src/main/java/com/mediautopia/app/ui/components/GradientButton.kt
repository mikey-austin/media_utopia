package com.mediautopia.app.ui.components

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ProvideTextStyle
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.unit.dp
import com.mediautopia.app.ui.theme.OnPrimary
import com.mediautopia.app.ui.theme.Primary
import com.mediautopia.app.ui.theme.PrimaryContainer

@Composable
fun GradientButton(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable RowScope.() -> Unit,
) {
    // 135-degree gradient: top-left to bottom-right
    val gradient = Brush.linearGradient(
        colors = listOf(Primary, PrimaryContainer),
        start = Offset(0f, 0f),
        end = Offset(Float.POSITIVE_INFINITY, Float.POSITIVE_INFINITY),
    )

    Row(
        modifier = modifier
            .clip(MaterialTheme.shapes.medium)
            .background(gradient)
            .clickable(onClick = onClick)
            .padding(horizontal = 24.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        ProvideTextStyle(
            value = MaterialTheme.typography.labelMedium.copy(color = OnPrimary),
        ) {
            content()
        }
    }
}
