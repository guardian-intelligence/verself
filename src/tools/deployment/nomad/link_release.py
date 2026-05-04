#!/usr/bin/env python3
"""Link authored Nomad jobs and Bazel artifacts into one release JSON."""

import argparse
import hashlib
import json
import sys
from pathlib import Path


ARTIFACT_PREFIX = "verself-artifact://"
SCHEMA_VERSION = 1


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--site", required=True)
    parser.add_argument("--jobs-target", required=True)
    parser.add_argument("--jobs-index", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--artifact", action="append", default=[], help="output=path")
    parser.add_argument("--job-spec", action="append", default=[], help="job_id=path")
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


def parse_kv_paths(values: list[str], label: str) -> dict[str, Path]:
    out: dict[str, Path] = {}
    for item in values:
        name, sep, raw_path = item.partition("=")
        if not sep or not name or not raw_path:
            raise ValueError(f"{label} must be name=path, got {item!r}")
        if name in out:
            raise ValueError(f"duplicate {label} {name!r}")
        out[name] = Path(raw_path)
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
        raise ValueError("Nomad release manifest must include artifact_delivery")
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


def resolve_artifacts(spec: dict, artifact_bindings: dict[str, dict[str, object]]) -> set[str]:
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
                stanza["GetterSource"] = binding["getter_source"]
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
        job_spec = component.get("job_spec")
        if not isinstance(job_spec, str) or not job_spec:
            raise ValueError(f"component {job_id!r} missing owner-local job_spec")
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
    artifacts_by_output = parse_kv_paths(args.artifact, "artifact")
    job_specs_by_id = parse_kv_paths(args.job_spec, "job spec")
    embedded_template_paths = parse_kv_paths(args.embedded_template, "embedded template")
    embedded_templates = {
        placeholder: path.read_text(encoding="utf-8")
        for placeholder, path in embedded_template_paths.items()
    }
    used_embedded_templates: set[str] = set()

    index_path = Path(args.jobs_index)
    index = json.loads(index_path.read_text(encoding="utf-8"))
    components = index.get("components", [])
    if not isinstance(components, list):
        raise ValueError("Nomad release manifest components must be a list")
    ordered = ordered_components(components)
    delivery = artifact_delivery(index)

    artifact_bindings: dict[str, dict[str, object]] = {}
    artifacts_by_key: dict[tuple[str, str], dict[str, object]] = {}
    for component in components:
        for artifact in component.get("artifacts", []):
            output = artifact.get("output", "")
            path = artifacts_by_output.get(output)
            if path is None:
                raise ValueError(f"Nomad release manifest references artifact {output!r}, but Bazel rule did not provide it")
            digest = sha256_file(path)
            key = f"{delivery['key_prefix']}/{digest}/{output}.tar"
            getter_source = f"{delivery['source_prefix']}/{key}"
            checksum = f"{delivery['checksum_algorithm']}:{digest}"
            binding = {
                "output": output,
                "local_path": str(path),
                "sha256": digest,
                "bucket": delivery["bucket"],
                "key": key,
                "getter_source": getter_source,
                "getter_options": delivery["getter_options"],
                "checksum": checksum,
            }
            artifact_bindings[output] = binding
            artifacts_by_key[(str(delivery["bucket"]), key)] = binding

    referenced = set()
    jobs = []
    submit_order = []
    for component in ordered:
        job_id = component["job_id"]
        spec_path = job_specs_by_id.get(job_id)
        if spec_path is None:
            raise ValueError(f"release manifest references job_id {job_id!r}, but Bazel rule did not provide its job spec")
        spec = json.loads(spec_path.read_text(encoding="utf-8"))
        spec = replace_embedded_templates(spec, embedded_templates, used_embedded_templates)
        seen = resolve_artifacts(spec, artifact_bindings)
        referenced.update(seen)
        artifact_digest = canonical_digest(
            [
                {
                    "output": output,
                    "sha256": artifact_bindings[output]["sha256"],
                    "getter_source": artifact_bindings[output]["getter_source"],
                }
                for output in sorted(seen)
            ]
        )
        stamp_meta(spec, artifact_digest)
        meta = spec["Job"]["Meta"]
        jobs.append(
            {
                "job_id": job_id,
                "spec_sha256": meta["spec_sha256"],
                "artifact_sha256": meta["artifact_sha256"],
                "spec": spec,
            }
        )
        submit_order.append(job_id)

    extras = sorted(set(artifact_bindings) - referenced)
    if extras:
        raise ValueError(f"artifacts were provided but not referenced by authored jobs: {', '.join(extras)}")
    unused_templates = sorted(set(embedded_templates) - used_embedded_templates)
    if unused_templates:
        raise ValueError(f"embedded template placeholders were provided but not used: {', '.join(unused_templates)}")
    extra_job_specs = sorted(set(job_specs_by_id) - set(submit_order))
    if extra_job_specs:
        raise ValueError(f"job specs were provided but not referenced by release manifest: {', '.join(extra_job_specs)}")

    release = {
        "schema_version": SCHEMA_VERSION,
        "site": args.site,
        "sha": "",
        "jobs_target": args.jobs_target,
        "artifact_delivery": index["artifact_delivery"],
        "artifacts": [
            {
                "output": item["output"],
                "local_path": item["local_path"],
                "sha256": item["sha256"],
                "bucket": item["bucket"],
                "key": item["key"],
                "getter_source": item["getter_source"],
                "getter_options": item["getter_options"],
            }
            for item in sorted(artifacts_by_key.values(), key=lambda row: (row["bucket"], row["key"], row["output"]))
        ],
        "jobs": jobs,
        "submit_order": submit_order,
    }
    Path(args.out).write_text(json.dumps(release, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"link_release.py: {exc}", file=sys.stderr)
        raise SystemExit(1)
