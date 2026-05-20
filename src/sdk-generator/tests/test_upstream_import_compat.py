"""P3 (QUALITY_PLAN comparator 1) — run the REAL Stainless-generated
OneBusAway SDK's *own* test files against stainful's output.

Honest scope: the upstream suite is coupled to Stainless's private runtime
(`conftest.py` imports `onebusaway._utils.is_dict`, `DefaultAioHttpClient`)
and needs a Prism mock server for behavioral execution — that runtime is
deliberately ours, not theirs (already covered by test_runtime.py). What we
*can* measure Prism-free and faithfully is the strongest pure drop-in signal:
**do the imports the real upstream test files make against the `onebusaway`
package actually resolve against stainful's generated output?** Every upstream
`tests/api_resources/test_*.py` does `from onebusaway import …`,
`from onebusaway._utils import parse_datetime`, and
`from onebusaway.types import <ExactResponseName>` — if those resolve, a real
Stainless user's test file keeps importing unchanged.

Behavioral execution via Prism is the documented next step (QUALITY_PLAN).
"""

from __future__ import annotations

import ast
import importlib
import json
import sys
from pathlib import Path

import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"
UPSTREAM = (
    Path(__file__).parent / "oracles" / "onebusaway-python-sdk"
    / "tests" / "api_resources"
)
BASELINE = Path(__file__).parent / "quality" / "baseline_upstream_compat.json"

@pytest.fixture(scope="module")
def gen_pkg(tmp_path_factory):
    if not UPSTREAM.exists():
        pytest.skip("oracle missing — run python scripts/fetch_oracles.py")
    out = tmp_path_factory.mktemp("gen")
    api = build_ir(
        load_spec(str(FIX / "openapi.yml")),
        load_config(str(FIX / "stainless.yml")),
    )
    emit(api, str(out))
    sys.path.insert(0, str(out))
    for m in [m for m in sys.modules if m == "onebusaway" or m.startswith("onebusaway.")]:
        del sys.modules[m]
    return out


def _result(gen_pkg):
    files = sorted(UPSTREAM.glob("test_*.py"))
    ok, failures = 0, {}
    for f in files:
        tree = ast.parse(f.read_text())
        problems: list[str] = []
        for node in ast.walk(tree):
            if isinstance(node, ast.ImportFrom):
                if not node.module or node.module.split(".")[0] != "onebusaway":
                    continue
                try:
                    mod = importlib.import_module(node.module)
                except Exception as e:  # noqa: BLE001
                    problems.append(f"{node.module}: {type(e).__name__}: {e}")
                    continue
                for a in node.names:
                    if not hasattr(mod, a.name):
                        problems.append(f"{node.module}.{a.name} missing")
            elif isinstance(node, ast.Import):
                for a in node.names:
                    if a.name.split(".")[0] == "onebusaway":
                        try:
                            importlib.import_module(a.name)
                        except Exception as e:  # noqa: BLE001
                            problems.append(f"{a.name}: {type(e).__name__}: {e}")
        if problems:
            failures[f.name] = problems
        else:
            ok += 1
    rate = ok / len(files) if files else 1.0
    rep = {
        "total": len(files),
        "import_compatible": ok,
        "rate": round(rate, 4),
        "failures": failures,
    }
    (Path(__file__).parent / "quality" / "upstream_compat.json").write_text(
        json.dumps(rep, indent=2)
    )
    print(
        "\nUPSTREAM IMPORT COMPAT vs real Stainless test suite:\n"
        + json.dumps(rep, indent=2)
    )
    return rep


def test_no_regression_vs_baseline(gen_pkg):
    rep = _result(gen_pkg)
    if not BASELINE.exists():
        BASELINE.write_text(json.dumps(rep, indent=2))
        pytest.skip(f"baseline created at {BASELINE} — commit it; gate active next run")
    base = json.loads(BASELINE.read_text())
    assert rep["import_compatible"] >= base["import_compatible"], (
        f"upstream import-compat regressed: {rep['import_compatible']} "
        f"< baseline {base['import_compatible']}\n"
        + json.dumps(rep["failures"], indent=2)
    )


def test_client_symbols_are_drop_in(gen_pkg):
    # The core public surface every upstream test file imports first.
    import importlib

    ob = importlib.import_module("onebusaway")
    for sym in ("OnebusawaySDK", "AsyncOnebusawaySDK"):
        assert hasattr(ob, sym), sym
    u = importlib.import_module("onebusaway._utils")
    for sym in ("parse_date", "parse_datetime", "is_dict"):
        assert hasattr(u, sym), f"_utils.{sym}"
