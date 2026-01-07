"""HTTP views for the Mu integration."""

from __future__ import annotations

import asyncio
from typing import Any
from urllib.parse import urlparse

from aiohttp import ClientError, ClientTimeout, web
from homeassistant.components.http import HomeAssistantView
from homeassistant.core import HomeAssistant
from homeassistant.helpers.aiohttp_client import async_get_clientsession

ARTWORK_PROXY_PATH = "/api/mu/artwork"
ARTWORK_PROXY_NAME = "api:mu:artwork"
ARTWORK_PROXY_CACHE_CONTROL = "public, max-age=3600"


class ArtworkProxyView(HomeAssistantView):
    """Proxy artwork from upstream HTTP servers with caching headers."""

    url = ARTWORK_PROXY_PATH
    name = ARTWORK_PROXY_NAME
    requires_auth = False

    def __init__(self, hass: HomeAssistant) -> None:
        self.hass = hass
        self._timeout = ClientTimeout(total=10)

    async def get(self, request: web.Request) -> web.StreamResponse:
        """Proxy the artwork request."""
        upstream = request.query.get("url")
        if not upstream:
            return web.Response(status=400, text="Missing url parameter")

        parsed = urlparse(upstream)
        if parsed.scheme not in {"http", "https"}:
            return web.Response(status=400, text="Invalid url")

        session = async_get_clientsession(self.hass)
        try:
            async with session.get(
                upstream, allow_redirects=True, timeout=self._timeout
            ) as resp:
                if resp.status != 200:
                    return web.Response(status=resp.status)

                body = await resp.read()
                headers: dict[str, Any] = {
                    "Cache-Control": ARTWORK_PROXY_CACHE_CONTROL,
                    "Content-Type": resp.headers.get("Content-Type", "image/jpeg"),
                    "X-Content-Type-Options": "nosniff",
                }
                for header in ("ETag", "Last-Modified", "Content-Length"):
                    if header in resp.headers:
                        headers[header] = resp.headers[header]

                return web.Response(body=body, headers=headers)
        except (asyncio.TimeoutError, ClientError):
            return web.Response(status=504, text="Upstream fetch failed")
        except Exception:
            return web.Response(status=500, text="Internal error")
