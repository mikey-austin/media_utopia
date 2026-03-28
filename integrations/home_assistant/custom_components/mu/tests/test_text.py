"""Tests for text entity structure."""

import ast
import os


def test_text_classes_exist():
    """Check expected text entity classes."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'text.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    assert 'SnapshotNameManager' in classes
    assert 'SnapshotNameEntity' in classes


def test_text_entity_has_icon():
    """Text entity should have icon."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'text.py')
    with open(path) as f:
        source = f.read()

    assert 'mdi:label-outline' in source


def test_text_entity_limits():
    """Text entity should define min/max length."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'text.py')
    with open(path) as f:
        source = f.read()

    assert 'native_min' in source
    assert 'native_max' in source
