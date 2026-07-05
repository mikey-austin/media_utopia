"""Regression tests for the 2026-07 stability/performance overhaul.

Covers:
- async_get_queue distinguishing fetch failure (None) from empty queue
- presence re-announce merging instead of wiping cached state
- retained state arriving before presence being buffered, not dropped
- cleared retained presence removing the node
- request timeouts marking renderers offline (and state bringing them back)
- ifRevision travelling in the command envelope
- playlist discovery only republishing on change
- artwork proxy host allowlist
"""

import asyncio
import json
import sys
import types


def _install_mocks() -> None:
    ha_mock = types.ModuleType('homeassistant')
    ha_components = types.ModuleType('homeassistant.components')
    ha_mqtt = types.ModuleType('homeassistant.components.mqtt')
    ha_mqtt.async_subscribe = lambda *a, **k: None
    ha_mqtt.async_publish = lambda *a, **k: None
    ha_mqtt.async_wait_for_mqtt_client = lambda *a, **k: None
    ha_config = types.ModuleType('homeassistant.config_entries')
    ha_config.ConfigEntry = type('ConfigEntry', (), {})
    ha_core = types.ModuleType('homeassistant.core')
    ha_core.HomeAssistant = type('HomeAssistant', (), {})
    ha_core.callback = lambda f: f
    ha_network = types.ModuleType('homeassistant.helpers.network')
    ha_network.get_url = lambda *a, **k: 'http://localhost:8123'
    ha_storage = types.ModuleType('homeassistant.helpers.storage')
    ha_storage.Store = type('Store', (), {'__init__': lambda *a, **k: None})
    ha_mqtt_const = types.ModuleType('homeassistant.components.mqtt.const')
    ha_mqtt_const.DATA_MQTT = 'mqtt'
    ha_exceptions = types.ModuleType('homeassistant.exceptions')
    ha_exceptions.ConfigEntryNotReady = type('ConfigEntryNotReady', (Exception,), {})
    for name, mod in [
        ('homeassistant', ha_mock),
        ('homeassistant.components', ha_components),
        ('homeassistant.components.mqtt', ha_mqtt),
        ('homeassistant.components.mqtt.const', ha_mqtt_const),
        ('homeassistant.config_entries', ha_config),
        ('homeassistant.core', ha_core),
        ('homeassistant.helpers', types.ModuleType('homeassistant.helpers')),
        ('homeassistant.helpers.network', ha_network),
        ('homeassistant.helpers.storage', ha_storage),
        ('homeassistant.exceptions', ha_exceptions),
        ('homeassistant.components.image_proxy',
         types.ModuleType('homeassistant.components.image_proxy')),
    ]:
        sys.modules.setdefault(name, mod)

    vol_mock = types.ModuleType('voluptuous')
    for attr in ('Schema', 'Required', 'Optional', 'In', 'Coerce', 'All',
                 'Range', 'Length', 'Strip', 'Exclusive'):
        setattr(vol_mock, attr, lambda *a, **k: None)
    sys.modules.setdefault('voluptuous', vol_mock)


_install_mocks()

from ..bridge import MudBridge  # noqa: E402


class _Msg:
    def __init__(self, topic: str, payload) -> None:
        self.topic = topic
        if isinstance(payload, (dict, list)):
            payload = json.dumps(payload)
        self.payload = payload


def _make_bridge():
    bridge = object.__new__(MudBridge)
    bridge.topic_base = "mu/v1"
    bridge.artwork_base_url = ""
    bridge.identity = "homeassistant"
    bridge.entity_prefix = "mu/ha"
    bridge.discovery_prefix = "homeassistant"
    bridge._renderers = {}
    bridge._renderer_topics = {}
    bridge._renderer_listeners = []
    bridge._renderer_state_listeners = []
    bridge._zones = {}
    bridge._zone_listeners = []
    bridge._zone_state_listeners = []
    bridge._zone_controllers = {}
    bridge._zone_controller_listeners = []
    bridge._libraries = {}
    bridge._playlist_servers = {}
    bridge._selected_playlist_server = None
    bridge._playlists = {}
    bridge._playlist_listeners = []
    bridge._published_playlists = {}
    bridge._playlist_outage_logged = False
    bridge._discovery_topics = set()
    bridge._leases = {}
    bridge._pending = {}
    bridge._pending_states = {}
    bridge._unresponsive_nodes = set()
    bridge._request_semas = {}
    bridge._metadata_cache = {}
    bridge._metadata_failures = {}
    bridge._bg_tasks = set()
    bridge._artwork_hosts = set()
    bridge._image_proxy_url = None
    bridge.hass = types.SimpleNamespace(
        config=types.SimpleNamespace(external_url=None),
        data={},
    )
    return bridge


