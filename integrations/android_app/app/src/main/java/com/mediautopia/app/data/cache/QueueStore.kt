package com.mediautopia.app.data.cache

import android.content.Context
import android.util.Log
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.first
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import javax.inject.Inject
import javax.inject.Singleton

private val Context.queueDataStore by preferencesDataStore(name = "mu_queue")

/**
 * Persisted queue snapshot. Compatible with the MU snapshot protocol format
 * so it can later be used for snapshot.save/restore.
 */
@Serializable
data class QueueSnapshot(
    val entries: List<QueueEntrySnapshot> = emptyList(),
    val index: Long = 0,
    val revision: Long = 0,
    val shuffle: Boolean = false,
    val repeat: Boolean = false,
    val repeatMode: String = "",
    val volume: Double = 1.0,
    val mute: Boolean = false,
    val positionMs: Long = 0,
    val playbackStatus: String = "stopped",
)

@Serializable
data class QueueEntrySnapshot(
    val queueEntryId: String,
    val itemId: String,
    val url: String = "",
    val mime: String = "",
    val metadata: Map<String, JsonElement> = emptyMap(),
)

@Singleton
class QueueStore @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val tag = "QueueStore"
    private val json = Json { ignoreUnknownKeys = true; encodeDefaults = true }

    private companion object {
        val KEY_SNAPSHOT = stringPreferencesKey("queue_snapshot")
    }

    suspend fun save(snapshot: QueueSnapshot) {
        try {
            val encoded = json.encodeToString(QueueSnapshot.serializer(), snapshot)
            context.queueDataStore.edit { prefs ->
                prefs[KEY_SNAPSHOT] = encoded
            }
        } catch (e: Exception) {
            Log.e(tag, "Failed to save queue snapshot: ${e.message}")
        }
    }

    suspend fun load(): QueueSnapshot? {
        return try {
            val prefs = context.queueDataStore.data.first()
            val raw = prefs[KEY_SNAPSHOT] ?: return null
            json.decodeFromString(QueueSnapshot.serializer(), raw)
        } catch (e: Exception) {
            Log.e(tag, "Failed to load queue snapshot: ${e.message}")
            null
        }
    }

    suspend fun clear() {
        context.queueDataStore.edit { prefs ->
            prefs.remove(KEY_SNAPSHOT)
        }
    }
}
