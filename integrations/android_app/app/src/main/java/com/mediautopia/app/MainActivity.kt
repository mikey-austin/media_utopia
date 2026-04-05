package com.mediautopia.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.mediautopia.app.service.MqttForegroundService
import com.mediautopia.app.ui.navigation.AppNavigation
import com.mediautopia.app.ui.theme.SonicCuratorTheme
import dagger.hilt.android.AndroidEntryPoint

@AndroidEntryPoint
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        MqttForegroundService.start(this)
        setContent {
            SonicCuratorTheme {
                AppNavigation()
            }
        }
    }
}
