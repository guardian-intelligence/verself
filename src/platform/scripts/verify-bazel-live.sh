#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "$0")" && pwd)"
platform_root="$(cd "${script_dir}/.." && pwd)"
repo_root="$(git -C "${platform_root}" rev-parse --show-toplevel)"

cd "${platform_root}"

inventory="ansible/inventory/${VERSELF_SITE:-prod}.ini"
if [[ ! -f "${inventory}" ]]; then
  echo "ERROR: ${inventory} not found. Provision the environment first." >&2
  exit 1
fi

remote_host="$(grep -m1 'ansible_host=' "${inventory}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
remote_user="$(grep -m1 'ansible_user=' "${inventory}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
if [[ -z "${remote_host}" || -z "${remote_user}" ]]; then
  echo "ERROR: could not resolve ansible_host/ansible_user from ${inventory}." >&2
  exit 1
fi

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
ssh -N \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=15 \
  -o ServerAliveCountMax=3 \
  -o StrictHostKeyChecking=no \
  -L "${port}:127.0.0.1:4317" \
  "${remote_user}@${remote_host}" </dev/null >/dev/null 2>&1 &
tunnel_pid=$!
trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' EXIT

for _ in $(seq 1 20); do
  if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
    break
  fi
  sleep 0.25
done
if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
  echo "ERROR: OTLP tunnel to ${remote_user}@${remote_host} did not come up on 127.0.0.1:${port}." >&2
  exit 1
fi

export VERSELF_OTLP_ENDPOINT="127.0.0.1:${port}"
export VERSELF_DEPLOY_KIND="bazel-smoke-test"
# shellcheck source=src/platform/scripts/deploy_identity.sh
source "${script_dir}/deploy_identity.sh"
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
