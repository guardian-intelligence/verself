"""P2 type-parity (QUALITY_PLAN comparator 3).

Stainless SDKs ship `py.typed` and type-check clean — that's a concrete
quality property, not a vibe. We generate the OneBusAway SDK and run mypy on
the generated package, measuring its own type errors (third-party stubs
silenced so the signal is *our* generated code, not httpx/pydantic).

Honest pattern: measure reality, baseline it, gate no-regression + an
absolute sanity ceiling. Comparing against mypy on the real Stainless SDK
(the "0 net-new vs baseline" ideal) is a documented refinement — it needs
the oracle's own deps/config and is heavy.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest

from stainful.config import load_config
from stainful.emit.python import emit
from stainful.ir.builder import build_ir
from stainful.openapi.loader import load_spec

FIX = Path(__file__).parent.parent / "examples" / "onebusaway"
BASELINE = Path(__file__).parent / "quality" / "baseline_type_parity.json"


@pytest.fixture(scope="module")
def gen(tmp_path_factory):
    out = tmp_path_factory.mktemp("typed")
    api = build_ir(
        load_spec(str(FIX / "openapi.yml")),
        load_config(str(FIX / "stainless.yml")),
    )
    emit(api, str(out))
    return out / "onebusaway"


def _mypy_errors(pkg: Path) -> tuple[int, str]:
    try:
        proc = subprocess.run(
            [
                sys.executable, "-m", "mypy", str(pkg),
                "--ignore-missing-imports", "--follow-imports=silent",
                "--no-error-summary", "--hide-error-context",
                "--no-color-output",
            ],
            capture_output=True, text=True, timeout=300,
        )
    except FileNotFoundError:
        pytest.skip("mypy not installed (uv pip install -e '.[dev]')")
    out = proc.stdout + proc.stderr
    n = sum(1 for line in out.splitlines() if ": error:" in line)
    return n, out


def test_generated_sdk_type_parity(gen):
    n, out = _mypy_errors(gen)
    rep = {"mypy_errors": n}
    (Path(__file__).parent / "quality" / "type_parity.json").write_text(
        json.dumps(rep, indent=2)
    )
    print(f"\nMYPY errors on generated SDK: {n}")
    if n:
        print("\n".join(out.splitlines()[:25]))

    # Absolute sanity ceiling — a catastrophic typing regression fails even
    # with no baseline (the generated tree is ~150 modules; this is generous).
    assert n < 400, f"generated SDK has {n} mypy errors (catastrophic)"

    if not BASELINE.exists():
        BASELINE.write_text(json.dumps(rep, indent=2))
        pytest.skip(f"baseline created at {BASELINE} — commit it; gate active next run")
    base = json.loads(BASELINE.read_text())
    assert n <= base["mypy_errors"], (
        f"type-parity regressed: {n} mypy errors > baseline "
        f"{base['mypy_errors']}\n" + "\n".join(out.splitlines()[:30])
    )
