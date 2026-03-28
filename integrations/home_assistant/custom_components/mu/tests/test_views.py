"""Tests for views.py."""

import pytest
from ..views import ARTWORK_PROXY_PATH, ARTWORK_PROXY_NAME, ARTWORK_PROXY_CACHE_CONTROL


def test_artwork_proxy_constants():
    assert ARTWORK_PROXY_PATH == "/api/mu/artwork"
    assert ARTWORK_PROXY_NAME == "api:mu:artwork"
    assert "max-age" in ARTWORK_PROXY_CACHE_CONTROL


def test_artwork_proxy_path_format():
    """Path should start with /api/ for HA convention."""
    assert ARTWORK_PROXY_PATH.startswith("/api/")


def test_artwork_proxy_cache_control():
    """Cache control should be public and have reasonable max-age."""
    assert "public" in ARTWORK_PROXY_CACHE_CONTROL
    assert "max-age=" in ARTWORK_PROXY_CACHE_CONTROL