RENDERER = "mu:renderer:gstreamer:mud@ha:living-room"
PRESENCE_TOPIC = f"mu/v1/node/{RENDERER}/presence"
STATE_TOPIC = f"mu/v1/node/{RENDERER}/state"


# ---------------------------------------------------------------------------
# async_get_queue: failure vs empty
# ---------------------------------------------------------------------------

def test_get_queue_returns_none_when_page_fetch_fails():
    bridge = _make_bridge()
    bridge._renderers[RENDERER] = {
        "state": {"queue": {"length": 250}},
    }

    async def fake_page(node_id, from_index, count):
        if from_index == 0:
            return [{"queueEntryId": str(i)} for i in range(100)]
        return None  # simulate timeout on page 2

    bridge.async_get_queue_page = fake_page
    result = asyncio.run(bridge.async_get_queue(RENDERER))
    assert result is None


def test_get_queue_empty_queue_returns_empty_list():
    bridge = _make_bridge()
    bridge._renderers[RENDERER] = {"state": {"queue": {"length": 0}}}
    result = asyncio.run(bridge.async_get_queue(RENDERER))
    assert result == []


def test_get_queue_advances_by_actual_page_size():
    bridge = _make_bridge()
    bridge._renderers[RENDERER] = {"state": {"queue": {"length": 120}}}
    calls = []

    async def fake_page(node_id, from_index, count):
        calls.append(from_index)
        # Server caps replies at 60 entries even though 100 were asked for.
        remaining = 120 - from_index
        return [{"queueEntryId": str(from_index + i)} for i in range(min(60, remaining))]

    bridge.async_get_queue_page = fake_page
    result = asyncio.run(bridge.async_get_queue(RENDERER))
    assert len(result) == 120
    assert calls == [0, 60]


# ---------------------------------------------------------------------------
# Presence / state lifecycle
# ---------------------------------------------------------------------------

def test_presence_reannounce_preserves_cached_state():
    bridge = _make_bridge()
    state = {"playback": {"status": "paused"}}
    bridge._renderers[RENDERER] = {
        "nodeId": RENDERER,
        "name": "Living Room",
        "online": True,
        "state": state,
    }
    msg = _Msg(PRESENCE_TOPIC, {"nodeId": RENDERER, "kind": "renderer", "name": "Living Room"})
    asyncio.run(bridge._on_presence(msg))
    assert bridge._renderers[RENDERER]["state"] == state
    assert bridge._renderers[RENDERER]["online"] is True


def test_presence_reannounce_drops_cached_lease():
    bridge = _make_bridge()
    bridge._leases[RENDERER] = object()
    msg = _Msg(PRESENCE_TOPIC, {"nodeId": RENDERER, "kind": "renderer", "name": "LR"})
    asyncio.run(bridge._on_presence(msg))
    assert RENDERER not in bridge._leases


def test_state_before_presence_is_buffered_then_applied():
    bridge = _make_bridge()
    state_payload = {"playback": {"status": "paused"}, "queue": {"length": 3}}
    asyncio.run(bridge._on_state(_Msg(STATE_TOPIC, state_payload)))
    assert RENDERER not in bridge._renderers
    assert bridge._pending_states[RENDERER] == state_payload

    msg = _Msg(PRESENCE_TOPIC, {"nodeId": RENDERER, "kind": "renderer", "name": "LR"})
    asyncio.run(bridge._on_presence(msg))
    assert bridge._renderers[RENDERER]["state"] == state_payload
    assert RENDERER not in bridge._pending_states


