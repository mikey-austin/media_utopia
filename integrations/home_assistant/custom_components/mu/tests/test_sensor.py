"""Tests for sensor entity structure."""

import ast
import os


def test_sensor_classes_exist():
    """Check all expected sensor classes are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'sensor.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    expected = [
        'LeaseSensorManager', 'LeaseSensorBase',
        'LeaseOwnerSensor', 'LeaseIDSensor', 'LeaseTTLSecondsSensor',
        'QueueLengthSensor', 'QueueIndexSensor', 'PlaybackStatusSensor',
        'ZoneSensorManager', 'ZoneControllerSensorBase',
        'TotalZonesSensor', 'ActiveZonesSensor', 'TotalSourcesSensor', 'SourceIDsSensor',
    ]
    for cls in expected:
        assert cls in classes, f"Missing class: {cls}"


def test_sensors_have_unique_ids():
    """All sensor classes should define unique_id."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'sensor.py')
    with open(path) as f:
        source = f.read()

    # Count unique_id property definitions
    tree = ast.parse(source)
    unique_id_count = 0
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == 'unique_id':
            unique_id_count += 1

    # Should have many unique_id definitions (one per sensor type)
    assert unique_id_count >= 8, f"Expected 8+ unique_id properties, found {unique_id_count}"


def test_sensors_have_icons():
    """Key sensors should have icons defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'sensor.py')
    with open(path) as f:
        source = f.read()

    assert 'mdi:account-key' in source  # LeaseOwnerSensor
    assert 'mdi:playlist-music' in source  # QueueLengthSensor
    assert 'mdi:timer-outline' in source  # LeaseTTLSensor


def test_no_deprecated_datetime():
    """Ensure no deprecated datetime usage."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'sensor.py')
    with open(path) as f:
        source = f.read()

    assert 'utcfromtimestamp' not in source
