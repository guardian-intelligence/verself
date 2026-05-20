"""P4 behavioral conformance (QUALITY_PLAN comparator 4).

The §4 resilience behaviors are tested on the hand-written golden tape-out
(test_runtime.py). This proves the SAME behaviors hold **end-to-end on a
freshly generated SDK**, exercised over its own vendored `_core` runtime —
which is what a real user actually runs.

Scenarios (sync + async): retry on 5xx, retry on 429 + honor Retry-After,
typed-error mapping with request_id. Driven by an httpx MockTransport; the
backoff sleep is monkeypatched to a no-op recorder so the test is fast and
can assert the delay logic without real waiting.
"""

from __future__ import annotations

import asyncio
import importlib
import sys
import time
from pathlib import Path

import httpx
import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"

_OK = {
    "code": 200, "currentTime": 1, "text": "OK", "version": 2,
    "data": {"entry": {"id": "1", "name": "Metro", "timezone": "PT", "url": "u"},
             "references": {"agencies": [], "routes": [], "situations": [],
                            "stopTimes": [], "stops": [], "trips": []}},
}


@pytest.fixture(scope="module")
def ob(tmp_path_factory):
    out = tmp_path_factory.mktemp("beh")
    emit(build_ir(load_spec(str(FIX / "openapi.yml")),
                  load_config(str(FIX / "stainless.yml"))), str(out))
    sys.path.insert(0, str(out))
    for m in [m for m in sys.modules if m == "onebusaway" or m.startswith("onebusaway.")]:
        del sys.modules[m]
    return importlib.import_module("onebusaway")


def _client(ob, handler):
    return ob.OnebusawaySDK(
        api_key="k", http_client=httpx.Client(transport=httpx.MockTransport(handler))
    )


def test_retries_5xx_then_succeeds(ob, monkeypatch):
    monkeypatch.setattr(time, "sleep", lambda _s: None)
    n = {"i": 0}

    def h(_req):
        n["i"] += 1
        return (httpx.Response(500, json={"error": "boom"}) if n["i"] == 1
                else httpx.Response(200, json=_OK))

    r = _client(ob, h).agency.retrieve("1")
    assert r.code == 200 and n["i"] == 2          # one retry, over generated SDK


def test_retry_after_header_is_honored(ob, monkeypatch):
    slept: list[float] = []
    monkeypatch.setattr(time, "sleep", lambda s: slept.append(s))
    n = {"i": 0}

    def h(_req):
        n["i"] += 1
        if n["i"] == 1:
            return httpx.Response(429, headers={"retry-after": "2"}, json={})
        return httpx.Response(200, json=_OK)

    _client(ob, h).agency.retrieve("1")
    assert slept and slept[0] == pytest.approx(2.0)  # Retry-After respected


def test_typed_error_with_request_id(ob):
    def h(_req):
        return httpx.Response(404, json={"error": {"code": "nope"}},
                              headers={"x-request-id": "req_x"})

    with pytest.raises(ob.NotFoundError) as e:
        _client(ob, h).agency.retrieve("missing")
    assert e.value.status_code == 404
    assert e.value.request_id == "req_x"
    assert isinstance(e.value, ob.APIStatusError)


def test_sync_async_error_parity(ob, monkeypatch):
    monkeypatch.setattr(time, "sleep", lambda _s: None)

    async def _no_sleep(_s):
        return None

    monkeypatch.setattr(asyncio, "sleep", _no_sleep)

    def h(_req):
        return httpx.Response(429, json={})

    with pytest.raises(ob.RateLimitError):
        _client(ob, h).agency.retrieve("1")

    async def go():
        ac = ob.AsyncOnebusawaySDK(
            api_key="k",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(h)),
        )
        with pytest.raises(ob.RateLimitError):
            await ac.agency.retrieve("1")

    asyncio.run(go())
