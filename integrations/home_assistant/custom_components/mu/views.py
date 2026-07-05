"""HTTP views for the Mu integration."""

from __future__ import annotations

import asyncio
import logging
from urllib.parse import urlparse

from aiohttp import ClientError, ClientTimeout, web
from homeassistant.components.http import HomeAssistantView
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession

from .const import DOMAIN

_LOGGER = logging.getLogger(__name__)

ARTWORK_PROXY_PATH = "/api/mu/artwork"
ARTWORK_PROXY_NAME = "api:mu:artwork"
ARTWORK_PROXY_CACHE_CONTROL = "public, max-age=3600"
ARTWORK_MAX_BYTES = 10 * 1024 * 1024


class ArtworkProxyView(HomeAssistantView):
    """Proxy artwork from upstream HTTP servers with caching headers.

    The view is unauthenticated (it serves <img> tags that cannot attach
    auth headers), so upstream hosts are restricted to those the bridge has
    actually handed out artwork URLs for — otherwise this would be an open
    SSRF relay into the local network.
    """

    url = ARTWORK_PROXY_PATH
    name = ARTWORK_PROXY_NAME
    requires_auth = False

    def __init__(self, hass: HomeAssistant) -> None:
        self.hass = hass
        self._timeout = ClientTimeout(total=10)

    def _validate(self, request: web.Request) -> tuple[str | None, web.Response | None]:
        upstream = request.query.get("url")
        if not upstream:
            return None, web.Response(status=400, text="Missing url parameter")
        parsed = urlparse(upstream)
        if parsed.scheme not in {"http", "https"}:
            return None, web.Response(status=400, text="Invalid url")
        allowed = self.hass.data.get(DOMAIN, {}).get("artwork_hosts") or set()
        if parsed.netloc not in allowed:
            _LOGGER.debug("artwork proxy denied for host %s", parsed.netloc)
            return None, web.Response(status=403, text="Host not allowed")
        return upstream, None

    async def head(self, request: web.Request) -> web.Response:
        """Handle HEAD request for artwork by probing upstream."""
        upstream, error = self._validate(request)
        if error is not None:
            return error
        session = async_get_clientsession(self.hass)
        try:
            async with session.head(
                upstream, allow_redirects=True, timeout=self._timeout
            ) as resp:
                return web.Response(
                    status=resp.status,
                    headers={
                        "Content-Type": resp.headers.get("Content-Type", "image/jpeg"),
                        "Cache-Control": ARTWORK_PROXY_CACHE_CONTROL,
                    },
                )
        except (asyncio.TimeoutError, ClientError):
            return web.Response(status=504, text="Upstream fetch failed")

    async def get(self, request: web.Request) -> web.StreamResponse:
        """Proxy the artwork request."""
        upstream, error = self._validate(request)
        if error is not None:
            return error
        session = async_get_clientsession(self.hass)
        try:
            async with session.get(
                upstream, allow_redirects=True, timeout=self._timeout
            ) as resp:
                if resp.status != 200:
                    return web.Response(status=resp.status)

                length = resp.headers.get("Content-Length")
                if length and int(length) > ARTWORK_MAX_BYTES:
                    return web.Response(status=502, text="Upstream body too large")
                body = bytearray()
                async for chunk in resp.content.iter_chunked(64 * 1024):
                    body.extend(chunk)
                    if len(body) > ARTWORK_MAX_BYTES:
                        return web.Response(status=502, text="Upstream body too large")

                headers: dict[str, str] = {
                    "Cache-Control": ARTWORK_PROXY_CACHE_CONTROL,
                    "Content-Type": resp.headers.get("Content-Type", "image/jpeg"),
                    "X-Content-Type-Options": "nosniff",
                }
                for header in ("ETag", "Last-Modified"):
                    if header in resp.headers:
                        headers[header] = resp.headers[header]

                return web.Response(body=bytes(body), headers=headers)
        except (asyncio.TimeoutError, ClientError) as err:
            _LOGGER.debug("Artwork fetch failed for %s: %s", upstream, err)
            return web.Response(status=504, text="Upstream fetch failed")
        except Exception as err:
            _LOGGER.warning("Artwork proxy error: %s", err)
            return web.Response(status=500, text="Internal error")
