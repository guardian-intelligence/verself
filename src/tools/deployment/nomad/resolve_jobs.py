#!/usr/bin/env python3
"""Resolve authored Nomad specs into Bazel deploy artifacts."""

import argparse
import hashlib
import json
import os
import shutil
import sys
from pathlib import Path


ARTIFACT_PREFIX = "verself-artifact://"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--jobs-index", required=True)
    parser.add_argument("--out-dir", required=True)
    parser.add_argument("--artifact", action="append", default=[], help="output=path")
    parser.add_argument(
        "--embedded-template",
        action="append",
        default=[],
        help="placeholder=path; replaces exact placeholder string values inside authored Nomad specs before digest stamping.",
    )
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


def parse_embedded_templates(values: list[str]) -> dict[str, Path]:
    out: dict[str, Path] = {}
    for item in values:
        placeholder, sep, raw_path = item.partition("=")
        if not sep or not placeholder or not raw_path:
            raise ValueError(f"embedded template must be placeholder=path, got {item!r}")
        if placeholder in out:
            raise ValueError(f"duplicate embedded template placeholder {placeholder!r}")
        out[placeholder] = Path(raw_path)
    return out


def replace_embedded_templates(value: object, templates: dict[str, str], used: set[str]) -> object:
    if isinstance(value, str):
        replacement = templates.get(value)
        if replacement is None:
            return value
        used.add(value)
        return replacement
    if isinstance(value, list):
        return [replace_embedded_templates(item, templates, used) for item in value]
    if isinstance(value, dict):
        return {
            key: replace_embedded_templates(item, templates, used)
            for key, item in value.items()
        }
    return value


def artifact_delivery(index: dict) -> dict[str, object]:
    delivery = index.get("artifact_delivery")
    if not isinstance(delivery, dict):
        raise ValueError("jobs/index.json must include artifact_delivery")
    if delivery.get("public") is not False:
        raise ValueError("artifact_delivery.public must be false")

    source_prefix = delivery.get("getter_source_prefix")
    if not isinstance(source_prefix, str) or not source_prefix.startswith("s3::https://"):
        raise ValueError("artifact_delivery.getter_source_prefix must start with s3::https://")

    key_prefix = delivery.get("key_prefix")
    if not isinstance(key_prefix, str) or not key_prefix.strip("/"):
        raise ValueError("artifact_delivery.key_prefix is required")

    bucket = delivery.get("bucket")
    if not isinstance(bucket, str) or not bucket:
        raise ValueError("artifact_delivery.bucket is required")

    checksum_algorithm = delivery.get("checksum_algorithm")
    if checksum_algorithm != "sha256":
        raise ValueError(f"artifact_delivery.checksum_algorithm={checksum_algorithm!r}: only sha256 is supported")

    getter_options = delivery.get("getter_options", {})
    if not isinstance(getter_options, dict) or not all(isinstance(k, str) and isinstance(v, str) for k, v in getter_options.items()):
        raise ValueError("artifact_delivery.getter_options must be a string map")

    return {
        "bucket": bucket,
        "key_prefix": key_prefix.strip("/"),
        "source_prefix": source_prefix.rstrip("/"),
        "checksum_algorithm": checksum_algorithm,
        "getter_options": dict(getter_options),
    }


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
                getter_options = dict(binding["getter_options"])
                getter_options["checksum"] = binding["checksum"]
                stanza["GetterOptions"] = getter_options
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


