"""Fetch pinned third-party SDK oracles into tests/oracles/.

These repos are conformance inputs for optional quality tests. They are fetched
at pinned SHAs and are intentionally ignored by git.
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Oracle:
    name: str
    repo: str
    sha: str
    sparse_paths: tuple[str, ...]


ORACLES = (
    Oracle(
        name="onebusaway-python-sdk",
        repo="OneBusAway/python-sdk",
        sha="c4ce16d7f64ebc6e938481284ce0784c19d5c2cb",
        sparse_paths=("src", "tests", "pyproject.toml", "api.md"),
    ),
    Oracle(
        name="openai-python",
        repo="openai/openai-python",
        sha="658be644f48028ea3c7b1545034470fda75a70ba",
        sparse_paths=("src", "tests", "pyproject.toml"),
    ),
    Oracle(
        name="anthropic-sdk-python",
        repo="anthropics/anthropic-sdk-python",
        sha="a28508b8c22d806688e4d4faa97ca60ce04ce745",
        sparse_paths=("src", "tests", "pyproject.toml"),
    ),
)


def _run(args: list[str], *, cwd: Path | None = None) -> str:
    proc = subprocess.run(args, cwd=cwd, check=True, text=True, capture_output=True)
    return proc.stdout.strip()


def _head(path: Path) -> str | None:
    try:
        return _run(["git", "rev-parse", "HEAD"], cwd=path)
    except subprocess.CalledProcessError:
        return None


def fetch(oracle: Oracle, dest: Path) -> None:
    target = dest / oracle.name
    if (target / ".git").is_dir() and _head(target) == oracle.sha:
        print(f"{oracle.name} already at {oracle.sha}")
        return

    print(f"{oracle.name} @ {oracle.sha}")
    shutil.rmtree(target, ignore_errors=True)
    _run(
        [
            "git",
            "clone",
            "--quiet",
            "--filter=blob:none",
            "--no-checkout",
            f"https://github.com/{oracle.repo}",
            str(target),
        ]
    )
    _run(["git", "sparse-checkout", "init", "--cone"], cwd=target)
    _run(["git", "sparse-checkout", "set", *oracle.sparse_paths], cwd=target)
    _run(["git", "fetch", "--quiet", "--depth", "1", "origin", oracle.sha], cwd=target)
    _run(["git", "checkout", "--quiet", oracle.sha], cwd=target)
    print(f"{oracle.name} -> {_run(['git', 'rev-parse', '--short', 'HEAD'], cwd=target)}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("name", nargs="?", default="all")
    args = parser.parse_args()

    root = Path(__file__).parent.parent
    dest = root / "tests" / "oracles"
    dest.mkdir(parents=True, exist_ok=True)

    selected = [oracle for oracle in ORACLES if args.name in ("all", oracle.name)]
    if not selected:
        names = ", ".join(oracle.name for oracle in ORACLES)
        parser.error(f"unknown oracle {args.name!r}; expected one of: {names}")

    for oracle in selected:
        fetch(oracle, dest)
    print(f"oracles in {dest}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
