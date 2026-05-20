from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest


def main() -> int:
    root = Path(__file__).parent.parent
    os.chdir(root)
    sys.path.insert(0, str(root / "src"))
    return pytest.main(["-q", "tests"])


if __name__ == "__main__":
    raise SystemExit(main())
