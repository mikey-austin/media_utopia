"""Tests for bridge helper functions."""

import sys
import types

# Create minimal mock for homeassistant modules so bridge.py can be parsed
# without requiring the full HA installation
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

# Register all mocks
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
    ('homeassistant.exceptions', types.ModuleType('homeassistant.exceptions')),
]:
    sys.modules.setdefault(name, mod)

# Also mock voluptuous
vol_mock = types.ModuleType('voluptuous')
vol_mock.Schema = lambda *a, **k: None
vol_mock.Required = lambda *a, **k: None
vol_mock.Optional = lambda *a, **k: None
vol_mock.In = lambda *a, **k: None
vol_mock.Coerce = lambda *a, **k: None
sys.modules.setdefault('voluptuous', vol_mock)

# Also mock image_proxy
ip_mock = types.ModuleType('homeassistant.components.image_proxy')
sys.modules.setdefault('homeassistant.components.image_proxy', ip_mock)

from ..bridge import Lease, MudBridge


# ---------------------------------------------------------------------------
# Helpers: create a minimal MudBridge without a real HA instance
# ---------------------------------------------------------------------------

def _make_bridge(artwork_base_url="", topic_base="mu/v1"):
    """Create a MudBridge with mocked hass/entry for pure-function testing."""
    bridge = object.__new__(MudBridge)
    bridge.topic_base = topic_base
    bridge.artwork_base_url = artwork_base_url
    bridge._renderer_topics = {}
    bridge._image_proxy_url = None
    # Stub hass so _proxy_artwork_url doesn't crash
    bridge.hass = type('FakeHass', (), {
        'config': type('Cfg', (), {'external_url': None})(),
    })()
    return bridge


# ---------------------------------------------------------------------------
# Lease dataclass
# ---------------------------------------------------------------------------

def test_lease_dataclass():
    lease = Lease(session_id="sess-1", token="tok-1", expires_at=1700000000)
    assert lease.session_id == "sess-1"
    assert lease.token == "tok-1"
    assert lease.expires_at == 1700000000


def test_lease_equality():
    a = Lease(session_id="a", token="t", expires_at=0)
    b = Lease(session_id="a", token="t", expires_at=0)
    assert a == b


def test_lease_different():
    a = Lease(session_id="a", token="t", expires_at=0)
    b = Lease(session_id="b", token="t", expires_at=0)
    assert a != b


# ---------------------------------------------------------------------------
# _split_lib_ref
# ---------------------------------------------------------------------------

def test_split_lib_ref_valid():
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref(
        "lib:mu:library:upnp:mud@office:default:uuid::base64"
    )
    assert lib_id == "mu:library:upnp:mud@office:default"
    assert item_id == "uuid::base64"


def test_split_lib_ref_no_prefix():
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref("no_lib_prefix")
    assert lib_id is None
    assert item_id is None


def test_split_lib_ref_too_few_parts():
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref("lib:a:b:c:d")
    assert lib_id is None
    assert item_id is None


def test_split_lib_ref_empty_item():
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref("lib:a:b:c:d:e:")
    assert lib_id is None
    assert item_id is None


def test_split_lib_ref_exact_six_parts():
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref("lib:a:b:c:d:e:f")
    assert lib_id == "a:b:c:d:e"
    assert item_id == "f"


def test_split_lib_ref_item_with_colons():
    """Item IDs may themselves contain colons; everything after the 5th colon is the item."""
    bridge = _make_bridge()
    lib_id, item_id = bridge._split_lib_ref("lib:a:b:c:d:e:x:y:z")
    assert lib_id == "a:b:c:d:e"
    assert item_id == "x:y:z"


# ---------------------------------------------------------------------------
# _node_id_from_topic
# ---------------------------------------------------------------------------

def test_node_id_from_topic_standard():
    bridge = _make_bridge()
    assert bridge._node_id_from_topic("mu/v1/node/renderer-1/state") == "renderer-1"


def test_node_id_from_topic_different_base():
    bridge = _make_bridge(topic_base="custom/base")
    # The method checks parts[2] == "node", works regardless of topic_base content
    assert bridge._node_id_from_topic("custom/base/node/mynode/presence") == "mynode"


def test_node_id_from_topic_short():
    bridge = _make_bridge()
    result = bridge._node_id_from_topic("mu/v1/node")
    assert result is None


def test_node_id_from_topic_not_node():
    bridge = _make_bridge()
    result = bridge._node_id_from_topic("mu/v1/other/renderer-1/state")
    assert result is None


def test_node_id_from_topic_fallback_to_renderer_topics():
    bridge = _make_bridge()
    bridge._renderer_topics = {
        "renderer-abc": {"state": "some/custom/topic"}
    }
    assert bridge._node_id_from_topic("some/custom/topic") == "renderer-abc"


# ---------------------------------------------------------------------------
# _normalize_metadata
# ---------------------------------------------------------------------------

def test_normalize_metadata_empty():
    bridge = _make_bridge()
    assert bridge._normalize_metadata({}) == {}


def test_normalize_metadata_none():
    bridge = _make_bridge()
    assert bridge._normalize_metadata(None) is None


def test_normalize_metadata_no_artwork():
    bridge = _make_bridge()
    meta = {"title": "Song", "artist": "Band"}
    result = bridge._normalize_metadata(meta)
    assert result == {"title": "Song", "artist": "Band"}


def test_normalize_metadata_with_artwork_no_base():
    bridge = _make_bridge(artwork_base_url="")
    meta = {"title": "Song", "artworkUrl": "http://original/art.jpg"}
    result = bridge._normalize_metadata(meta)
    assert result["artworkUrl"] == "http://original/art.jpg"


