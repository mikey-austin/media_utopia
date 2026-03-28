"""Tests for __init__.py module structure."""

import ast
import json
import os


def test_platforms_defined():
    """Verify PLATFORMS list contains expected platforms."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), '__init__.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    for node in ast.walk(tree):
        if isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == 'PLATFORMS':
                    assert isinstance(node.value, ast.List)
                    platforms = [elt.value for elt in node.value.elts if isinstance(elt, ast.Constant)]
                    assert 'media_player' in platforms
                    assert 'button' in platforms
                    assert 'sensor' in platforms
                    assert 'select' in platforms
                    assert 'text' in platforms
                    return
    assert False, "PLATFORMS not found"


def test_manifest_valid():
    """Verify manifest.json is valid and has required fields."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'manifest.json')
    with open(path) as f:
        manifest = json.load(f)

    assert manifest['domain'] == 'mu'
    assert manifest['name'] == 'Media Utopia'
    assert 'version' in manifest
    assert 'mqtt' in manifest.get('dependencies', [])
    assert manifest.get('config_flow') is True
    assert manifest.get('iot_class') == 'local_push'


def test_manifest_version_format():
    """Version should be semver format."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'manifest.json')
    with open(path) as f:
        manifest = json.load(f)

    version = manifest['version']
    parts = version.split('.')
    assert len(parts) == 3, f"Version {version} should have 3 parts"
    for part in parts:
        assert part.isdigit(), f"Version part {part} should be numeric"


def test_setup_and_unload_functions():
    """Verify async_setup_entry and async_unload_entry are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), '__init__.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    functions = [n.name for n in ast.walk(tree) if isinstance(n, (ast.FunctionDef, ast.AsyncFunctionDef))]
    assert 'async_setup_entry' in functions
    assert 'async_unload_entry' in functions


def test_no_deprecated_apis():
    """Check for deprecated API usage in init."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), '__init__.py')
    with open(path) as f:
        source = f.read()

    assert 'utcfromtimestamp' not in source
