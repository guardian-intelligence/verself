"""Multipart / file-upload conformance (RESEARCH §4 #10) — end-to-end on a
generated SDK. Before this, a `multipart/form-data` body was emitted as a
single opaque `body` param and sent as JSON (broken for real uploads, e.g.
OpenAI files/audio). Now: fields are expanded, binary fields typed
`FileTypes`, and the runtime sends real multipart with the file in `files`.
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

FIX = Path(__file__).parent / "fixtures" / "upload"


@pytest.fixture(scope="module")
def up(tmp_path_factory):
    out = tmp_path_factory.mktemp("up")
    emit(build_ir(load_spec(str(FIX / "openapi.yml")),
                  load_config(str(FIX / "stainless.yml"))), str(out))
    sys.path.insert(0, str(out))
    for m in [m for m in sys.modules if m == "upload" or m.startswith("upload.")]:
        del sys.modules[m]
    return out, importlib.import_module("upload")


def test_all_files_compile(up):
    import py_compile
    out, _ = up
    for f in out.rglob("*.py"):
        if "/_core/" not in str(f):
            py_compile.compile(str(f), doraise=True)


def test_signature_expands_fields_and_types_file(up):
    out, _ = up
    src = (out / "upload" / "resources" / "files.py").read_text()
    assert "file: FileTypes" in src          # binary field typed FileTypes
    assert "purpose:" in src                 # scalar field expanded too
    assert "multipart=True" in src           # runtime told it's multipart
    assert "to_jsonable(_body)" not in src   # NOT json-coerced


def test_real_multipart_request_sync_and_async(up):
    _, u = up
    captured = {}

    def h(req: httpx.Request) -> httpx.Response:
        captured["ct"] = req.headers.get("content-type", "")
        captured["body"] = req.content
        return httpx.Response(200, json={"id": "f1", "filename": "hello.txt"})

    c = u.UploadSDK(
        api_key="k", http_client=httpx.Client(transport=httpx.MockTransport(h))
    )
    r = c.files.create(file=b"hello world", purpose="fine-tune")
    assert type(r).__name__.endswith("Response") or r.id == "f1"
    assert r.id == "f1" and r.filename == "hello.txt"
    assert captured["ct"].startswith("multipart/form-data")   # real multipart
    assert b"hello world" in captured["body"]                  # file part sent
    assert b"fine-tune" in captured["body"]                    # scalar part sent

    async def go():
        ac = u.AsyncUploadSDK(
            api_key="k",
            http_client=httpx.AsyncClient(transport=httpx.MockTransport(h)),
        )
        return await ac.files.create(file=b"abc", purpose="assistants")

    rr = asyncio.run(go())
    assert rr.id == "f1"
    assert captured["ct"].startswith("multipart/form-data")
