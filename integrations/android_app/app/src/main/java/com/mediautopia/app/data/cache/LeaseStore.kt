package com.mediautopia.app.data.cache

import android.content.Context
import android.util.Log
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.first
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.json.Json
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Persisted snapshot of a single cached lease. Mirrors LeaseManager's
 * internal CachedLease but lives here so it can be serialized to disk
 * without a circular dependency.
 */
@Serializable
data class StoredLease(
    val sessionId: String,
    val token: String,
    val expiresAt: Long,
)

private val Context.leaseDataStore by preferencesDataStore(name = "lease_cache")

/**
 * Durable storage for the controller's lease cache. Survives MQTT
 * reconnects and process restarts so we don't lose session ids/tokens
 * and end up competing with our own previous self for the lease.
 */
@Singleton
class LeaseStore @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val tag = "LeaseStore"

    private val json = Json { ignoreUnknownKeys = true }
    private val mapSerializer =
        MapSerializer(String.serializer(), StoredLease.serializer())

    suspend fun load(): Map<String, StoredLease> {
        val prefs = context.leaseDataStore.data.first()
        val payload = prefs[KEY] ?: return emptyMap()
        return try {
            json.decodeFromString(mapSerializer, payload)
        } catch (e: Exception) {
            Log.w(tag, "Failed to decode lease cache, starting fresh: ${e.message}")
            emptyMap()
        }
    }

    suspend fun save(leases: Map<String, StoredLease>) {
        val payload = json.encodeToString(mapSerializer, leases)
        context.leaseDataStore.edit { it[KEY] = payload }
    }

    companion object {
        private val KEY = stringPreferencesKey("leases_v1")
    }
}
