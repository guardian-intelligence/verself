"""Shared fixtures.

`golden_onebusaway` simulates the emitter's vendoring step (DESIGN §5): copy
`src/stainful/runtime/` into the golden target as `<pkg>/_core/`, then make the
hand-written `onebusaway` package importable. This lets slice-4 tests drive the
real runtime through the exact code the emitter must reproduce.
"""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

import pytest

_ROOT = Path(__file__).parent.parent
_RUNTIME = _ROOT / "src" / "stainful" / "runtime"
_GOLDEN = _ROOT / "tests" / "golden" / "onebusaway"


@pytest.fixture()
def golden_onebusaway(monkeypatch):
    core = _GOLDEN / "onebusaway" / "_core"
    if core.exists():
        shutil.rmtree(core)
    shutil.copytree(_RUNTIME, core)
    (core / "__init__.py").write_text("")  # _core is a plain namespace, not the API
    monkeypatch.syspath_prepend(str(_GOLDEN))
    for mod in list(sys.modules):
        if mod == "onebusaway" or mod.startswith("onebusaway."):
            del sys.modules[mod]
    import onebusaway  # noqa: F401

    return onebusaway
