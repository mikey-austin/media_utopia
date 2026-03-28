"""Tests for const.py."""

from ..const import (
    DOMAIN,
    CONF_TOPIC_BASE,
    CONF_DISCOVERY_PREFIX,
    CONF_ENTITY_PREFIX,
    CONF_PLAYLIST_REFRESH,
    CONF_IDENTITY,
    CONF_ARTWORK_BASE_URL,
    DEFAULT_TOPIC_BASE,
    DEFAULT_DISCOVERY_PREFIX,
    DEFAULT_ENTITY_PREFIX,
    DEFAULT_PLAYLIST_REFRESH,
    DEFAULT_IDENTITY,
    DEFAULT_ARTWORK_BASE_URL,
    REPLY_TIMEOUT_SECONDS,
    RENDERER_LOAD_TIMEOUT_SECONDS,
    LEASE_TTL_MS,
    LEASE_RENEW_THRESHOLD_SECONDS,
)


def test_domain():
    assert DOMAIN == "mu"


def test_defaults():
    assert DEFAULT_TOPIC_BASE == "mu/v1"
    assert DEFAULT_DISCOVERY_PREFIX == "homeassistant"
    assert DEFAULT_ENTITY_PREFIX == "mu/ha"
    assert DEFAULT_PLAYLIST_REFRESH == 30
    assert DEFAULT_IDENTITY == "homeassistant"
    assert DEFAULT_ARTWORK_BASE_URL == ""


def test_timeouts_are_positive():
    assert REPLY_TIMEOUT_SECONDS > 0
    assert RENDERER_LOAD_TIMEOUT_SECONDS > 0
    assert LEASE_TTL_MS > 0
    assert LEASE_RENEW_THRESHOLD_SECONDS > 0


def test_conf_keys_are_strings():
    for key in [CONF_TOPIC_BASE, CONF_DISCOVERY_PREFIX, CONF_ENTITY_PREFIX,
                CONF_PLAYLIST_REFRESH, CONF_IDENTITY, CONF_ARTWORK_BASE_URL]:
        assert isinstance(key, str)
        assert len(key) > 0
