"""Slice 2 conformance: the real OneBusAway OpenAPI spec loads, the named-schema
registry is intact, and the canonical `allOf` envelope merges correctly.
"""

from pathlib import Path

import pytest

from stainful.errors import SpecError
from stainful.openapi.loader import load_spec
from stainful.openapi.resolver import flatten_allof, resolve_ref

SPEC = Path(__file__).parent.parent / "examples" / "onebusaway" / "openapi.yml"


def _doc():
    return load_spec(str(SPEC))


def test_spec_loads_and_is_navigable():
    doc = _doc()
    assert doc.version == "3.0.0"
    assert doc.info["title"] == "OneBusAway"
    assert doc.servers[0]["url"] == "https://api.pugetsound.onebusaway.org"
    assert doc.security == [{"ApiKeyAuth": []}]

    # named-schema registry = the Model boundary (DESIGN §3)
    assert "ResponseWrapper" in doc.schemas
    assert "Agency" in doc.schemas
    assert "AgencyResponse" in doc.schemas

    ops = {(o.verb, o.path): o for o in doc.operations()}
    agency = ops[("get", "/api/where/agency/{agencyID}.json")]
    assert agency.parameters[0]["name"] == "agencyID"
    assert agency.parameters[0]["in"] == "path"
    assert "200" in agency.responses


def test_resolve_ref_marks_component_schemas():
    doc = _doc()
    r = resolve_ref(doc, "#/components/schemas/Agency")
    assert r.schema_name == "Agency"
    assert r.target["type"] == "object"


def test_external_ref_is_explicit_error_not_fallback():
    doc = _doc()
    with pytest.raises(SpecError, match="external/file refs"):
        resolve_ref(doc, "common.yml#/Agency")


def test_unresolvable_ref_errors():
    doc = _doc()
    with pytest.raises(SpecError, match="does not resolve"):
        resolve_ref(doc, "#/components/schemas/DoesNotExist")


def test_canonical_allof_envelope_merges():
    doc = _doc()
    ops = {(o.verb, o.path): o for o in doc.operations()}
    schema = (
        ops[("get", "/api/where/agency/{agencyID}.json")]
        .responses["200"]["content"]["application/json"]["schema"]
    )
    assert "allOf" in schema  # precondition

    merged = flatten_allof(doc, schema)
    assert merged["type"] == "object"
    props = merged["properties"]
    # ResponseWrapper fields …
    assert {"code", "currentTime", "text", "version"} <= set(props)
    # … plus the inline `data` member
    assert props["data"] == {"$ref": "#/components/schemas/AgencyResponse"}
    # required is the union, including the inline `data`
    assert {"code", "currentTime", "text", "version", "data"} <= set(merged["required"])


def test_schema_without_allof_unchanged():
    doc = _doc()
    rw = doc.schemas["ResponseWrapper"]
    assert flatten_allof(doc, rw) is rw  # untouched, not deep-walked


def test_allof_cycle_is_explicit_error():
    # Synthetic: A allOf-> B allOf-> A. Must raise, not hang or silently flatten.
    from stainful.openapi.loader import OpenAPIDocument

    raw = {
        "openapi": "3.0.0",
        "components": {
            "schemas": {
                "A": {"allOf": [{"$ref": "#/components/schemas/B"}]},
                "B": {"allOf": [{"$ref": "#/components/schemas/A"}]},
            }
        },
    }
    doc = OpenAPIDocument(raw=raw, source="synthetic")
    with pytest.raises(SpecError, match="allOf cycle"):
        flatten_allof(doc, raw["components"]["schemas"]["A"])
