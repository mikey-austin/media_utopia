"""Tests for config_flow.py."""


def test_config_flow_class_exists():
    """Test that the MudConfigFlow class is importable."""
    # We just verify the module structure, not HA runtime
    import ast
    import os

    flow_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_flow.py')
    with open(flow_path) as f:
        tree = ast.parse(f.read())

    class_names = [node.name for node in ast.walk(tree) if isinstance(node, ast.ClassDef)]
    assert 'MudConfigFlow' in class_names


def test_config_flow_version():
    """Test that VERSION is set."""
    import ast
    import os

    flow_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_flow.py')
    with open(flow_path) as f:
        tree = ast.parse(f.read())

    for node in ast.walk(tree):
        if isinstance(node, ast.ClassDef) and node.name == 'MudConfigFlow':
            for item in node.body:
                if isinstance(item, ast.Assign):
                    for target in item.targets:
                        if isinstance(target, ast.Name) and target.id == 'VERSION':
                            assert isinstance(item.value, ast.Constant)
                            assert item.value.value >= 1
                            return
    assert False, "VERSION not found in MudConfigFlow"


def test_config_flow_has_async_step_user():
    """Test that the flow defines the async_step_user method."""
    import ast
    import os

    flow_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_flow.py')
    with open(flow_path) as f:
        tree = ast.parse(f.read())

    for node in ast.walk(tree):
        if isinstance(node, ast.ClassDef) and node.name == 'MudConfigFlow':
            method_names = [
                item.name for item in node.body
                if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef))
            ]
            assert 'async_step_user' in method_names
            return
    assert False, "MudConfigFlow class not found"


def test_config_flow_schema_fields():
    """Test that the config flow schema references all expected config keys."""
    import ast
    import os

    flow_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_flow.py')
    with open(flow_path) as f:
        source = f.read()

    expected_fields = [
        'CONF_TOPIC_BASE',
        'CONF_DISCOVERY_PREFIX',
        'CONF_ENTITY_PREFIX',
        'CONF_IDENTITY',
        'CONF_PLAYLIST_REFRESH',
        'CONF_ARTWORK_BASE_URL',
    ]
    for field in expected_fields:
        assert field in source, f"{field} not referenced in config_flow.py"


def test_config_flow_imports_from_const():
    """Test that config_flow imports necessary constants from const module."""
    import ast
    import os

    flow_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_flow.py')
    with open(flow_path) as f:
        tree = ast.parse(f.read())

    imported_names = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom) and node.module and node.module.endswith('.const'):
            for alias in node.names:
                imported_names.add(alias.name)

    # Should import both CONF_ keys and DEFAULT_ values
    assert 'CONF_TOPIC_BASE' in imported_names
    assert 'DEFAULT_TOPIC_BASE' in imported_names
    assert 'DOMAIN' in imported_names
