"""Regeneration stability (QUALITY_PLAN §4 comparator 5) — the migration-trust
test. When a customer's spec grows (adds an endpoint), the code emitted for
their EXISTING endpoints must not churn. With the Stainless hosted generator
winding down, "regenerate without breaking my v1 users" is the property that
wins or loses migrating customers — independent of parity.
"""

from __future__ import annotations

import copy
import json
from pathlib import Path

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

# V1: one resource, one endpoint.
_SPEC_V1 = {
    "openapi": "3.0.0",
    "info": {"title": "Demo", "version": "1.0.0"},
    "servers": [{"url": "https://api.demo.test"}],
    "paths": {
        "/widgets/{id}": {
            "get": {
                "operationId": "getWidget",
                "parameters": [
                    {"name": "id", "in": "path", "required": True,
                     "schema": {"type": "string"}}
                ],
                "responses": {
                    "200": {
                        "description": "ok",
                        "content": {"application/json": {
                            "schema": {"$ref": "#/components/schemas/Widget"}}},
                    }
                },
            }
        }
    },
    "components": {
        "schemas": {
            "Widget": {
                "type": "object",
                "required": ["id", "name"],
                "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"},
                },
            }
        }
    },
}

_CONFIG_V1 = {
    "organization": {"name": "demo-sdk"},
    "resources": {
        "widgets": {"methods": {"retrieve": "get /widgets/{id}"}}
    },
    "targets": {"python": {"package_name": "demo"}},
    "environments": {"production": "https://api.demo.test"},
}


def _gen(spec: dict, config: dict, out: Path) -> Path:
    out.mkdir(parents=True, exist_ok=True)
    sp = out / "openapi.json"          # loader: .json -> json.loads
    cf = out / "stainless.yml"          # config loader: ruamel parses JSON too
    sp.write_text(json.dumps(spec))
    cf.write_text(json.dumps(config))
    api = build_ir(load_spec(str(sp)), load_config(str(cf)))
    dest = out / "gen"
    emit(api, str(dest))
    return dest / "demo"


def _snapshot(pkg: Path) -> dict[str, str]:
    return {
        p.relative_to(pkg).as_posix(): p.read_text()
        for p in sorted(pkg.rglob("*.py"))
    }


def test_additive_spec_change_does_not_churn_existing_endpoints(tmp_path):
    v1 = _gen(_SPEC_V1, _CONFIG_V1, tmp_path / "v1")
    before = _snapshot(v1)

    # V2 = V1 + a brand-new resource/endpoint. Nothing about widgets changed.
    spec_v2 = copy.deepcopy(_SPEC_V1)
    spec_v2["paths"]["/gadgets"] = {
        "get": {
            "operationId": "listGadgets",
            "responses": {"200": {
                "description": "ok",
                "content": {"application/json": {
                    "schema": {"type": "array",
                               "items": {"$ref": "#/components/schemas/Widget"}}}},
            }},
        }
    }
    config_v2 = copy.deepcopy(_CONFIG_V1)
    config_v2["resources"]["gadgets"] = {"methods": {"list": "get /gadgets"}}

    v2 = _gen(spec_v2, config_v2, tmp_path / "v2")
    after = _snapshot(v2)

    # The widgets resource file and its shared model must be byte-identical;
    # _client.py legitimately gains a `gadgets` attribute line, so it is not
    # required to be stable — only the existing endpoint's code is.
    assert "resources/widgets.py" in before and "resources/widgets.py" in after
    assert before["resources/widgets.py"] == after["resources/widgets.py"], (
        "existing endpoint code churned on an additive spec change"
    )

    # Per-file types/ layout: the existing endpoint owns its own model module.
    # Adding a new endpoint must not touch that file at all — the cleanest
    # possible statement of the no-churn property.
    mod = "types/widgets_retrieve_response.py"
    assert mod in before and mod in after
    assert "class WidgetRetrieveResponse(" in before[mod]
    assert before[mod] == after[mod], (
        "existing endpoint's model module churned on an additive change"
    )

    # New endpoint actually appeared (sanity: regeneration did something).
    assert "resources/gadgets.py" in after
    assert "resources/gadgets.py" not in before


def test_idempotent_regeneration_is_bit_identical(tmp_path):
    a = _snapshot(_gen(_SPEC_V1, _CONFIG_V1, tmp_path / "a"))
    b = _snapshot(_gen(_SPEC_V1, _CONFIG_V1, tmp_path / "b"))
    assert a == b, "regenerating the same spec twice is not deterministic"
