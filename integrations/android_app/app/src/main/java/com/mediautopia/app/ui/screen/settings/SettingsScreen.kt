package com.mediautopia.app.ui.screen.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.ui.components.GradientButton
import com.mediautopia.app.ui.theme.Secondary

@Composable
fun SettingsSheet(
    onDismiss: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val brokerUrl by viewModel.brokerUrl.collectAsStateWithLifecycle()
    val identity by viewModel.identity.collectAsStateWithLifecycle()
    val connectionState by viewModel.connectionState.collectAsStateWithLifecycle()
    val visualizerEnabled by viewModel.visualizerEnabled.collectAsStateWithLifecycle()

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp),
    ) {
        // ---- Header with close ----
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 16.dp, bottom = 8.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "Settings",
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            IconButton(onClick = onDismiss) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = "Close",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        Spacer(modifier = Modifier.height(16.dp))

        // ---- Broker URL ----
        SettingsLabel("BROKER URL")
        Spacer(modifier = Modifier.height(4.dp))
        SettingsTextField(
            value = brokerUrl,
            onValueChange = viewModel::updateBrokerUrl,
            placeholder = "mqtt://localhost:1883",
        )

        Spacer(modifier = Modifier.height(20.dp))

        // ---- Identity ----
        SettingsLabel("IDENTITY")
        Spacer(modifier = Modifier.height(4.dp))
        SettingsTextField(
            value = identity,
            onValueChange = viewModel::updateIdentity,
            placeholder = "android@device",
        )

        Spacer(modifier = Modifier.height(28.dp))

        // ---- Save button ----
        GradientButton(onClick = viewModel::save) {
            Text("SAVE")
        }

        Spacer(modifier = Modifier.height(24.dp))

        // ---- Visualizer toggle ----
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            Column {
                Text(
                    text = "AUDIO VISUALIZER",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    letterSpacing = 0.8.sp,
                )
                Text(
                    text = "Show frequency bars on Now Playing",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.7f),
                )
            }
            Switch(
                checked = visualizerEnabled,
                onCheckedChange = { viewModel.toggleVisualizer() },
                colors = SwitchDefaults.colors(
                    checkedTrackColor = Secondary,
                    checkedThumbColor = MaterialTheme.colorScheme.onSecondary,
                ),
            )
        }

        Spacer(modifier = Modifier.height(24.dp))

        // ---- Connection status ----
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            ConnectionDot(connectionState)
            Text(
                text = connectionState.label(),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        Spacer(modifier = Modifier.height(12.dp))

        // ---- Reconnect button ----
        GradientButton(onClick = viewModel::reconnect) {
            Text("RECONNECT")
        }

        Spacer(modifier = Modifier.height(32.dp))
    }
}

// ---------- Private helpers ----------

@Composable
private fun SettingsLabel(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        letterSpacing = 0.8.sp,
    )
}

@Composable
private fun SettingsTextField(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String,
) {
    TextField(
        value = value,
        onValueChange = onValueChange,
        placeholder = {
            Text(
                text = placeholder,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.5f),
            )
        },
        singleLine = true,
        textStyle = MaterialTheme.typography.bodyMedium.copy(
            color = MaterialTheme.colorScheme.onSurface,
        ),
        colors = TextFieldDefaults.colors(
            focusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLow,
            unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainerLow,
            focusedIndicatorColor = Color.Transparent,
            unfocusedIndicatorColor = Color.Transparent,
            cursorColor = MaterialTheme.colorScheme.primary,
        ),
        modifier = Modifier
            .fillMaxWidth()
            .border(
                width = 1.dp,
                color = MaterialTheme.colorScheme.secondary.copy(alpha = 0.4f),
                shape = MaterialTheme.shapes.small,
            ),
        shape = MaterialTheme.shapes.small,
    )
}

@Composable
private fun ConnectionDot(state: ConnectionState) {
    val color = when (state) {
        ConnectionState.CONNECTED -> Color(0xFF4CAF50)     // green
        ConnectionState.CONNECTING,
        ConnectionState.RECONNECTING -> Color(0xFFFF9800)  // amber
        ConnectionState.DISCONNECTED -> Color(0xFFF44336)  // red
    }
    Box(
        modifier = Modifier
            .size(10.dp)
            .background(color, CircleShape),
    )
}

private fun ConnectionState.label(): String = when (this) {
    ConnectionState.CONNECTED -> "Connected"
    ConnectionState.CONNECTING -> "Connecting..."
    ConnectionState.RECONNECTING -> "Reconnecting..."
    ConnectionState.DISCONNECTED -> "Disconnected"
}
