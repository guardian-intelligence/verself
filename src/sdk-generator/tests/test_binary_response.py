"""Binary-response conformance — download endpoints (e.g. OpenAI
`audio.speech.create`, file content). A non-JSON 200 used to get no type and
be JSON-parsed (mangled). Now it returns raw `bytes`, end-to-end on the
generated SDK, sync + async.
"""

from __future__ import annotations

import asyncio
import importlib
import sys
from pathlib import Path

import httpx
import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent / "fixtures" / "download"
_AUDIO = b"\x00\x01ID3AUDIObytes\xff"


@pytest.fixture(scope="module")
def dl(tmp_path_factory):
    out = tmp_path_factory.mktemp("dl")
    emit(build_ir(load_spec(str(FIX / "openapi.yml")),
                  load_config(str(FIX / "stainless.yml"))), str(out))
    sys.path.insert(0, str(out))
    for m in [m for m in sys.modules if m == "download" or m.startswith("download.")]:
        del sys.modules[m]
    return out, importlib.import_module("download")


def test_all_files_compile(dl):
    import py_compile
    out, _ = dl
    for f in out.rglob("*.py"):
        if "/_core/" not in str(f):
            py_compile.compile(str(f), doraise=True)


def test_return_type_is_bytes(dl):
    out, _ = dl
    src = (out / "download" / "resources" / "speech.py").read_text()
    assert "-> bytes:" in src
    assert "cast_to=bytes" in src


def test_returns_raw_bytes_sync_and_async(dl):
    _, d = dl

    def h(req: httpx.Request) -> httpx.Response:
        return httpx.Response(200, content=_AUDIO,
                              headers={"content-type": "audio/mpeg"})

    c = d.DownloadSDK(
        api_key="k", http_client=httpx.Client(transport=httpx.MockTransport(h))
    )
    out = c.speech.create(input="hello", voice="alloy")
    assert isinstance(out, bytes) and out == _AUDIO   # not JSON-mangled

    async def go():
        ac = d.AsyncDownloadSDK(
            api_key="k",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(h)),
        )
        return await ac.speech.create(input="hi", voice="echo")

    assert asyncio.run(go()) == _AUDIO
