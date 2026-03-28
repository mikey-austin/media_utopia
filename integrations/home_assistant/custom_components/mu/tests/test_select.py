"""Tests for select entity structure."""

import ast
import os


def test_select_classes_exist():
    """Check expected select classes are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'select.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    expected = ['PlaylistSelectionManager', 'PlaylistSelectManager', 'PlaylistRendererSelect']
    for cls in expected:
        assert cls in classes, f"Missing class: {cls}"


def test_select_has_icon():
    """Select entity should have icon."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'select.py')
    with open(path) as f:
        source = f.read()

    assert 'mdi:cast-audio' in source


def test_select_uses_callable_type():
    """Should use Callable from collections.abc, not lowercase callable."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'select.py')
    with open(path) as f:
        source = f.read()

    assert 'from collections.abc import Callable' in source
    # Should not use lowercase callable as type hint
    assert 'list[callable]' not in source
