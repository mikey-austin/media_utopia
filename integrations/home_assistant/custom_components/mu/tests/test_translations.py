"""Tests for translation files."""

import json
import os


def _load_translations():
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'translations', 'en.json')
    with open(path) as f:
        return json.load(f)


def test_translations_valid_json():
    t = _load_translations()
    assert isinstance(t, dict)


def test_config_flow_translations():
    t = _load_translations()
    assert 'config' in t
    assert 'step' in t['config']
    assert 'user' in t['config']['step']
    user = t['config']['step']['user']
    assert 'data' in user
    assert 'topic_base' in user['data']


def test_options_flow_translations():
    t = _load_translations()
    assert 'options' in t, "Missing options flow translations"
    assert 'step' in t['options']
    assert 'init' in t['options']['step']
    init = t['options']['step']['init']
    assert 'data' in init


def test_all_config_fields_have_translations():
    t = _load_translations()
    user_data = t['config']['step']['user']['data']
    expected_fields = [
        'topic_base', 'discovery_prefix', 'entity_prefix',
        'identity', 'playlist_refresh_seconds', 'artwork_base_url'
    ]
    for field in expected_fields:
        assert field in user_data, f"Missing translation for config field: {field}"


def test_options_fields_match_config():
    """Options flow should have same fields as config flow."""
    t = _load_translations()
    config_data = set(t['config']['step']['user']['data'].keys())
    options_data = set(t['options']['step']['init']['data'].keys())
    assert config_data == options_data, f"Mismatch: config={config_data}, options={options_data}"


def test_data_descriptions_exist():
    """Config and options should have data_description for help text."""
    t = _load_translations()
    assert 'data_description' in t['config']['step']['user'], "Missing config data_description"
    assert 'data_description' in t['options']['step']['init'], "Missing options data_description"
