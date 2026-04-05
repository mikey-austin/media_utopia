package com.mediautopia.app.domain.model

data class Node(
    val nodeId: String,
    val kind: String,     // "renderer", "library", "playlist", "zone_controller", "zone"
    val name: String,
    val caps: Map<String, Any> = emptyMap(),
    val endpoints: Map<String, Any> = emptyMap(),
    val source: String = "",
    val lastSeen: Long = 0,  // unix timestamp
    val isLocal: Boolean = false,  // true for "This Phone" renderer
)
