"""`load_spec(path) -> OpenAPIDocument` (DESIGN §6 slice 2).

A *thin navigable wrapper*, not a typed re-model of OpenAPI. The rich semantic
typing is the IR's job (slice 3); duplicating it here would violate "build deep
modules, minimize interfaces". This module only:

  1. parses OpenAPI 3.x (YAML/JSON), position-aware via ruamel;
  2. exposes `components/schemas` as a **named registry** — the Model boundary
     (DESIGN §3: a schema is a Model iff referenced by $ref under components);
  3. iterates operations.

It deliberately does NOT inline component $refs into properties — that is where
recursive cycles live and where the named-Model boundary must be preserved. One
ref hop and allOf flattening are provided by `openapi.resolver`, on demand.
"""

from __future__ import annotations

import json
from collections.abc import Iterator
from dataclasses import dataclass
from pathlib import Path

from ruamel.yaml import YAML
from ruamel.yaml.error import YAMLError

from stainful.errors import SourceLoc, SpecError

_HTTP_VERBS = ("get", "post", "put", "patch", "delete")


@dataclass
class Operation:
    path: str
    verb: str
    raw: dict
    # OpenAPI: parameters declared on the PATH ITEM apply to every operation
    # under that path; operation-level params override by (name, in).
    path_item_params: tuple[dict, ...] = ()

    @property
    def parameters(self) -> list[dict]:
        op_params = list(self.raw.get("parameters", []) or [])
        seen = {
            (p.get("name"), p.get("in"))
            for p in op_params
            if isinstance(p, dict)
        }
        merged = list(op_params)
        for p in self.path_item_params:
            if isinstance(p, dict) and (p.get("name"), p.get("in")) not in seen:
                merged.append(p)
        return merged

    @property
    def request_body(self) -> dict | None:
        return self.raw.get("requestBody")

    @property
    def responses(self) -> dict:
        return dict(self.raw.get("responses", {}) or {})

    @property
    def operation_id(self) -> str | None:
        return self.raw.get("operationId")

    @property
    def summary(self) -> str | None:
        return self.raw.get("summary") or self.raw.get("description")


class OpenAPIDocument:
    """Navigable, *un-inlined* OpenAPI document. $refs stay refs by design."""

    def __init__(self, raw: dict, source: str) -> None:
        self.raw = raw
        self.source = source

    # --- top level ---------------------------------------------------------
    @property
    def version(self) -> str:
        return str(self.raw.get("openapi", ""))

    @property
    def info(self) -> dict:
        return dict(self.raw.get("info", {}) or {})

    @property
    def servers(self) -> list[dict]:
        return list(self.raw.get("servers", []) or [])

    @property
    def security(self) -> list:
        return list(self.raw.get("security", []) or [])

    @property
    def security_schemes(self) -> dict:
        return dict((self.raw.get("components", {}) or {}).get("securitySchemes", {}) or {})

    @property
    def schemas(self) -> dict[str, dict]:
        """`components/schemas` — the named Model registry."""
        return dict((self.raw.get("components", {}) or {}).get("schemas", {}) or {})

    def operations(self) -> Iterator[Operation]:
        for path, item in (self.raw.get("paths", {}) or {}).items():
            if not isinstance(item, dict):
                continue
            shared = tuple(item.get("parameters", []) or [])
            for verb in _HTTP_VERBS:
                op = item.get(verb)
                if isinstance(op, dict):
                    yield Operation(
                        path=path, verb=verb, raw=op, path_item_params=shared
                    )


def load_spec(path: str) -> OpenAPIDocument:
    p = Path(path)
    if not p.is_file():
        raise SpecError("spec file not found", SourceLoc(path))
    text = p.read_text()
    try:
        if p.suffix in (".json",):
            raw = json.loads(text)
        else:
            raw = YAML().load(text)  # round-trip: positions + anchors
    except (YAMLError, json.JSONDecodeError) as exc:
        raise SpecError(f"parse error: {exc}", SourceLoc(path)) from exc

    if not isinstance(raw, dict):
        raise SpecError("top-level OpenAPI document must be a mapping", SourceLoc(path))
    version = str(raw.get("openapi", ""))
    if not version.startswith("3."):
        raise SpecError(
            f"unsupported OpenAPI version {version!r}; stainful v1 targets 3.x",
            SourceLoc(path),
        )
    return OpenAPIDocument(raw=raw, source=path)
