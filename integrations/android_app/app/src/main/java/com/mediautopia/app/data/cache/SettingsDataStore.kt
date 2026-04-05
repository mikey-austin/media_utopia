package com.mediautopia.app.data.cache

import android.content.Context
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import java.util.UUID
import javax.inject.Inject
import javax.inject.Singleton

private val Context.dataStore by preferencesDataStore(name = "mu_settings")

@Singleton
class SettingsDataStore @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private companion object {
        val KEY_BROKER_URL = stringPreferencesKey("broker_url")
        val KEY_CLIENT_ID = stringPreferencesKey("client_id")
        val KEY_IDENTITY = stringPreferencesKey("identity")

        const val DEFAULT_BROKER_URL = "mqtt://mqtt.lan:1883"
    }

    val brokerUrl: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[KEY_BROKER_URL] ?: DEFAULT_BROKER_URL
    }

    /**
     * Auto-generated UUID persisted on first access. If no value exists yet,
     * one is generated and written before emitting.
     */
    val clientId: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[KEY_CLIENT_ID] ?: run {
            val generated = UUID.randomUUID().toString()
            context.dataStore.edit { it[KEY_CLIENT_ID] = generated }
            generated
        }
    }

    val identity: Flow<String> = context.dataStore.data.map { prefs ->
        prefs[KEY_IDENTITY] ?: "android@${android.os.Build.MODEL}"
    }

    suspend fun setBrokerUrl(url: String) {
        context.dataStore.edit { prefs ->
            prefs[KEY_BROKER_URL] = url
        }
    }

    suspend fun setIdentity(identity: String) {
        context.dataStore.edit { prefs ->
            prefs[KEY_IDENTITY] = identity
        }
    }
}
