"""Shared pytest configuration: stub homeassistant modules before tests collect."""

import sys
import types


def _install_ha_mocks() -> None:
    if "homeassistant" in sys.modules and "homeassistant.components.mqtt" in sys.modules:
        return

    ha_mock = types.ModuleType("homeassistant")
    ha_components = types.ModuleType("homeassistant.components")
    ha_mqtt = types.ModuleType("homeassistant.components.mqtt")
    ha_mqtt.async_subscribe = lambda *a, **k: None
    ha_mqtt.async_publish = lambda *a, **k: None
    ha_mqtt.async_wait_for_mqtt_client = lambda *a, **k: None
    ha_mqtt.is_configured = lambda *a, **k: True
    ha_config_entries = types.ModuleType("homeassistant.config_entries")
    ha_config_entries.ConfigEntry = type("ConfigEntry", (), {})
    ha_core = types.ModuleType("homeassistant.core")
    ha_core.HomeAssistant = type("HomeAssistant", (), {})
    ha_core.callback = lambda f: f
    ha_helpers = types.ModuleType("homeassistant.helpers")
    ha_network = types.ModuleType("homeassistant.helpers.network")
    ha_network.get_url = lambda *a, **k: "http://localhost:8123"
    ha_storage = types.ModuleType("homeassistant.helpers.storage")
    ha_storage.Store = type("Store", (), {"__init__": lambda *a, **k: None})
    ha_helpers_entity_platform = types.ModuleType("homeassistant.helpers.entity_platform")
    ha_helpers_entity_platform.AddEntitiesCallback = type("AddEntitiesCallback", (), {})
    ha_helpers_dispatcher = types.ModuleType("homeassistant.helpers.dispatcher")
    ha_helpers_dispatcher.async_dispatcher_send = lambda *a, **k: None
    ha_helpers_dispatcher.async_dispatcher_connect = lambda *a, **k: (lambda: None)
    ha_mqtt_const = types.ModuleType("homeassistant.components.mqtt.const")
    ha_mqtt_const.DATA_MQTT = "mqtt"
    ha_exceptions = types.ModuleType("homeassistant.exceptions")
    ha_exceptions.ConfigEntryNotReady = type("ConfigEntryNotReady", (Exception,), {})

    ha_components_http = types.ModuleType("homeassistant.components.http")
    ha_components_http.StaticPathConfig = type(
        "StaticPathConfig", (), {"__init__": lambda self, **k: None}
    )
    ha_components_http.HomeAssistantView = type("HomeAssistantView", (), {})
    ha_components_frontend = types.ModuleType("homeassistant.components.frontend")
    ha_components_frontend.async_register_built_in_panel = lambda *a, **k: None

    ha_components_image_proxy = types.ModuleType("homeassistant.components.image_proxy")
    ha_components_image_proxy.async_image_proxy_url = None

    ha_components_mp = types.ModuleType("homeassistant.components.media_player")
    ha_components_mp.MediaPlayerEntity = type("MediaPlayerEntity", (), {})
    ha_components_mp.BrowseMedia = type(
        "BrowseMedia", (), {"__init__": lambda self, **k: None}
    )
    ha_components_mp.MediaPlayerDeviceClass = None

    ha_components_mp_const = types.ModuleType(
        "homeassistant.components.media_player.const"
    )

    class _Feature(int):
        def __or__(self, other):
            return _Feature(int(self) | int(other))

    class _MPEF:
        PLAY = _Feature(1)
        PAUSE = _Feature(2)
        STOP = _Feature(4)
        NEXT_TRACK = _Feature(8)
        PREVIOUS_TRACK = _Feature(16)
        SEEK = _Feature(32)
        VOLUME_SET = _Feature(64)
        VOLUME_MUTE = _Feature(128)
        SHUFFLE_SET = _Feature(256)
        REPEAT_SET = _Feature(512)
        PLAY_MEDIA = _Feature(1024)
        BROWSE_MEDIA = _Feature(2048)
        TURN_ON = _Feature(4096)
        TURN_OFF = _Feature(8192)

    class _MPS:
        PLAYING = "playing"
        PAUSED = "paused"
        IDLE = "idle"

    class _MPRM:
        ALL = "all"
        OFF = "off"
        ONE = "one"

    ha_components_mp_const.MediaPlayerEntityFeature = _MPEF
    ha_components_mp_const.MediaPlayerState = _MPS
    ha_components_mp_const.MediaPlayerRepeatMode = _MPRM
    ha_components_mp_const.MEDIA_CLASS_DIRECTORY = "directory"
    ha_components_mp_const.MEDIA_CLASS_MUSIC = "music"
    ha_components_mp_const.MEDIA_CLASS_PLAYLIST = "playlist"
    ha_components_mp_const.MEDIA_TYPE_DIRECTORY = "directory"
    ha_components_mp_const.MEDIA_TYPE_MUSIC = "music"
    ha_components_mp_const.MEDIA_TYPE_PLAYLIST = "playlist"

    ha_components_mp.MediaPlayerEntityFeature = _MPEF
    ha_components_mp.MediaPlayerState = _MPS

    ha_components_websocket = types.ModuleType("homeassistant.components.websocket_api")
    ha_components_websocket.async_register_command = lambda *a, **k: None
    ha_components_websocket.async_response = lambda f: f
    ha_components_websocket.websocket_command = lambda schema: (lambda f: f)
    ha_components_websocket.event_message = lambda mid, data: data
    ha_components_websocket.ActiveConnection = type("ActiveConnection", (), {})

    ha_util = types.ModuleType("homeassistant.util")
    ha_util_dt = types.ModuleType("homeassistant.util.dt")

    import datetime as _dt

    ha_util_dt.utcnow = lambda: _dt.datetime.now(_dt.timezone.utc)
    ha_util.dt = ha_util_dt

    ha_helpers_event = types.ModuleType("homeassistant.helpers.event")
    ha_helpers_event.async_track_time_interval = lambda *a, **k: (lambda: None)

    ha_data_entry_flow = types.ModuleType("homeassistant.data_entry_flow")
    ha_data_entry_flow.FlowResult = dict

    ha_const = types.ModuleType("homeassistant.const")
    ha_const.STATE_UNKNOWN = "unknown"

    ha_helpers_selector = types.ModuleType("homeassistant.helpers.selector")
    ha_helpers_selector.selector = lambda x: x
    ha_helpers_selector.SelectSelector = type("SelectSelector", (), {})
    ha_helpers_selector.SelectSelectorConfig = type("SelectSelectorConfig", (), {})

    ha_components_button = types.ModuleType("homeassistant.components.button")
    ha_components_button.ButtonEntity = type("ButtonEntity", (), {})
    ha_components_select = types.ModuleType("homeassistant.components.select")
    ha_components_select.SelectEntity = type("SelectEntity", (), {})
    ha_components_sensor = types.ModuleType("homeassistant.components.sensor")
    ha_components_sensor.SensorEntity = type("SensorEntity", (), {})
    ha_components_text = types.ModuleType("homeassistant.components.text")
    ha_components_text.TextEntity = type("TextEntity", (), {})

    ha_helpers_entity = types.ModuleType("homeassistant.helpers.entity")
    ha_helpers_entity.Entity = type("Entity", (), {})

    ha_helpers_aiohttp_client = types.ModuleType("homeassistant.helpers.aiohttp_client")
    ha_helpers_aiohttp_client.async_get_clientsession = lambda *a, **k: None

    # Mark parent modules as packages so children can be imported as submodules.
    for parent in (ha_mock, ha_components, ha_helpers, ha_components_mp, ha_components_mqtt := ha_mqtt, ha_util):
        parent.__path__ = []  # type: ignore[attr-defined]

    for name, mod in [
        ("homeassistant", ha_mock),
        ("homeassistant.components", ha_components),
        ("homeassistant.components.mqtt", ha_mqtt),
        ("homeassistant.components.mqtt.const", ha_mqtt_const),
        ("homeassistant.components.http", ha_components_http),
        ("homeassistant.components.frontend", ha_components_frontend),
        ("homeassistant.components.image_proxy", ha_components_image_proxy),
        ("homeassistant.components.media_player", ha_components_mp),
        ("homeassistant.components.media_player.const", ha_components_mp_const),
        ("homeassistant.components.websocket_api", ha_components_websocket),
        ("homeassistant.components.button", ha_components_button),
        ("homeassistant.components.select", ha_components_select),
        ("homeassistant.components.sensor", ha_components_sensor),
        ("homeassistant.components.text", ha_components_text),
        ("homeassistant.config_entries", ha_config_entries),
        ("homeassistant.const", ha_const),
        ("homeassistant.core", ha_core),
        ("homeassistant.data_entry_flow", ha_data_entry_flow),
        ("homeassistant.exceptions", ha_exceptions),
        ("homeassistant.helpers", ha_helpers),
        ("homeassistant.helpers.dispatcher", ha_helpers_dispatcher),
        ("homeassistant.helpers.entity", ha_helpers_entity),
        ("homeassistant.helpers.aiohttp_client", ha_helpers_aiohttp_client),
        ("homeassistant.helpers.entity_platform", ha_helpers_entity_platform),
        ("homeassistant.helpers.event", ha_helpers_event),
        ("homeassistant.helpers.network", ha_network),
        ("homeassistant.helpers.selector", ha_helpers_selector),
        ("homeassistant.helpers.storage", ha_storage),
        ("homeassistant.util", ha_util),
        ("homeassistant.util.dt", ha_util_dt),
    ]:
        sys.modules.setdefault(name, mod)

    vol_mock = types.ModuleType("voluptuous")
    vol_mock.Schema = lambda *a, **k: None
    vol_mock.Required = lambda *a, **k: None
    vol_mock.Optional = lambda *a, **k: None
    vol_mock.Exclusive = lambda *a, **k: None
    vol_mock.In = lambda *a, **k: None
    vol_mock.Coerce = lambda *a, **k: None
    sys.modules.setdefault("voluptuous", vol_mock)


_install_ha_mocks()
