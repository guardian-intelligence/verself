"""Slice 3 conformance: OneBusAway (spec + config) -> a coherent API IR.

Tests the IR alone — no code emission. This is where the moat is verified:
3-valued cardinality, ModelRef cycle-breaking, allOf-envelope typing, the
config's idiomatic resource/method/auth overlay.
"""

from pathlib import Path

import pytest

from stainful.config import load_config
from stainful.errors import IRBuildError
from stainful.ir.builder import build_ir
from stainful.ir.model import HTTPVerb
from stainful.ir.types import ModelRef, ObjectType, PrimitiveKind, PrimitiveType
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"


def _api():
    return build_ir(
        load_spec(str(FIX / "openapi.yml")),
        load_config(str(FIX / "stainless.yml")),
    )


def _prop(obj: ObjectType, name: str):
    return next(p for p in obj.properties if p.name == name)


def test_api_top_level():
    api = _api()
    assert api.name == "onebusaway-sdk"
    assert api.version == "0.0.2"
    assert api.environments == {
        "production": "https://api.pugetsound.onebusaway.org"
    }


def test_auth_from_config_overlay():
    (scheme,) = _api().auth
    assert scheme.name == "api_key"
    assert scheme.kind == "apiKey"
    assert scheme.location == "query"        # send_as_query_param wins
    assert scheme.param_name == "key"
    assert scheme.read_env == "ONEBUSAWAY_API_KEY"


def test_models_registered_with_3valued_cardinality():
    api = _api()
    assert len(api.models) > 30

    rw = api.models["ResponseWrapper"].type
    assert isinstance(rw, ObjectType)
    code = _prop(rw, "code")
    assert code.required is True            # in `required`
    assert code.nullable is False          # not nullable -> distinct from optional
    assert code.type == PrimitiveType(PrimitiveKind.INTEGER)


def test_component_refs_become_modelref_not_inlined():
    # AgencyResponse.entry -> $ref Agency  ==> ModelRef, NOT an inlined object.
    ar = _api().models["AgencyResponse"].type
    assert isinstance(ar, ObjectType)
    entry = _prop(ar, "entry")
    assert entry.type == ModelRef("Agency")
    assert entry.required is True
    assert _prop(ar, "references").type == ModelRef("Reference")


def test_recursive_schema_graph_does_not_blow_up():
    # 141 $refs incl. mutually-referencing models; ModelRef must keep this finite.
    api = _api()
    assert "Situation" in api.models  # large interlinked schema; built without recursion


def test_resource_tree_and_method_overlay():
    api = _api()
    assert api.root.name == "$client"
    subs = {r.name: r for r in api.root.subresources}

    # hyphenated + multi-method resources survive the overlay
    assert "trip-details" in subs
    assert {m.name for m in subs["arrival_and_departure"].methods} == {
        "list", "retrieve"
    }

    retrieve = next(m for m in subs["agency"].methods if m.name == "retrieve")
    assert retrieve.http_verb == HTTPVerb.GET
    assert retrieve.idempotent is True
    (pp,) = retrieve.path_params
    assert pp.name == "agencyID"
    assert pp.required is True
    assert pp.type == PrimitiveType(PrimitiveKind.STRING)


def test_allof_envelope_typed_with_modelref_data():
    retrieve = next(
        m
        for r in _api().root.subresources if r.name == "agency"
        for m in r.methods if m.name == "retrieve"
    )
    resp = retrieve.responses["200"]
    assert isinstance(resp, ObjectType)
    # Stainless envelope shape: `class XResponse(ResponseWrapper)` — the shared
    # allOf member becomes a BASE, not merged in. `code/currentTime/...` live
    # on the ResponseWrapper base model, not on the envelope itself.
    assert ModelRef("ResponseWrapper") in resp.bases
    names = {p.name for p in resp.properties}
    assert names == {"data"}                          # only the inline member
    data = _prop(resp, "data")
    assert data.type == ModelRef("AgencyResponse")    # inner $ref preserved
    assert data.required is True
    # the inherited fields are defined on the shared base model
    base = _api().models["ResponseWrapper"].type
    assert {"code", "currentTime", "text", "version"} <= {
        p.name for p in base.properties
    }


def test_missing_operation_is_loud_error_not_fallback(tmp_path):
    bad = tmp_path / "c.yml"
    bad.write_text(
        "organization:\n  name: x\n"
        "resources:\n  thing:\n    methods:\n"
        "      list: get /nope\n"
    )
    with pytest.raises(IRBuildError, match="no matching operation"):
        build_ir(load_spec(str(FIX / "openapi.yml")), load_config(str(bad)))


def test_build_ir_rejects_unloaded_spec():
    with pytest.raises(IRBuildError, match="OpenAPIDocument"):
        build_ir({"openapi": "3.0.0"}, load_config(str(FIX / "stainless.yml")))
