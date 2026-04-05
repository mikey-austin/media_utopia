package com.mediautopia.app.service

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import com.mediautopia.app.data.cache.SettingsDataStore
import com.mediautopia.app.data.mqtt.ConnectionState
import com.mediautopia.app.data.mqtt.MqttConnectionManager
import com.mediautopia.app.data.mqtt.MqttTopics
import com.mediautopia.app.data.repository.NodeRepository
import com.mediautopia.app.data.repository.RendererStateRepository
import com.mediautopia.app.domain.usecase.CommandCorrelator
import com.mediautopia.app.renderer.LocalRendererService
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import javax.inject.Inject

@AndroidEntryPoint
class MqttForegroundService : Service() {

    @Inject lateinit var mqttConnectionManager: MqttConnectionManager
    @Inject lateinit var commandCorrelator: CommandCorrelator
    @Inject lateinit var nodeRepository: NodeRepository
    @Inject lateinit var settingsDataStore: SettingsDataStore
    @Inject lateinit var rendererStateRepository: RendererStateRepository

    private val tag = "MqttForegroundService"
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var localRenderer: LocalRendererService? = null

    companion object {
        private const val CHANNEL_ID = "mu_service"
        private const val NOTIFICATION_ID = 1

        fun start(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, MqttForegroundService::class.java)
            context.stopService(intent)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        createNotificationChannel()
        startForeground(NOTIFICATION_ID, buildNotification("Connecting..."))

        serviceScope.launch {
            try {
                val brokerUrl = settingsDataStore.brokerUrl.first()
                val clientId = "mu-android-${settingsDataStore.clientId.first()}"
                val identity = settingsDataStore.identity.first()

                mqttConnectionManager.connect(brokerUrl, clientId)

                commandCorrelator.setup(
                    topicBase = MqttTopics.BASE,
                    controllerId = clientId,
                    identity = identity,
                )

                nodeRepository.startDiscovery()

                // Start the local renderer so this phone appears as a
                // renderer on the network.
                val renderer = LocalRendererService(
                    mqtt = mqttConnectionManager,
                    nodeRepository = nodeRepository,
                    rendererStateRepository = rendererStateRepository,
                    context = this@MqttForegroundService,
                )
                renderer.start()
                localRenderer = renderer

                Log.i(tag, "MQTT connected, correlator set up, discovery and local renderer started")
            } catch (e: Exception) {
                Log.e(tag, "Failed to start MQTT session: ${e.message}", e)
            }
        }

        // Observe connection state and update the notification text.
        serviceScope.launch {
            mqttConnectionManager.connectionState.collect { state ->
                val text = when (state) {
                    ConnectionState.CONNECTED -> "Connected"
                    ConnectionState.CONNECTING -> "Connecting..."
                    ConnectionState.RECONNECTING -> "Reconnecting..."
                    ConnectionState.DISCONNECTED -> "Disconnected"
                }
                val manager = getSystemService(NotificationManager::class.java)
                manager.notify(NOTIFICATION_ID, buildNotification(text))
            }
        }

        return START_STICKY
    }

    override fun onDestroy() {
        Log.i(tag, "Service destroying, cleaning up")
        localRenderer?.stop()
        localRenderer = null
        nodeRepository.stopDiscovery()
        commandCorrelator.cleanup()

        serviceScope.launch {
            try {
                mqttConnectionManager.disconnect()
            } catch (e: Exception) {
                Log.e(tag, "Error disconnecting MQTT: ${e.message}", e)
            }
        }

        serviceScope.cancel()
        super.onDestroy()
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Media Utopia",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "Keeps the MQTT connection alive"
            setSound(null, null)
        }
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(channel)
    }

    private fun buildNotification(text: String) =
        NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("Media Utopia")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_media_play)
            .setOngoing(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setSilent(true)
            .build()
}
