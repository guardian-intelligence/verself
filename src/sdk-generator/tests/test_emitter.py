"""Slice 5: the emitter reproduces a working, Stainless-shaped SDK from the IR.

Generates the OneBusAway SDK into a tmp dir, then asserts: every file compiles,
the package imports, sync+async behave against a mock transport, the §4 surface
is present, and the shape matches the hand-written golden tape-out.
"""

from __future__ import annotations

import importlib
import py_compile
import sys
from pathlib import Path

import httpx
import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"

_BODY = {
    "code": 200, "currentTime": 1700000000, "text": "OK", "version": 2,
    "data": {
        "entry": {"id": "1", "name": "Metro", "timezone": "PT", "url": "u",
                  "privateService": False},
        "references": {"agencies": [], "routes": [], "situations": [],
                       "stopTimes": [], "stops": [], "trips": []},
    },
}


@pytest.fixture()
def generated(tmp_path, monkeypatch):
    api = build_ir(
        load_spec(str(FIX / "openapi.yml")),
        load_config(str(FIX / "stainless.yml")),
    )
    emit(api, str(tmp_path))
    monkeypatch.syspath_prepend(str(tmp_path))
    for m in list(sys.modules):
        if m == "onebusaway" or m.startswith("onebusaway."):
            del sys.modules[m]
    return tmp_path, importlib.import_module("onebusaway")


def _mock(req: httpx.Request) -> httpx.Response:
    assert "key=tok" in str(req.url)  # auth query injected by generated client
    return httpx.Response(200, json=_BODY, headers={"x-request-id": "rq1"})


def test_every_generated_file_compiles(generated):
    out, _ = generated
    files = [p for p in out.rglob("*.py") if "/_core/" not in str(p)]
    assert len(files) > 30
    for f in files:
        py_compile.compile(str(f), doraise=True)


def test_sync_and_async_behaviour(generated):
    _, ob = generated
    c = ob.OnebusawaySDK(api_key="tok",
                       http_client=httpx.Client(transport=httpx.MockTransport(_mock)))
    r = c.agency.retrieve("1")
    assert type(r).__name__ == "AgencyRetrieveResponse"
    assert r.code == 200
    assert r.current_time == 1700000000          # currentTime alias mapped
    assert r.data.entry.name == "Metro"
    assert r.data.entry.private_service is False  # privateService alias
    assert r._request_id == "rq1"

    import asyncio

    async def go():
        ac = ob.AsyncOnebusawaySDK(
            api_key="tok",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(_mock)),
        )
        return await ac.agency.retrieve("1")

    assert asyncio.run(go()).data.entry.name == "Metro"


def test_path_guard_and_typed_errors_present(generated):
    _, ob = generated
    c = ob.OnebusawaySDK(api_key="tok",
                       http_client=httpx.Client(transport=httpx.MockTransport(_mock)))
    with pytest.raises(ValueError, match="non-empty value for `agency_id`"):
        c.agency.retrieve("")
    # §4 typed-error surface re-exported at top level (drop-in symbol contract)
    for name in ("RateLimitError", "NotFoundError", "APIStatusError", "APIError"):
        assert hasattr(ob, name)
    assert ob.not_given is ob.NOT_GIVEN


def test_shape_matches_golden_tapeout(generated):
    out, ob = generated
    # resource class + raw/streaming wrappers, exactly like the hand-written target
    agency_src = (out / "onebusaway" / "resources" / "agency.py").read_text()
    for sym in (
        "class AgencyResource(SyncAPIResource)",
        "class AsyncAgencyResource(AsyncAPIResource)",
        "class AgencyResourceWithRawResponse",
        "class AgencyResourceWithStreamingResponse",
        "def retrieve(",
        "timeout: float | httpx.Timeout | None | NotGiven = not_given",
        "cast_to=AgencyRetrieveResponse",
    ):
        assert sym in agency_src, sym
    # generated package declares only the external runtime deps
    pyproject = (out / "pyproject.toml").read_text()
    assert "httpx" in pyproject and "pydantic" in pyproject


def test_keyword_and_punctuation_field_names_are_safe(generated):
    # OneBusAway has a `from` field (keyword) and `git.branch` (punctuation);
    # both must be mangled+aliased so models.py is valid and importable.
    out, _ = generated
    # per-file types/ layout — scan every generated type module
    models = "\n".join(
        p.read_text() for p in (out / "onebusaway" / "types").glob("*.py")
    )
    assert "from_:" in models or 'alias="from"' in models
    # `git.branch` is valid ONLY inside the alias string, never as a field name
    assert 'alias="git.branch"' in models
    assert "\n    git.branch:" not in models
