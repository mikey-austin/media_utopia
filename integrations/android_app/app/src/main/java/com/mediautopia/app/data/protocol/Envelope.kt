package com.mediautopia.app.data.protocol

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

@Serializable
data class CommandEnvelope(
    val id: String,
    val type: String,
    val ts: Long,
    val from: String,
    val replyTo: String? = null,
    val lease: Lease? = null,
    val ifRevision: Long? = null,
    val body: JsonElement,
)

@Serializable
data class Lease(
    val sessionId: String,
    val token: String,
)

@Serializable
data class ReplyEnvelope(
    val id: String,
    val type: String,
    val ok: Boolean,
    val ts: Long,
    val body: JsonElement? = null,
    val err: ReplyError? = null,
)

@Serializable
data class ReplyError(
    val code: String,
    val message: String,
    val detail: JsonElement? = null,
)
