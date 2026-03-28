"""Tests for manifest.json validation."""

import json
import os
import re


def _load_manifest():
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'manifest.json')
    with open(path) as f:
        return json.load(f)


def test_required_fields():
    """Manifest must have all required HACS/HA fields."""
    m = _load_manifest()
    required = ['domain', 'name', 'version', 'documentation', 'dependencies', 'codeowners', 'config_flow', 'iot_class']
    for field in required:
        assert field in m, f"Missing required field: {field}"


def test_domain():
    m = _load_manifest()
    assert m['domain'] == 'mu'


def test_version_semver():
    m = _load_manifest()
    assert re.match(r'^\d+\.\d+\.\d+$', m['version']), f"Version '{m['version']}' is not semver"


def test_mqtt_dependency():
    m = _load_manifest()
    assert 'mqtt' in m['dependencies']


def test_config_flow_enabled():
    m = _load_manifest()
    assert m['config_flow'] is True


def test_iot_class():
    m = _load_manifest()
    assert m['iot_class'] == 'local_push'


def test_has_issue_tracker():
    m = _load_manifest()
    assert 'issue_tracker' in m
    assert m['issue_tracker'].startswith('http')


def test_codeowners_format():
    m = _load_manifest()
    for owner in m['codeowners']:
        assert owner.startswith('@'), f"Codeowner '{owner}' should start with @"
