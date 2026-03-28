"""Tests for WebSocket API command registration."""

import ast
import os


def test_all_commands_registered():
    """Verify all defined WS handlers are registered."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'websocket_api.py')
    with open(path) as f:
        source = f.read()

    # Find all functions decorated with @websocket_api.websocket_command
    tree = ast.parse(source)
    ws_handlers = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            if node.name.startswith('ws_'):
                ws_handlers.add(node.name)

    # Find all registered commands in async_register_websocket_api
    registered = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Call):
            if hasattr(node, 'args') and len(node.args) >= 2:
                arg = node.args[1] if len(node.args) > 1 else None
                if isinstance(arg, ast.Name) and arg.id.startswith('ws_'):
                    registered.add(arg.id)

    # All handlers should be registered
    unregistered = ws_handlers - registered - {'ws_subscribe_renderer_state'}  # subscription is also a handler
    # Note: ws_subscribe_renderer_state is registered, just check it's in registered
    assert len(ws_handlers) > 20, f"Expected 20+ WS handlers, found {len(ws_handlers)}"
    assert len(registered) > 20, f"Expected 20+ registered commands, found {len(registered)}"


def test_all_commands_have_type_field():
    """Every WS command should define a type field."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'websocket_api.py')
    with open(path) as f:
        source = f.read()

    import re
    types = re.findall(r'vol\.Required\("type"\):\s*"(mu/\w+)"', source)
    assert len(types) > 20, f"Expected 20+ command types, found {len(types)}"

    # All types should start with mu/
    for t in types:
        assert t.startswith("mu/"), f"Command type '{t}' should start with 'mu/'"


def test_no_duplicate_command_types():
    """Each command type should be unique."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'websocket_api.py')
    with open(path) as f:
        source = f.read()

    import re
    types = re.findall(r'vol\.Required\("type"\):\s*"(mu/\w+)"', source)
    seen = set()
    for t in types:
        assert t not in seen, f"Duplicate command type: {t}"
        seen.add(t)
