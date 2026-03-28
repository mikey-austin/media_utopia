"""Tests for button entity structure."""

import ast
import os


def test_button_classes_exist():
    """Check all expected button classes are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'button.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    expected = [
        'ButtonManager', 'LeaseButton',
        'LeaseAcquireButton', 'LeaseRenewButton', 'LeaseReleaseButton',
        'PlaylistLoadButton', 'SnapshotSaveButton',
    ]
    for cls in expected:
        assert cls in classes, f"Missing class: {cls}"


def test_buttons_have_icons():
    """Check buttons have icons."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'button.py')
    with open(path) as f:
        source = f.read()

    assert 'mdi:lock-open-variant' in source  # Acquire
    assert 'mdi:lock-clock' in source  # Renew
    assert 'mdi:lock-open' in source  # Release
    assert 'mdi:content-save' in source  # Snapshot save
    assert 'mdi:playlist-play' in source  # Playlist load


def test_buttons_have_unique_ids():
    """All button classes should define unique_id."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'button.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    unique_id_count = 0
    for node in ast.walk(tree):
        if isinstance(node, ast.FunctionDef) and node.name == 'unique_id':
            unique_id_count += 1

    assert unique_id_count >= 3, f"Expected 3+ unique_id properties, found {unique_id_count}"
