"""Verify the bridge does NOT issue library.* calls during state ingest.

After the structured-ref + display protocol reset, the renderer publishes a
`display` block with every state update so controllers don't have to follow up
with library lookups for ordinary rendering.  This module makes that a hard
contract: any library.* command issued from the state listener fails the test.
"""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from ..bridge import MudBridge


def _run(coro):
    loop = asyncio.new_event_loop()
    try:
        return loop.run_until_complete(coro)
    finally:
        loop.close()


# ---------------------------------------------------------------------------
# A no-IO MudBridge fixture.  We override the network seam (`_request`) so any
# MQTT round-trip is observable, and we stub the bits that would otherwise need
# a real Home Assistant.
# ---------------------------------------------------------------------------


class _DummyHass:
    class _Loop:
        @staticmethod
        def create_future():
            return asyncio.get_running_loop().create_future()

    def __init__(self) -> None:
        self.loop = self._Loop()
        self._scheduled: list[Any] = []

        class _Cfg:
            external_url = None

        self.config = _Cfg()

    def async_create_task(self, coro):  # noqa: D401 — match HA signature
        self._scheduled.append(coro)

        class _Task:
            def cancel(self_inner):
                pass

        return _Task()


def _make_bridge() -> MudBridge:
    bridge = MudBridge.__new__(MudBridge)
    bridge.hass = _DummyHass()
    bridge.identity = "ha-test"
    bridge.topic_base = "mu/v1"
    bridge.artwork_base_url = ""
    bridge._renderers = {}
    bridge._renderer_topics = {}
    bridge._renderer_state_listeners = []
    bridge._zones = {}
    bridge._zone_state_listeners = []
    bridge._metadata_cache = {}
    bridge._metadata_failures = {}
    bridge._metadata_sema = asyncio.Semaphore(2)
    bridge._image_proxy_url = None
    bridge._library_calls: list[tuple[str, str, dict[str, Any]]] = []  # type: ignore[attr-defined]

    async def _forbidden_request(node_id, cmd_type, body, **_kw):
        bridge._library_calls.append((node_id, cmd_type, body))  # type: ignore[attr-defined]
        return None

    bridge._request = _forbidden_request  # type: ignore[assignment]

    async def _no_publish(*_a, **_k):
        return None

    bridge._publish = _no_publish  # type: ignore[assignment]
    bridge._publish_renderer_state = _no_publish  # type: ignore[assignment]
    return bridge


def _state_with_display() -> dict[str, Any]:
    return {
        "ts": 1700000000,
        "session": {"owner": "renderer-1", "id": "sess-1"},
        "playback": {"status": "playing", "positionMs": 1234, "durationMs": 322000, "volume": 1.0, "mute": False},
        "queue": {"revision": 7, "length": 1, "index": 0, "shuffle": False, "repeat": False, "repeatMode": "off"},
        "current": {
            "queueEntryId": "qe-1",
            "ref": {
                "kind": "libraryItem",
                "libraryId": "mu:library:jellyfin:mud@home:default",
                "itemId": "track-123",
            },
            "resolved": {"url": "http://upstream/track.flac", "mime": "audio/flac"},
            "display": {
                "title": "Hide and Seek",
                "artist": "Imogen Heap",
                "album": "Speak for Yourself",
                "artworkUrl": "http://upstream/art/123.jpg",
                "durationMs": 322000,
                "mediaType": "audio",
            },
        },
    }


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_state_ingest_does_not_call_library() -> None:
    """Hot path: state with a display block triggers ZERO library.* MQTT calls."""
    bridge = _make_bridge()
    bridge._renderers["renderer-1"] = {"name": "Living Room"}
    bridge._maybe_resolve_metadata("renderer-1", _state_with_display())
    # No follow-up tasks scheduled — and certainly no calls reached `_request`.
    assert bridge._library_calls == []
    assert bridge.hass._scheduled == []


def test_state_ingest_folds_display_into_metadata() -> None:
    """The display block is exposed through the legacy `metadata` key for back-compat."""
    bridge = _make_bridge()
    state = _state_with_display()
    bridge._maybe_resolve_metadata("renderer-1", state)
    metadata = state["current"]["metadata"]
    assert metadata["title"] == "Hide and Seek"
    assert metadata["artist"] == "Imogen Heap"
    assert metadata["album"] == "Speak for Yourself"
    assert metadata["durationMs"] == 322000
    assert "artworkUrl" in metadata


def test_state_ingest_caches_display_for_later_reuse() -> None:
    """The display block is cached against the structured ref."""
    bridge = _make_bridge()
    bridge._maybe_resolve_metadata("renderer-1", _state_with_display())
    cache_key = "mu:library:jellyfin:mud@home:default|track-123"
    assert cache_key in bridge._metadata_cache
    assert bridge._metadata_cache[cache_key]["title"] == "Hide and Seek"
    # Cached entry survives a second state arriving with no display.
    bridge._library_calls.clear()
    state_no_display = _state_with_display()
    state_no_display["current"]["display"] = None
    bridge._maybe_resolve_metadata("renderer-1", state_no_display)
    assert bridge._library_calls == [], "second state must not trigger MQTT calls"
    assert state_no_display["current"]["metadata"]["title"] == "Hide and Seek"


