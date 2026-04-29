#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
self_path="${script_dir}/$(basename "$0")"
platform_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(git -C "${platform_root}" rev-parse --show-toplevel)"

inventory="${platform_root}/ansible/inventory/${VERSELF_SITE:-prod}.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Provision the environment first." >&2
  exit 1
fi

# Re-exec under the controller-side OTLP buffer agent so the smoke span
# we emit below is buffered by scripts/with-otel-agent.sh's file_storage
# queue. Same agent ansible deploys use, same disk durability, same SSH
# transport — no per-canary `ssh -L` race.
if [[ -z "${VERSELF_OTEL_AGENT_INNER:-}" ]]; then
  export VERSELF_OTEL_AGENT_INNER=1
  export VERSELF_ANSIBLE_INVENTORY="${inventory}"
  export VERSELF_DEPLOY_KIND="bazel-smoke-test"
  exec "${script_dir}/with-otel-agent.sh" "${self_path}" "$@"
fi

cd "${platform_root}"

# Inside the wrapper now: VERSELF_OTLP_ENDPOINT and OTEL_RESOURCE_ATTRIBUTES
# are set, deploy_identity.sh has been sourced.
export VERIFICATION_RUN_ID="${VERSELF_DEPLOY_RUN_KEY}"

bazelisk="${BAZELISK:-bazelisk}"
(
  cd "${repo_root}"
  "${bazelisk}" run //tools/bazel:doctor
)

attrs="$(
  RUN_ID="${VERIFICATION_RUN_ID}" python3 - <<'PY'
import json
import os

print(json.dumps({
    "verself.verification_run": os.environ["RUN_ID"],
    "verself.deploy_id": os.environ.get("VERSELF_DEPLOY_ID", ""),
    "verself.deploy_run_key": os.environ.get("VERSELF_DEPLOY_RUN_KEY", ""),
    "verself.deploy_kind": os.environ.get("VERSELF_DEPLOY_KIND", "bazel-smoke-test"),
    "build.system": "bazel",
    "bazel.version": "9.1.0",
    "bazelisk.version": "1.28.1",
    "artifact.root": "artifacts",
    "smoke_artifact.root": "smoke-artifacts",
}))
PY
)"
(
  cd "${repo_root}/src/otel"
  SMOKE_SPAN_SERVICE="bazel-smoke-test" \
    SMOKE_SPAN_NAME="bazel.smoke_test" \
    SMOKE_SPAN_ATTRS_JSON="${attrs}" \
    go run ./cmd/smoke-span
)

assert_ready() {
  local query_output="$1"
  python3 -c '
import json
import sys

payload = sys.argv[1].strip()
row = json.loads(payload.splitlines()[0]) if payload else {}
ready = (
    row.get("rows", 0) >= 1
    and row.get("status_ok", 0) >= 1
    and row.get("build_system", "") == "bazel"
    and row.get("bazel_version", "") == "9.1.0"
    and row.get("bazelisk_version", "") == "1.28.1"
    and row.get("artifact_root", "") == "artifacts"
    and row.get("smoke_artifact_root", "") == "smoke-artifacts"
)
raise SystemExit(0 if ready else 1)
' "${query_output}"
}

query_output=""
for _ in $(seq 1 45); do
  query_output="$(./scripts/clickhouse.sh \
    --database default \
    --param_deploy_id="${VERSELF_DEPLOY_ID}" \
    --param_deploy_run_key="${VERSELF_DEPLOY_RUN_KEY}" \
    --param_verification_run="${VERIFICATION_RUN_ID}" \
    --query "
      SELECT
        count() AS rows,
        countIf(StatusCode = 'Ok') AS status_ok,
        any(SpanAttributes['build.system']) AS build_system,
        any(SpanAttributes['bazel.version']) AS bazel_version,
        any(SpanAttributes['bazelisk.version']) AS bazelisk_version,
        any(SpanAttributes['artifact.root']) AS artifact_root,
        any(SpanAttributes['smoke_artifact.root']) AS smoke_artifact_root
      FROM default.otel_traces
      WHERE Timestamp > now() - INTERVAL 20 MINUTE
        AND ServiceName = 'bazel-smoke-test'
        AND SpanName = 'bazel.smoke_test'
        AND SpanAttributes['verself.deploy_id'] = {deploy_id:String}
        AND SpanAttributes['verself.deploy_run_key'] = {deploy_run_key:String}
        AND SpanAttributes['verself.verification_run'] = {verification_run:String}
      FORMAT JSONEachRow
    " || true)"
  if assert_ready "${query_output}"; then
    echo "bazel-smoke-test: verified deploy_id=${VERSELF_DEPLOY_ID} deploy_run_key=${VERSELF_DEPLOY_RUN_KEY}"
    exit 0
  fi
  sleep 1
done

echo "ERROR: timed out waiting for Bazel smoke-test span in default.otel_traces." >&2
printf 'Last query row: %s\n' "${query_output}" >&2
exit 1