def ordered_components(components: list[dict]) -> list[dict]:
    by_job_id: dict[str, dict] = {}
    for component in components:
        job_id = component.get("job_id")
        if not isinstance(job_id, str) or not job_id:
            raise ValueError(f"component index row missing job_id: {component!r}")
        if job_id in by_job_id:
            raise ValueError(f"duplicate Nomad job_id in index: {job_id}")
        by_job_id[job_id] = component

    temporary: set[str] = set()
    permanent: set[str] = set()
    out: list[dict] = []

    def visit(job_id: str, stack: list[str]) -> None:
        if job_id in permanent:
            return
        if job_id in temporary:
            cycle = " -> ".join(stack + [job_id])
            raise ValueError(f"Nomad job dependency cycle: {cycle}")
        temporary.add(job_id)
        component = by_job_id[job_id]
        depends_on = component.get("depends_on", [])
        if not isinstance(depends_on, list) or not all(isinstance(dep, str) for dep in depends_on):
            raise ValueError(f"{job_id}.depends_on must be a string list")
        for dependency in sorted(depends_on):
            if dependency not in by_job_id:
                raise ValueError(f"{job_id}.depends_on references unknown Nomad job {dependency!r}")
            visit(dependency, stack + [job_id])
        temporary.remove(job_id)
        permanent.add(job_id)
        out.append(component)

    for job_id in sorted(by_job_id):
        visit(job_id, [])
    return out


def main() -> int:
    args = parse_args()
    artifacts_by_output = parse_artifacts(args.artifact)
    embedded_template_paths = parse_embedded_templates(args.embedded_template)
    embedded_templates = {
        placeholder: path.read_text(encoding="utf-8")
        for placeholder, path in embedded_template_paths.items()
    }
    used_embedded_templates: set[str] = set()
    out_dir = Path(args.out_dir)
    if out_dir.exists():
        shutil.rmtree(out_dir)
    out_dir.mkdir(parents=True)

    index_path = Path(args.jobs_index)
    index = json.loads(index_path.read_text(encoding="utf-8"))
    components = index.get("components", [])
    if not isinstance(components, list):
        raise ValueError("jobs/index.json components must be a list")
    ordered = ordered_components(components)
    delivery = artifact_delivery(index)

    artifact_bindings = {}
    publish_artifacts_by_key = {}
    for component in components:
        for artifact in component.get("artifacts", []):
            output = artifact.get("output", "")
            path = artifacts_by_output.get(output)
            if path is None:
                raise ValueError(f"jobs/index.json references artifact {output!r}, but Bazel rule did not provide it")
            digest = sha256_file(path)
            key = f"{delivery['key_prefix']}/{digest}/{output}.tar"
            url = f"{delivery['source_prefix']}/{key}"
            checksum = f"{delivery['checksum_algorithm']}:{digest}"
            artifact_bindings[output] = {
                "path": str(path),
                "sha256": digest,
                "url": url,
                "key": key,
                "bucket": delivery["bucket"],
                "checksum": checksum,
                "getter_options": delivery["getter_options"],
            }
            publish_artifacts_by_key[(delivery["bucket"], key)] = {
                "output": output,
                "local_path": str(path),
                "sha256": digest,
                "bucket": delivery["bucket"],
                "key": key,
                "getter_source": url,
            }

    authored_jobs_dir = index_path.parent
    referenced = set()
    submit_rows = []
    for component in ordered:
        job_id = component["job_id"]
        spec_path = authored_jobs_dir / f"{job_id}.nomad.json"
        spec = json.loads(spec_path.read_text(encoding="utf-8"))
        spec = replace_embedded_templates(spec, embedded_templates, used_embedded_templates)
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
        raise ValueError(f"artifacts were provided but not referenced by authored jobs: {', '.join(extras)}")
    unused_templates = sorted(set(embedded_templates) - used_embedded_templates)
    if unused_templates:
        raise ValueError(f"embedded template placeholders were provided but not used: {', '.join(unused_templates)}")

    publish_manifest = {
        "artifact_delivery": index["artifact_delivery"],
        "artifacts": sorted(publish_artifacts_by_key.values(), key=lambda row: (row["bucket"], row["key"], row["output"])),
    }
    (out_dir / "publish.json").write_text(
        json.dumps(publish_manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    (out_dir / "submit.tsv").write_text(
        "".join("\t".join(row) + "\n" for row in submit_rows),
        encoding="utf-8",
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"resolve_jobs.py: {exc}", file=sys.stderr)
        raise SystemExit(1)
