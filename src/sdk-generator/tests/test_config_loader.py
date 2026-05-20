"""Slice 1 conformance: the real OneBusAway Stainless config must load cleanly,
and malformed input must fail loud with a source location.
"""

from pathlib import Path

import pytest

from stainful.config import load_config
from stainful.config.model import CLIENT_KEY, SHARED_KEY
from stainful.errors import ConfigError

FIXTURE = Path(__file__).parent.parent / "examples" / "onebusaway" / "stainless.yml"


def test_onebusaway_config_loads():
    cfg = load_config(str(FIXTURE))

    # organization
    assert cfg.organization.name == "onebusaway-sdk"
    assert cfg.organization.docs == "https://developer.onebusaway.org"

    # resources: real resources only; $shared/$client lifted out
    assert SHARED_KEY not in cfg.resources
    assert CLIENT_KEY not in cfg.resources
    assert "agency" in cfg.resources
    assert "trip-details" in cfg.resources  # hyphenated name preserved

    # string shorthand normalized
    m = cfg.resources["agency"].methods["retrieve"]
    assert m.endpoint is not None
    assert m.endpoint.verb == "get"
    assert m.endpoint.path == "/api/where/agency/{agencyID}.json"

    # object form parsed, with paginated flag
    lm = cfg.resources["agencies_with_coverage"].methods["list"]
    assert lm.endpoint.verb == "get"
    assert lm.paginated is False

    # a resource with two methods
    ad = cfg.resources["arrival_and_departure"].methods
    assert set(ad) == {"list", "retrieve"}

    # $shared.models lifted
    assert cfg.shared_models["response_wrapper"].openapi_ref == "ResponseWrapper"
    assert cfg.shared_models["references"].openapi_ref == "Reference"

    # targets
    assert cfg.targets["python"].package_name == "onebusaway"
    assert cfg.targets["python"].publish == {"pypi": True}
    assert cfg.targets["java"].reverse_domain == "org.onebusaway"

    # client_settings.opts (auth via query param)
    api_key = cfg.client_settings.opts["api_key"]
    assert api_key.read_env == "ONEBUSAWAY_API_KEY"
    assert api_key.send_as_query_param == "key"
    assert api_key.auth["security_scheme"] == "ApiKeyAuth"

    # environments + settings
    assert cfg.environments["production"] == "https://api.pugetsound.onebusaway.org"
    assert cfg.settings["license"] == "Apache-2.0"

    # forward-compat: deferred Stainless keys preserved (not dropped), and
    # NOT warned about — they're valid config, just not acted on yet.
    assert "readme" in cfg.extra
    assert "query_settings" in cfg.extra


def test_real_config_is_clean_even_in_strict(tmp_path):
    # A real stainless.yml uses only valid keys (some deferred). It must load
    # with ZERO diagnostics even in strict mode — no false-positive warnings
    # (this is the clean-demo / good-drop-in-UX guarantee).
    load_config(str(FIXTURE), strict=True)  # must not raise


def test_strict_mode_flags_genuinely_unknown_keys(tmp_path):
    bad = tmp_path / "typo.yml"
    bad.write_text(
        "organization:\n  name: x\n"
        "resorces:\n  thing: {}\n"          # typo'd 'resources'
    )
    with pytest.raises(ConfigError, match="resorces"):
        load_config(str(bad), strict=True)
    # lenient mode still loads it (preserved), just warns
    cfg = load_config(str(bad))
    assert "resorces" in cfg.extra


def test_bad_endpoint_verb_fails_with_location(tmp_path):
    bad = tmp_path / "bad.yml"
    bad.write_text(
        "organization:\n"
        "  name: x\n"
        "resources:\n"
        "  thing:\n"
        "    methods:\n"
        "      list: fetch /things\n"  # 'fetch' is not a valid verb
    )
    with pytest.raises(ConfigError) as exc:
        load_config(str(bad))
    msg = str(exc.value)
    assert "invalid endpoint" in msg
    assert "bad.yml:" in msg  # carries source location


def test_missing_organization_fails():
    import tempfile
    with tempfile.NamedTemporaryFile("w", suffix=".yml", delete=False) as f:
        f.write("resources: {}\n")
        path = f.name
    with pytest.raises(ConfigError, match="organization"):
        load_config(path)
