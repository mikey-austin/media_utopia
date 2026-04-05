package com.mediautopia.app.data.repository

import android.content.Context
import android.os.Build
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import androidx.datastore.preferences.preferencesDataStore
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.map
import javax.inject.Inject
import javax.inject.Singleton

private val Context.rendererDataStore: DataStore<Preferences> by preferencesDataStore(
    name = "active_renderer"
)

@Singleton
class ActiveRendererRepository @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    private val activeRendererKey = stringPreferencesKey("active_renderer_id")

    /**
     * The currently selected renderer ID, persisted across app sessions.
     * Defaults to [LOCAL_RENDERER_ID] when no renderer has been explicitly selected.
     */
    val activeRendererId: Flow<String> = context.rendererDataStore.data.map { prefs ->
        prefs[activeRendererKey] ?: LOCAL_RENDERER_ID
    }

    /**
     * Set the active renderer. Persists immediately to DataStore.
     */
    suspend fun setActiveRenderer(nodeId: String) {
        context.rendererDataStore.edit { prefs ->
            prefs[activeRendererKey] = nodeId
        }
    }

    companion object {
        val LOCAL_RENDERER_ID: String = "mu:renderer:media3:android:${
            Build.MODEL.replace(" ", "-")
        }:default"
    }
}
