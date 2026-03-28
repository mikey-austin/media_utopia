"""Tests for the MU panel JavaScript file."""

import os
import re


def test_panel_file_exists():
    """mu-panel.js should exist."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    assert os.path.exists(path), "mu-panel.js not found"


def test_panel_uses_lit():
    """Panel should import from Lit."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    assert 'LitElement' in content
    assert 'html' in content
    assert 'css' in content


def test_panel_defines_component():
    """Panel should define mu-panel as a custom element."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    assert 'class MuPanel' in content or 'mu-panel' in content


def test_panel_has_keyboard_shortcuts():
    """Panel should support keyboard shortcuts."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    assert '_onKeydown' in content
    assert 'ArrowRight' in content
    assert 'ArrowLeft' in content


def test_panel_has_websocket_subscription():
    """Panel should use WebSocket subscription instead of just polling."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    assert 'subscribe_renderer_state' in content
    assert 'subscribeMessage' in content


def test_panel_has_transport_controls():
    """Panel should have play/pause/stop/next/prev controls."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    for control in ['play', 'pause', 'stop', 'next', 'prev']:
        assert control in content, f"Missing transport control: {control}"


def test_panel_no_console_log():
    """Panel should not have debug console.log statements."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    # Should not have console.log (warn/error are OK for actual errors)
    lines = content.split('\n')
    for i, line in enumerate(lines, 1):
        stripped = line.strip()
        if 'console.log(' in stripped and not stripped.startswith('//'):
            assert False, f"Found console.log on line {i}: {stripped}"


def test_panel_no_prompt_or_confirm():
    """Panel should not use blocking prompt() or confirm() calls."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'www', 'mu-panel.js')
    with open(path) as f:
        content = f.read()

    # Check for standalone prompt/confirm calls (not in comments)
    lines = content.split('\n')
    for i, line in enumerate(lines, 1):
        stripped = line.strip()
        if stripped.startswith('//'):
            continue
        if re.search(r'\bprompt\s*\(', stripped):
            assert False, f"Found blocking prompt() on line {i}: {stripped}"
        if re.search(r'\bconfirm\s*\(', stripped):
            assert False, f"Found blocking confirm() on line {i}: {stripped}"
