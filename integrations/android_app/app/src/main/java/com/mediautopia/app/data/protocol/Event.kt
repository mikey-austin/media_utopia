package com.mediautopia.app.data.protocol

import kotlinx.serialization.Serializable

@Serializable
data class Event(
    val type: String,
    val ts: Long,
)
