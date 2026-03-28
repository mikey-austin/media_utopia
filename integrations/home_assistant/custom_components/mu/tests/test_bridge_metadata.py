"""Tests for bridge metadata normalization."""

import sys
import types

# Create minimal mocks for homeassistant modules
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

vol_mock = types.ModuleType('voluptuous')
vol_mock.Schema = lambda *a, **k: None
vol_mock.Required = lambda *a, **k: None
vol_mock.Optional = lambda *a, **k: None
vol_mock.In = lambda *a, **k: None
vol_mock.Coerce = lambda *a, **k: None
sys.modules.setdefault('voluptuous', vol_mock)
sys.modules.setdefault('homeassistant.components.image_proxy', types.ModuleType('homeassistant.components.image_proxy'))


def _make_bridge(artwork_base_url=""):
    """Create a minimal bridge instance for testing."""
    from ..bridge import MudBridge
    bridge = MudBridge.__new__(MudBridge)
    bridge.artwork_base_url = artwork_base_url
    bridge._metadata_cache = {}
    bridge._image_proxy_url = None
    bridge.hass = type('FakeHass', (), {
        'config': type('Cfg', (), {'external_url': None})(),
    })()
    return bridge


def test_normalize_metadata_artists_array():
    """Should convert artists array to artist string."""
    bridge = _make_bridge()
    result = bridge._normalize_metadata({
        "title": "Test",
        "artists": ["Artist A", "Artist B"],
    })
    assert result["artist"] == "Artist A, Artist B"
    assert result["title"] == "Test"


def test_normalize_metadata_artist_preserved():
    """Should not overwrite existing artist field."""
    bridge = _make_bridge()
    result = bridge._normalize_metadata({
        "title": "Test",
        "artist": "Solo Artist",
        "artists": ["Different Artist"],
    })
    assert result["artist"] == "Solo Artist"


def test_normalize_metadata_duration_ms():
    """Should normalize duration_ms to durationMs."""
    bridge = _make_bridge()
    result = bridge._normalize_metadata({"duration_ms": 5000})
    assert result["durationMs"] == 5000
    assert "duration_ms" not in result


def test_normalize_metadata_empty():
    """Should handle empty metadata."""
    bridge = _make_bridge()
    result = bridge._normalize_metadata({})
    assert isinstance(result, dict)


def test_normalize_metadata_does_not_mutate():
    """Should not modify the input dict."""
    bridge = _make_bridge()
    original = {"title": "Test", "artists": ["A"]}
    result = bridge._normalize_metadata(original)
    assert "artist" not in original  # Original unchanged
    assert "artist" in result


def test_normalize_metadata_artwork_rewrite():
    """Should rewrite artwork URL when base URL is set."""
    bridge = _make_bridge(artwork_base_url="http://proxy:8080")
    result = bridge._normalize_metadata({
        "artworkUrl": "http://internal:8096/art/123.jpg",
    })
    assert "http://proxy:8080" in result.get("artworkUrl", "")
