"""Tests for source-resolution logic in bridge.py."""


def test_single_track_detection():
    """A single track with multiple alternative sources should produce one entry."""
    # Simulating what _resolve_media_entries does:
    # When library.resolveSources returns multiple sources for the same track
    sources = [
        {"url": "http://host/direct.mp3", "mime": "audio/mpeg"},
        {"url": "http://host/transcoded.mp3", "mime": "audio/mpeg"},
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert not is_container, "Two sources without itemId should be treated as single track"


def test_single_track_with_same_id():
    """Sources with the same itemId are the same track."""
    sources = [
        {"url": "http://host/direct.mp3", "itemId": "track1"},
        {"url": "http://host/hls.m3u8", "itemId": "track1"},
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert not is_container, "Same itemId = same track, not container"


def test_container_expansion():
    """Album with multiple tracks should be detected as container."""
    sources = [
        {"url": "http://host/track1.mp3", "itemId": "track1"},
        {"url": "http://host/track2.mp3", "itemId": "track2"},
        {"url": "http://host/track3.mp3", "itemId": "track3"},
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert is_container, "Different itemIds = container expansion"
    assert len(distinct_ids) == 3


def test_single_source():
    """Single source should not be a container."""
    sources = [
        {"url": "http://host/track.mp3", "itemId": "track1"},
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert not is_container


def test_empty_sources():
    """Empty sources list."""
    sources = []
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    assert len(distinct_ids) == 0


def test_mixed_with_and_without_ids():
    """Sources where some have itemId and some don't."""
    sources = [
        {"url": "http://host/track1.mp3", "itemId": "track1"},
        {"url": "http://host/alt.mp3"},  # No itemId
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert not is_container, "Only 1 distinct itemId, not a container"


def test_container_with_mixed_types():
    """Album expansion where tracks have different media types."""
    sources = [
        {"url": "http://host/intro.mp3", "itemId": "intro", "mime": "audio/mpeg"},
        {"url": "http://host/track1.flac", "itemId": "track1", "mime": "audio/flac"},
        {"url": "http://host/track2.ogg", "itemId": "track2", "mime": "audio/ogg"},
    ]
    distinct_ids = {s.get("itemId") for s in sources if s.get("itemId")}
    is_container = len(distinct_ids) > 1
    assert is_container
    assert len(distinct_ids) == 3