def test_cleared_retained_presence_removes_node():
    bridge = _make_bridge()
    bridge._renderers[RENDERER] = {"nodeId": RENDERER, "online": True}
    bridge._leases[RENDERER] = object()
    asyncio.run(bridge._on_presence(_Msg(PRESENCE_TOPIC, "")))
    assert RENDERER not in bridge._renderers
    assert RENDERER not in bridge._leases


# ---------------------------------------------------------------------------
# Timeout -> offline -> state -> online
# ---------------------------------------------------------------------------

def test_timeout_marks_renderer_offline_and_state_restores_it():
    bridge = _make_bridge()
    bridge._renderers[RENDERER] = {"nodeId": RENDERER, "online": True}
    notified = []
    bridge._renderer_listeners.append(notified.append)

    bridge._on_request_timeout(RENDERER, "playback.play")
    assert bridge._renderers[RENDERER]["online"] is False
    assert RENDERER in bridge._unresponsive_nodes
    assert notified == [RENDERER]

    bridge._maybe_resolve_metadata = lambda *a, **k: None
    asyncio.run(bridge._on_state(_Msg(STATE_TOPIC, {"playback": {"status": "playing"}})))
    assert bridge._renderers[RENDERER]["online"] is True
    assert RENDERER not in bridge._unresponsive_nodes
    assert notified == [RENDERER, RENDERER]


# ---------------------------------------------------------------------------
# ifRevision in the command envelope
# ---------------------------------------------------------------------------

def test_publish_command_includes_if_revision_in_envelope():
    bridge = _make_bridge()
    bridge.reply_topic = "mu/v1/reply/ha-test"
    published = []

    async def fake_publish(topic, payload, retain):
        published.append((topic, payload))

    bridge._publish = fake_publish
    asyncio.run(
        bridge._publish_command(
            RENDERER, "queue.move", {"fromIndex": 1, "toIndex": 2},
            need_lease=False, if_revision=7,
        )
    )
    assert len(published) == 1
    assert published[0][1]["ifRevision"] == 7
    assert published[0][1]["body"] == {"fromIndex": 1, "toIndex": 2}


def test_publish_command_omits_if_revision_when_absent():
    bridge = _make_bridge()
    bridge.reply_topic = "mu/v1/reply/ha-test"
    published = []

    async def fake_publish(topic, payload, retain):
        published.append(payload)

    bridge._publish = fake_publish
    asyncio.run(
        bridge._publish_command(RENDERER, "queue.clear", {}, need_lease=False)
    )
    assert "ifRevision" not in published[0]


# ---------------------------------------------------------------------------
# Playlist discovery churn
# ---------------------------------------------------------------------------

def test_playlist_discovery_not_republished_when_unchanged():
    bridge = _make_bridge()
    publishes = []

    async def fake_publish(topic, payload, retain):
        publishes.append(topic)

    async def fake_publish_discovery(topic, payload):
        publishes.append(topic)

    bridge._publish = fake_publish
    bridge._publish_discovery = fake_publish_discovery
    bridge._playlist_servers["srv"] = {"nodeId": "srv"}
    bridge._selected_playlist_server = "srv"

    pl = {"playlistId": "pl-1", "name": "Jazz", "revision": 3, "size": 10}
    asyncio.run(bridge._ensure_playlist_discovery("pl-1", pl))
    first_count = len(publishes)
    assert first_count > 0

    asyncio.run(bridge._ensure_playlist_discovery("pl-1", dict(pl)))
    assert len(publishes) == first_count  # unchanged -> no republish

    changed = dict(pl, revision=4)
    asyncio.run(bridge._ensure_playlist_discovery("pl-1", changed))
    assert len(publishes) > first_count  # revision bump -> republished


# ---------------------------------------------------------------------------
# Artwork proxy host allowlist
# ---------------------------------------------------------------------------

def test_proxy_artwork_url_registers_upstream_host():
    bridge = _make_bridge()
    bridge._proxy_artwork_url("http://nas.local:8484/art/cover.jpg")
    assert "nas.local:8484" in bridge._artwork_hosts
