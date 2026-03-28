"""Tests for HA services definition."""

import os


def test_services_yaml_exists():
    """services.yaml should exist."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'services.yaml')
    assert os.path.exists(path)


def test_services_yaml_content():
    """services.yaml should define expected services."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'services.yaml')
    with open(path) as f:
        content = f.read()

    expected_services = [
        'load_playlist',
        'clear_queue',
        'shuffle_queue',
        'save_snapshot',
    ]
    for service in expected_services:
        assert f'{service}:' in content, f"Missing service: {service}"


def test_services_have_descriptions():
    """Each service should have a description."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'services.yaml')
    with open(path) as f:
        content = f.read()

    assert content.count('description:') >= 4, "Expected at least 4 descriptions"


def test_services_have_fields():
    """Services should define their fields."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'services.yaml')
    with open(path) as f:
        content = f.read()

    assert 'renderer:' in content, "load_playlist should have renderer field"
    assert 'playlist:' in content, "load_playlist should have playlist field"
