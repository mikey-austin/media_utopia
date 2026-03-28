"""Tests for media player helpers and constants."""

import ast
import os


def test_media_player_constants():
    """Verify all expected constants are defined."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        source = f.read()

    # Check that REPEAT constants are defined
    assert 'REPEAT_ALL' in source
    assert 'REPEAT_OFF' in source
    assert 'REPEAT_ONE' in source


def test_media_player_has_renderer_manager():
    """Check RendererManager class exists."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        tree = ast.parse(f.read())

    classes = [n.name for n in ast.walk(tree) if isinstance(n, ast.ClassDef)]
    assert 'RendererManager' in classes
    assert 'MuRendererEntity' in classes


def test_media_player_entity_features():
    """Ensure supported features include expected capabilities."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        source = f.read()

    expected_features = [
        'MediaPlayerEntityFeature.PLAY',
        'MediaPlayerEntityFeature.PAUSE',
        'MediaPlayerEntityFeature.STOP',
        'MediaPlayerEntityFeature.NEXT_TRACK',
        'MediaPlayerEntityFeature.PREVIOUS_TRACK',
        'MediaPlayerEntityFeature.SEEK',
        'MediaPlayerEntityFeature.VOLUME_SET',
        'MediaPlayerEntityFeature.VOLUME_MUTE',
        'MediaPlayerEntityFeature.SHUFFLE_SET',
        'MediaPlayerEntityFeature.REPEAT_SET',
        'MediaPlayerEntityFeature.PLAY_MEDIA',
        'MediaPlayerEntityFeature.BROWSE_MEDIA',
    ]
    for feature in expected_features:
        assert feature in source, f"Missing {feature} in supported_features"


def test_media_player_browse_media_root_items():
    """Ensure browse_media provides expected root categories."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        source = f.read()

    # Should provide these browseable categories
    assert '"Current Queue"' in source
    assert '"Playlists"' in source
    assert '"Snapshots"' in source


def test_media_player_queue_jump_support():
    """Ensure queue_jump play_media handling exists."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        source = f.read()

    assert 'queue_jump:' in source, "Should support queue_jump: media IDs"


def test_no_deprecated_utcfromtimestamp():
    """Ensure deprecated datetime.utcfromtimestamp is not used."""
    path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'media_player.py')
    with open(path) as f:
        source = f.read()

    assert 'utcfromtimestamp' not in source, "Should use datetime.fromtimestamp(..., tz=timezone.utc) instead"