def test_state_ingest_with_no_ref_skips_cache_lookup() -> None:
    """A state with neither display nor ref must not crash and must not call MQTT."""
    bridge = _make_bridge()
    payload = {
        "current": {
            "queueEntryId": "qe-1",
            "resolved": {"url": "http://upstream/foo.flac"},
        },
    }
    bridge._maybe_resolve_metadata("renderer-1", payload)
    assert bridge._library_calls == []


def test_normalize_display_artists_array() -> None:
    bridge = _make_bridge()
    out = bridge._normalize_display({"title": "T", "artists": ["A", "B"]})
    assert out["artist"] == "A, B"


def test_display_for_entry_helper() -> None:
    bridge = _make_bridge()
    entry = {
        "queueEntryId": "qe-1",
        "ref": {"kind": "libraryItem", "libraryId": "lib", "itemId": "x"},
        "display": {"title": "Hello", "artist": "Adele"},
    }
    assert bridge.display_for_entry(entry)["title"] == "Hello"
    assert bridge.display_for_entry({}) == {}


# ---------------------------------------------------------------------------
# Snapshot save and playlist replace must use entries (not item ID strings).
# ---------------------------------------------------------------------------


def test_normalize_queue_entry_for_save_keeps_ref_resolved_display() -> None:
    out = MudBridge._normalize_queue_entry_for_save({
        "queueEntryId": "qe-1",
        "ref": {"kind": "libraryItem", "libraryId": "lib", "itemId": "x"},
        "resolved": {"url": "http://u/x.flac"},
        "display": {"title": "T"},
    })
    assert out == {
        "ref": {"kind": "libraryItem", "libraryId": "lib", "itemId": "x"},
        "resolved": {"url": "http://u/x.flac"},
        "display": {"title": "T"},
    }


def test_normalize_queue_entry_for_save_drops_empty() -> None:
    assert MudBridge._normalize_queue_entry_for_save({}) is None
    assert MudBridge._normalize_queue_entry_for_save({"queueEntryId": "qe"}) is None


def test_normalize_queue_entry_for_save_resolved_only() -> None:
    out = MudBridge._normalize_queue_entry_for_save({"resolved": {"url": "http://u/x"}})
    assert out == {"resolved": {"url": "http://u/x"}}


# ---------------------------------------------------------------------------
# library.getItems backfill is the ONLY library command path the bridge keeps.
# ---------------------------------------------------------------------------


def test_async_get_item_displays_uses_library_getItems():
    """The slow-path display backfill uses library.getItems with structured refs."""
    bridge = _make_bridge()
    captured: list[tuple[str, str, dict[str, Any]]] = []

    async def _fake_request(node_id, cmd_type, body, **_kw):
        captured.append((node_id, cmd_type, body))
        return {
            "type": "ack",
            "body": {
                "items": [
                    {
                        "ref": {
                            "kind": "libraryItem",
                            "libraryId": "lib-1",
                            "itemId": "x",
                        },
                        "display": {"title": "X"},
                    }
                ]
            },
        }

    bridge._request = _fake_request  # type: ignore[assignment]

    refs = [{"kind": "libraryItem", "libraryId": "lib-1", "itemId": "x"}]
    result = _run(
        bridge.async_get_item_displays(refs)
    )
    assert captured == [("lib-1", "library.getItems", {"refs": refs})]
    assert result["lib-1|x"]["title"] == "X"


def test_resolve_sources_for_ref_uses_library_resolveSources():
    bridge = _make_bridge()
    captured: list[tuple[str, str, dict[str, Any]]] = []

    async def _fake_request(node_id, cmd_type, body, **_kw):
        captured.append((node_id, cmd_type, body))
        return {"type": "ack", "body": {"sources": [{"url": "http://u/x.flac"}]}}

    bridge._request = _fake_request  # type: ignore[assignment]

    ref = {"kind": "libraryItem", "libraryId": "lib-1", "itemId": "x"}
    sources = _run(
        bridge._resolve_sources_for_ref(ref)
    )
    assert captured == [("lib-1", "library.resolveSources", {"ref": ref})]
    assert sources == [{"url": "http://u/x.flac"}]


def test_resolve_sources_for_refs_uses_library_resolveSourcesBatch():
    bridge = _make_bridge()
    captured: list[tuple[str, str, dict[str, Any]]] = []

    async def _fake_request(node_id, cmd_type, body, **_kw):
        captured.append((node_id, cmd_type, body))
        return {
            "type": "ack",
            "body": {
                "items": [
                    {"ref": {"libraryId": "lib-1", "itemId": "a"}, "sources": [{"url": "http://u/a"}]},
                    {"ref": {"libraryId": "lib-1", "itemId": "b"}, "sources": [{"url": "http://u/b"}]},
                ],
            },
        }

    bridge._request = _fake_request  # type: ignore[assignment]

    refs = [
        {"kind": "libraryItem", "libraryId": "lib-1", "itemId": "a"},
        {"kind": "libraryItem", "libraryId": "lib-1", "itemId": "b"},
    ]
    out = _run(
        bridge._resolve_sources_for_refs(refs)
    )
    assert len(captured) == 1
    assert captured[0][1] == "library.resolveSourcesBatch"
    assert captured[0][2]["refs"] == refs
    assert out["lib-1|a"][0]["url"] == "http://u/a"
    assert out["lib-1|b"][0]["url"] == "http://u/b"
