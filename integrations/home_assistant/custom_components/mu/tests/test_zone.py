"""Tests for zone.py module structure."""

import ast
import os


def test_zone_classes_exist():
    """Check expected classes are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    assert 'ZoneEntityManager' in classes
    assert 'MuZoneEntity' in classes


def test_zone_entity_features():
    """Verify supported features include volume and source selection."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        source = f.read()

    assert 'MediaPlayerEntityFeature.VOLUME_SET' in source
    assert 'MediaPlayerEntityFeature.VOLUME_MUTE' in source
    assert 'MediaPlayerEntityFeature.SELECT_SOURCE' in source


def test_zone_has_device_class():
    """Zone entity should have SPEAKER device class."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        source = f.read()

    assert 'MediaPlayerDeviceClass.SPEAKER' in source


def test_zone_no_deprecated_datetime():
    """Ensure deprecated datetime.utcfromtimestamp is not used."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        source = f.read()

    assert 'utcfromtimestamp' not in source, "Should use datetime.fromtimestamp(..., tz=timezone.utc)"


def test_zone_media_properties():
    """Zone should proxy media info from matched renderer."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        source = f.read()

    expected = ['media_title', 'media_artist', 'media_album_name', 'media_image_url', 'media_duration', 'media_position']
    for prop in expected:
        assert f'def {prop}' in source, f"Missing property: {prop}"


def test_zone_extra_attributes():
    """Zone should expose zone_id and controller_id as attributes."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'zone.py')
    with open(path) as f:
        source = f.read()

    assert 'zone_id' in source
    assert 'controller_id' in source
