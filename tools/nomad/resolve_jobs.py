#!/usr/bin/env python3
"""Resolve renderer-produced Nomad specs into Bazel deploy artifacts."""

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


ARTIFACT_PREFIX = "verself-artifact://"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--cue-renderer", required=True)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--topology-dir", default="src/cue-renderer")
    parser.add_argument("--instance", default="prod")
    parser.add_argument("--artifact", action="append", default=[], help="output=path")
    return parser.parse_args()


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def canonical_digest(value: object) -> str:
    body = json.dumps(value, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(body).hexdigest()


def parse_artifacts(values: list[str]) -> dict[str, Path]:
    out = {}
    for item in values:
        name, sep, raw_path = item.partition("=")
        if not sep or not name or not raw_path:
            raise ValueError(f"artifact must be output=path, got {item!r}")
        out[name] = Path(raw_path)
    return out


def resolve_artifacts(spec: dict, artifact_bindings: dict[str, dict[str, str]]) -> set[str]:
    job = spec.get("Job")
    if not isinstance(job, dict):
        raise ValueError("spec.Job missing or wrong type")
    seen = set()
    for group in job.get("TaskGroups", []):
        for task in group.get("Tasks", []):
            for stanza in task.get("Artifacts", []):
                source = stanza.get("GetterSource", "")
                if not isinstance(source, str) or not source.startswith(ARTIFACT_PREFIX):
                    continue
                output = source.removeprefix(ARTIFACT_PREFIX)
                binding = artifact_bindings.get(output)
                if binding is None:
                    raise ValueError(f"artifact {output!r} referenced by spec but not provided to Bazel rule")
                stanza["GetterSource"] = binding["url"]
                stanza["GetterOptions"] = {"checksum": "sha256:" + binding["sha256"]}
                seen.add(output)
    return seen


def stamp_meta(spec: dict, artifact_digest: str) -> None:
    job = spec["Job"]
    meta = job.get("Meta")
    if not isinstance(meta, dict):
        meta = {}
    meta["artifact_sha256"] = artifact_digest
    meta.pop("spec_sha256", None)
    job["Meta"] = meta
    meta["spec_sha256"] = canonical_digest(spec)


def main() -> int:
    args = parse_args()
    artifacts_by_output = parse_artifacts(args.artifact)
    out_dir = Path(args.out_dir)
    if out_dir.exists():
        shutil.rmtree(out_dir)
    out_dir.mkdir(parents=True)

    with tempfile.TemporaryDirectory() as tmp:
        render_dir = Path(tmp) / "render"
        subprocess.run(
            [
                args.cue_renderer,
                "generate",
                "--topology-dir",
                args.topology_dir,
                "--instance",
                args.instance,
                "--output-dir",
                str(render_dir),
            ],
            check=True,
        )

        index_path = render_dir / "jobs" / "index.json"
        if not index_path.exists():
            (out_dir / "publish.tsv").write_text("", encoding="utf-8")
            (out_dir / "submit.tsv").write_text("", encoding="utf-8")
            return 0
        index = json.loads(index_path.read_text(encoding="utf-8"))
        artifact_base_url = index.get("artifact_base_url", "").rstrip("/")
        artifact_store_path = index.get("artifact_store_path", "").rstrip("/")
        if not artifact_base_url or not artifact_store_path:
            raise ValueError("jobs/index.json must include artifact_base_url and artifact_store_path")

        artifact_bindings = {}
        publish_rows = []
        for component in index.get("components", []):
            for artifact in component.get("artifacts", []):
                output = artifact.get("output", "")
                path = artifacts_by_output.get(output)
                if path is None:
                    raise ValueError(f"jobs/index.json references artifact {output!r}, but Bazel rule did not provide it")
                digest = sha256_file(path)
                url = f"{artifact_base_url}/sha256/{digest}/{output}.tar"
                remote_path = f"{artifact_store_path}/sha256/{digest}/{output}.tar"
                artifact_bindings[output] = {"path": str(path), "sha256": digest, "url": url, "remote_path": remote_path}
                publish_rows.append((output, str(path), remote_path, digest))

        referenced = set()
        submit_rows = []
        for component in index.get("components", []):
            job_id = component["job_id"]
            spec_path = render_dir / "jobs" / f"{job_id}.nomad.json"
            spec = json.loads(spec_path.read_text(encoding="utf-8"))
            seen = resolve_artifacts(spec, artifact_bindings)
            referenced.update(seen)
            artifact_digest = canonical_digest(
                [
                    {
                        "output": output,
                        "sha256": artifact_bindings[output]["sha256"],
                        "url": artifact_bindings[output]["url"],
                    }
                    for output in sorted(seen)
                ]
            )
            stamp_meta(spec, artifact_digest)
            resolved_path = out_dir / f"{job_id}.nomad.json"
            resolved_path.write_text(json.dumps(spec, indent=2, sort_keys=True) + "\n", encoding="utf-8")
            submit_rows.append((job_id, resolved_path.name))

        extras = sorted(set(artifact_bindings) - referenced)
        if extras:
            raise ValueError(f"artifacts were provided but not referenced by rendered jobs: {', '.join(extras)}")

        publish_rows = sorted(set(publish_rows))
        (out_dir / "publish.tsv").write_text(
            "".join("\t".join(row) + "\n" for row in publish_rows),
            encoding="utf-8",
        )
        (out_dir / "submit.tsv").write_text(
            "".join("\t".join(row) + "\n" for row in sorted(submit_rows)),
            encoding="utf-8",
        )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"resolve_jobs.py: {exc}", file=sys.stderr)
        raise SystemExit(1)
