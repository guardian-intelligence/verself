"""Tier-1 differential: stainful's OneBusAway SDK vs the REAL Stainless-generated
`OneBusAway/python-sdk` (pinned oracle). QUALITY_PLAN §3–§4 comparator 2.

OneBusAway is the only target where spec + stainless.yml + golden output are all
public, so it's the only valid *surface*-fidelity measurement (same config drove
both). Gate = no regression vs the checked-in baseline; the report also prints
the QUALITY_PLAN §2 targets next to reality so the gap stays visible.
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

sys.path.insert(0, str(Path(__file__).parent))
from quality.compare import fidelity, not_worse_than  # noqa: E402
from quality.surface import extract  # noqa: E402

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"
ORACLE = (
    Path(__file__).parent / "oracles" / "onebusaway-python-sdk"
    / "src" / "onebusaway"
)
BASELINE = Path(__file__).parent / "quality" / "baseline_onebusaway.json"


@pytest.fixture(scope="module")
def report(tmp_path_factory):
    if not ORACLE.exists():
        pytest.skip(
            "oracle missing — run python scripts/fetch_oracles.py onebusaway-python-sdk"
        )
    out = tmp_path_factory.mktemp("gen")
    api = build_ir(
        load_spec(str(FIX / "openapi.yml")),
        load_config(str(FIX / "stainless.yml")),
    )
    emit(api, str(out))
    stainful_surface = extract(out / "onebusaway")
    oracle_surface = extract(ORACLE)
    rep = fidelity(stainful_surface, oracle_surface)
    # write the living scorecard artifact (gitignored)
    (Path(__file__).parent / "quality" / "fidelity_onebusaway.json").write_text(
        json.dumps(rep, indent=2)
    )
    print("\nFIDELITY vs real Stainless OneBusAway SDK:\n" + json.dumps(rep, indent=2))
    return rep


def test_no_regression_vs_baseline(report):
    if not BASELINE.exists():
        BASELINE.write_text(json.dumps(report, indent=2))
        pytest.skip(f"baseline created at {BASELINE} — commit it; gate active next run")
    baseline = json.loads(BASELINE.read_text())
    regressions = not_worse_than(report, baseline)
    assert not regressions, "fidelity regressed:\n" + "\n".join(regressions)


def test_drop_in_exception_contract(report):
    # The catchable error names are the config-INDEPENDENT drop-in contract:
    # a user's `except onebusaway.RateLimitError:` must keep working.
    core = {
        "APIError", "APIStatusError", "APIConnectionError", "APITimeoutError",
        "BadRequestError", "AuthenticationError", "PermissionDeniedError",
        "NotFoundError", "ConflictError", "UnprocessableEntityError",
        "RateLimitError", "InternalServerError",
    }
    missing = core - (core - set(report["missing_exceptions"]))
    assert not missing, f"core catchable exceptions not reproduced: {sorted(missing)}"


def test_resource_methods_substantially_reproduced(report):
    # Absolute sanity floors (the no-regression baseline is the real gate;
    # these just catch a catastrophic break independent of the baseline).
    assert report["resource_method_recall"] >= 0.80   # measured ~0.93
    assert report["export_recall"] >= 0.40            # measured ~0.49