def test_normalize_metadata_with_artwork_base():
    bridge = _make_bridge(artwork_base_url="http://newhost:8080")
    meta = {"title": "Song", "artworkUrl": "http://oldhost/art.jpg"}
    result = bridge._normalize_metadata(meta)
    assert "newhost:8080" in result["artworkUrl"]
    assert "/art.jpg" in result["artworkUrl"]


def test_normalize_metadata_does_not_mutate_original():
    bridge = _make_bridge(artwork_base_url="http://newhost:8080")
    meta = {"title": "Song", "artworkUrl": "http://oldhost/art.jpg"}
    bridge._normalize_metadata(meta)
    assert meta["artworkUrl"] == "http://oldhost/art.jpg"


# ---------------------------------------------------------------------------
# _rewrite_artwork_base
# ---------------------------------------------------------------------------

def test_rewrite_artwork_base_no_base():
    bridge = _make_bridge(artwork_base_url="")
    assert bridge._rewrite_artwork_base("http://host/img.jpg") == "http://host/img.jpg"


def test_rewrite_artwork_base_with_base():
    bridge = _make_bridge(artwork_base_url="https://proxy.local")
    result = bridge._rewrite_artwork_base("http://original.host/path/img.jpg")
    assert result == "https://proxy.local/path/img.jpg"


def test_rewrite_artwork_base_relative_url():
    bridge = _make_bridge(artwork_base_url="http://proxy.local/prefix/")
    result = bridge._rewrite_artwork_base("/img.jpg")
    assert result == "http://proxy.local/img.jpg"


def test_rewrite_artwork_base_preserves_path():
    bridge = _make_bridge(artwork_base_url="http://newhost:9090")
    result = bridge._rewrite_artwork_base("http://oldhost:1234/deep/path/img.png")
    assert "/deep/path/img.png" in result
    assert "newhost:9090" in result


# ---------------------------------------------------------------------------
# rewrite_artwork_url
# ---------------------------------------------------------------------------

def test_rewrite_artwork_url_none():
    bridge = _make_bridge()
    assert bridge.rewrite_artwork_url(None) is None


def test_rewrite_artwork_url_empty():
    bridge = _make_bridge()
    assert bridge.rewrite_artwork_url("") == ""


def test_rewrite_artwork_url_already_proxied():
    bridge = _make_bridge()
    url = "/api/mu/artwork?url=something"
    assert bridge.rewrite_artwork_url(url) == url


def test_rewrite_artwork_url_already_image_proxy():
    bridge = _make_bridge()
    url = "/api/image_proxy/something"
    assert bridge.rewrite_artwork_url(url) == url


def test_rewrite_artwork_url_for_internal():
    bridge = _make_bridge(artwork_base_url="http://newhost:8080")
    result = bridge.rewrite_artwork_url("http://old/img.jpg", for_internal=True)
    assert "newhost:8080" in result
    assert "/api/mu/artwork" not in result


def test_rewrite_artwork_url_for_browser():
    bridge = _make_bridge(artwork_base_url="")
    result = bridge.rewrite_artwork_url("http://upstream/img.jpg", for_internal=False)
    # Without a real HA instance, it should fall back to the proxy path
    assert "/api/mu/artwork" in result
    assert "url=" in result


# ---------------------------------------------------------------------------
# _cache_metadata
# ---------------------------------------------------------------------------

def test_cache_metadata_eviction():
    """Test that _cache_metadata evicts old entries when cache is full."""
    bridge = _make_bridge()
    bridge._metadata_cache = {}
    # Fill cache to just over limit
    for i in range(10001):
        bridge._metadata_cache[f"key-{i}"] = {"title": f"Track {i}"}
    assert len(bridge._metadata_cache) == 10001
    # Cache one more - should trigger eviction
    bridge._cache_metadata("new-key", {"title": "New"})
    assert len(bridge._metadata_cache) <= 10001 - 2000 + 1  # evicted 2000, added 1
    assert "new-key" in bridge._metadata_cache
    # Oldest keys should be evicted
    assert "key-0" not in bridge._metadata_cache


# ---------------------------------------------------------------------------
# _resolve_renderer
# ---------------------------------------------------------------------------

def test_resolve_renderer_by_name():
    """Test _resolve_renderer matches by name case-insensitively."""
    bridge = _make_bridge()
    bridge._renderers = {
        "mu:renderer:gst:default": {"name": "Living Room"},
        "mu:renderer:vlc:default": {"name": "Bedroom"},
    }
    assert bridge._resolve_renderer("mu:renderer:gst:default") == "mu:renderer:gst:default"
    assert bridge._resolve_renderer("Living Room") == "mu:renderer:gst:default"
    assert bridge._resolve_renderer("living room") == "mu:renderer:gst:default"
    assert bridge._resolve_renderer("nonexistent") is None


# ---------------------------------------------------------------------------
# _resolve_library
# ---------------------------------------------------------------------------

def test_resolve_library_by_prefix():
    """Test _resolve_library recognizes mu:library: prefix."""
    bridge = _make_bridge()
    bridge._libraries = {
        "mu:library:jellyfin:default": {"name": "Jellyfin"},
    }
    assert bridge._resolve_library("mu:library:jellyfin:default") == "mu:library:jellyfin:default"
    assert bridge._resolve_library("Jellyfin") == "mu:library:jellyfin:default"
    assert bridge._resolve_library("jellyfin") == "mu:library:jellyfin:default"
    assert bridge._resolve_library("nonexistent") is None
