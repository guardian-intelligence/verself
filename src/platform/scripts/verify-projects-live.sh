#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-projects-smoke-test-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/projects-smoke-test}"
artifact_dir="${artifact_root}/${run_id}"
projects_api_base_url="${PROJECTS_SMOKE_TEST_BASE_URL:-https://projects.api.${VERIFICATION_DOMAIN}}"
projects_api_base_url="${projects_api_base_url%/}"
projects_loopback_addr="$(
  python3 - "${VERIFICATION_GENERATED_VARS_FILE%/*}/endpoints.yml" <<'PY'
import sys
import yaml

with open(sys.argv[1], encoding="utf-8") as handle:
    endpoints = yaml.safe_load(handle)["topology_endpoints"]
address = endpoints["projects_service"]["endpoints"]["public_http"]["address"]
if not str(address).strip():
    raise SystemExit("projects_service public_http address missing from generated endpoints.yml")
print(address)
PY
)"
clickhouse_timeout_seconds="${PROJECTS_SMOKE_TEST_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

remote_psql() {
  local db="$1"
  local sql="$2"
  local output_path="$3"
  local request_b64
  request_b64="$(
    DB="${db}" SQL="${sql}" python3 - <<'PY'
import base64
import json
import os

print(base64.b64encode(json.dumps({
    "db": os.environ["DB"],
    "sql": os.environ["SQL"],
}).encode()).decode())
PY
  )"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64
import json
import subprocess
import sys

payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
cmd = [
    \"sudo\", \"-u\", \"postgres\", \"psql\",
    \"-d\", payload[\"db\"],
    \"-X\", \"-A\", \"-t\", \"-F\", \"\\t\", \"-P\", \"footer=off\",
    \"-c\", payload[\"sql\"],
]
result = subprocess.run(cmd, check=False, capture_output=True, text=True)
if result.returncode != 0:
    sys.stderr.write(result.stderr)
    raise SystemExit(result.returncode)
sys.stdout.write(result.stdout)
'" >"${output_path}"
}

api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${projects_access_token}"
    -H "Accept: application/json"
    -H "baggage: verself.verification_run=${run_id}"
  )
  if [[ -n "${body_path}" ]]; then
    curl_args+=(
      -H "Content-Type: application/json"
      --data-binary "@${body_path}"
    )
  fi
  if [[ -n "${idempotency_key}" ]]; then
    curl_args+=(-H "Idempotency-Key: ${idempotency_key}")
  fi
  curl "${curl_args[@]}" "${projects_api_base_url}${path}" >"${output_path}"
}

api_request_expect_status() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local expected_status="$4"
  local body_path="${5:-}"
  local idempotency_key="${6:-}"
  local curl_args=(
    -sS
    -o "${output_path}"
    -w "%{http_code}"
    -X "${method}"
    -H "Authorization: Bearer ${projects_access_token}"
    -H "Accept: application/json"
    -H "baggage: verself.verification_run=${run_id}"
  )
  if [[ -n "${body_path}" ]]; then
    curl_args+=(
      -H "Content-Type: application/json"
      --data-binary "@${body_path}"
    )
  fi
  if [[ -n "${idempotency_key}" ]]; then
    curl_args+=(-H "Idempotency-Key: ${idempotency_key}")
  fi
  local status
  status="$(curl "${curl_args[@]}" "${projects_api_base_url}${path}")"
  if [[ "${status}" != "${expected_status}" ]]; then
    echo "unexpected HTTP status for ${method} ${path}: got ${status}, expected ${expected_status}" >&2
    return 1
  fi
}

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  shift 4
  local extra_args=("$@")
  local count="0"
  local attempts=$((clickhouse_timeout_seconds / 2))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    (
      cd "${VERIFICATION_PLATFORM_ROOT}"
      ./scripts/clickhouse.sh \
        --database "${database}" \
        --param_window_start="${window_start}" \
        --param_window_end="${window_end}" \
        "${extra_args[@]}" \
        --query "${query}"
    ) >"${output_path}"
    count="$(tail -n 1 "${output_path}" | tr -d '[:space:]')"
    if [[ "${count}" =~ ^[0-9]+$ ]] && (( count >= min_count )); then
      return 0
    fi
    sleep 2
  done
  echo "ClickHouse assertion failed for ${output_path}: got ${count}, expected >= ${min_count}" >&2
  return 1
}

verification_print_artifacts "${artifact_dir}" "" "${artifact_dir}/run.json"
verification_wait_for_loopback_api "projects-service" "http://${projects_loopback_addr}/readyz" "200"
verification_wait_for_http "projects API auth boundary" "${projects_api_base_url}/api/v1/projects" "401"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
projects_access_token="${PROJECTS_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

project_slug="projects-smoke-test-$(printf '%s' "${run_id}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '-' | sed -E 's/^-+|-+$//g' | cut -c1-48)"
if [[ -z "${project_slug}" ]]; then
  project_slug="projects-smoke-test"
fi

cat >"${artifact_dir}/payloads/create-project.json" <<EOF
{
  "display_name": "Projects Smoke Test ${run_id}",
  "slug": "${project_slug}",
  "description": "Projects service live smoke test"
}
EOF
api_request "POST" "/api/v1/projects" "${artifact_dir}/responses/create-project.json" "${artifact_dir}/payloads/create-project.json" "projects-smoke-test:${run_id}"
read -r project_id project_version org_id <<<"$(
  python3 - "${artifact_dir}/responses/create-project.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["project_id"], payload["version"], payload["org_id"])
PY
)"

api_request "POST" "/api/v1/projects" "${artifact_dir}/responses/create-project-retry.json" "${artifact_dir}/payloads/create-project.json" "projects-smoke-test:${run_id}"
python3 - "${artifact_dir}/responses/create-project-retry.json" "${project_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["project_id"] != sys.argv[2] or str(payload["version"]) != "1":
    raise SystemExit("project create idempotency retry did not return the original project")
PY

cat >"${artifact_dir}/payloads/create-project-conflict.json" <<EOF
{
  "display_name": "Projects Smoke Test Conflict ${run_id}",
  "slug": "${project_slug}-conflict",
  "description": "Projects service conflicting idempotency smoke test"
}
EOF
api_request_expect_status "POST" "/api/v1/projects" "${artifact_dir}/responses/create-project-conflict.json" "409" "${artifact_dir}/payloads/create-project-conflict.json" "projects-smoke-test:${run_id}"

api_request "GET" "/api/v1/projects" "${artifact_dir}/responses/list-projects.json"
python3 - "${artifact_dir}/responses/list-projects.json" "${project_id}" <<'PY'
import json
import sys

project_id = sys.argv[2]
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if not any(project.get("project_id") == project_id for project in payload.get("projects") or []):
    raise SystemExit("created project missing from active list")
PY

cat >"${artifact_dir}/payloads/update-project.json" <<EOF
{
  "version": "${project_version}",
  "display_name": "Projects Smoke Test Updated ${run_id}"
}
EOF
api_request "PATCH" "/api/v1/projects/${project_id}" "${artifact_dir}/responses/update-project.json" "${artifact_dir}/payloads/update-project.json" "projects-update:${run_id}"
project_version="$(
  python3 - "${artifact_dir}/responses/update-project.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["description"] != "Projects service live smoke test":
    raise SystemExit("partial project PATCH cleared the existing description")
print(payload["version"])
PY
)"

cat >"${artifact_dir}/payloads/create-environment.json" <<'EOF'
{
  "display_name": "QA",
  "slug": "qa",
  "kind": "custom",
  "protection_policy": {
    "approval": "required"
  }
}
EOF
api_request "POST" "/api/v1/projects/${project_id}/environments" "${artifact_dir}/responses/create-environment.json" "${artifact_dir}/payloads/create-environment.json" "projects-env:${run_id}"
read -r environment_id environment_version <<<"$(
  python3 - "${artifact_dir}/responses/create-environment.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["environment_id"], payload["version"])
PY
)"

cat >"${artifact_dir}/payloads/archive-environment.json" <<EOF
{
  "version": "${environment_version}"
}
EOF
api_request "POST" "/api/v1/projects/${project_id}/environments/${environment_id}/archive" "${artifact_dir}/responses/archive-environment.json" "${artifact_dir}/payloads/archive-environment.json" "projects-env-archive:${run_id}"

api_request "GET" "/api/v1/projects/${project_id}" "${artifact_dir}/responses/get-project-before-archive.json"
project_version="$(
  python3 - "${artifact_dir}/responses/get-project-before-archive.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["version"])
PY
)"

cat >"${artifact_dir}/payloads/archive-project.json" <<EOF
{
  "version": "${project_version}"
}
EOF
api_request "POST" "/api/v1/projects/${project_id}/archive" "${artifact_dir}/responses/archive-project.json" "${artifact_dir}/payloads/archive-project.json" "projects-archive:${run_id}"
archive_version="$(
  python3 - "${artifact_dir}/responses/archive-project.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["state"] != "archived":
    raise SystemExit("archive project did not return archived state")
print(payload["version"])
PY
)"

api_request "POST" "/api/v1/projects/${project_id}/archive" "${artifact_dir}/responses/archive-project-retry.json" "${artifact_dir}/payloads/archive-project.json" "projects-archive:${run_id}"
python3 - "${artifact_dir}/responses/archive-project-retry.json" "${project_id}" "${archive_version}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["project_id"] != sys.argv[2] or payload["state"] != "archived" or str(payload["version"]) != sys.argv[3]:
    raise SystemExit("project archive idempotency retry did not return the archived project snapshot")
PY

cat >"${artifact_dir}/payloads/archive-project-duplicate.json" <<EOF
{
  "version": "${archive_version}"
}
EOF
api_request_expect_status "POST" "/api/v1/projects/${project_id}/archive" "${artifact_dir}/responses/archive-project-duplicate.json" "409" "${artifact_dir}/payloads/archive-project-duplicate.json" "projects-archive-duplicate:${run_id}"

cat >"${artifact_dir}/payloads/restore-project.json" <<EOF
{
  "version": "${archive_version}"
}
EOF
api_request "POST" "/api/v1/projects/${project_id}/restore" "${artifact_dir}/responses/restore-project.json" "${artifact_dir}/payloads/restore-project.json" "projects-restore:${run_id}"
python3 - "${artifact_dir}/responses/restore-project.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["state"] != "active":
    raise SystemExit("restore project did not return active state")
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
escaped_project_id="${project_id//\'/\'\'}"
escaped_environment_id="${environment_id//\'/\'\'}"

remote_psql projects_service "
SELECT project_id, org_id, slug, display_name, state, version
FROM projects
WHERE project_id = '${escaped_project_id}'::uuid
  AND state = 'active';
" "${artifact_dir}/postgres/project.tsv"

remote_psql projects_service "
SELECT environment_id, project_id, slug, display_name, kind, state, protection_policy
FROM project_environments
WHERE environment_id = '${escaped_environment_id}'::uuid
  AND project_id = '${escaped_project_id}'::uuid
  AND state = 'archived';
" "${artifact_dir}/postgres/environment.tsv"

remote_psql projects_service "
SELECT event_type, project_id, COALESCE(environment_id, '00000000-0000-0000-0000-000000000000'::uuid), trace_id
FROM project_events
WHERE project_id = '${escaped_project_id}'::uuid
ORDER BY created_at, event_type;
" "${artifact_dir}/postgres/project-events.tsv"

remote_psql projects_service "
SELECT operation, result_kind, result_project_id, COALESCE(result_environment_id, '00000000-0000-0000-0000-000000000000'::uuid)
FROM project_idempotency_records
WHERE result_project_id = '${escaped_project_id}'::uuid
ORDER BY operation;
" "${artifact_dir}/postgres/idempotency-records.tsv"

python3 - \
  "${artifact_dir}/postgres/project.tsv" \
  "${artifact_dir}/postgres/environment.tsv" \
  "${artifact_dir}/postgres/project-events.tsv" \
  "${artifact_dir}/postgres/idempotency-records.tsv" \
  "${project_id}" \
  "${environment_id}" \
  "${org_id}" <<'PY'
import csv
import sys

project_path, environment_path, events_path, idempotency_path, project_id, environment_id, org_id = sys.argv[1:8]
project_rows = list(csv.reader(open(project_path, encoding="utf-8"), delimiter="\t"))
if len(project_rows) != 1 or project_rows[0][0] != project_id or project_rows[0][1] != org_id or project_rows[0][4] != "active":
    raise SystemExit("projects table did not contain restored active project")
environment_rows = list(csv.reader(open(environment_path, encoding="utf-8"), delimiter="\t"))
if len(environment_rows) != 1 or environment_rows[0][0] != environment_id or environment_rows[0][1] != project_id or environment_rows[0][5] != "archived":
    raise SystemExit("project_environments table did not contain archived environment")
events = {row[0] for row in csv.reader(open(events_path, encoding="utf-8"), delimiter="\t") if row}
required = {
    "project.created",
    "project.updated",
    "project.environment.created",
    "project.environment.archived",
    "project.archived",
    "project.restored",
}
missing = required - events
if missing:
    raise SystemExit("project_events missing: " + ", ".join(sorted(missing)))
idempotency_rows = list(csv.reader(open(idempotency_path, encoding="utf-8"), delimiter="\t"))
operations = {row[0] for row in idempotency_rows if row}
required_operations = {
    "environment.create",
    "project.archived",
    "project.create",
    "project.update",
    "project.environment.archived",
    "project.restored",
}
missing_operations = required_operations - operations
if missing_operations:
    raise SystemExit("project_idempotency_records missing: " + ", ".join(sorted(missing_operations)))
for operation, result_kind, row_project_id, row_environment_id in idempotency_rows:
    if row_project_id != project_id:
        raise SystemExit("project idempotency record project attribution mismatch")
    if operation.startswith("project.environment") or operation == "environment.create":
        if result_kind != "environment" or row_environment_id != environment_id:
            raise SystemExit("environment idempotency record attribution mismatch")
    elif result_kind != "project":
        raise SystemExit("project idempotency record kind mismatch")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'projects-service'
    AND SpanName IN (
      'projects.project.create',
      'projects.project.list',
      'projects.project.read',
      'projects.project.update',
      'projects.environment.create',
      'projects.environment.archive',
      'projects.project.archive',
      'projects.project.restore'
    )
    AND (SpanAttributes['verself.project_id'] = '' OR SpanAttributes['verself.project_id'] = {project_id:String})
" 8 "${artifact_dir}/clickhouse/projects-api-spans-count.tsv" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'projects-service'
    AND SpanName IN (
      'projects.pg.project.create',
      'projects.pg.project.list',
      'projects.pg.project.get',
      'projects.pg.project.update',
      'projects.pg.environment.create',
      'projects.pg.environment.lifecycle',
      'projects.pg.project.lifecycle'
    )
    AND (SpanAttributes['verself.project_id'] = '' OR SpanAttributes['verself.project_id'] = {project_id:String})
" 7 "${artifact_dir}/clickhouse/projects-pg-spans-count.tsv" --param_project_id="${project_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_project_id="${project_id}" \
    --query "
      SELECT *
      FROM (
        SELECT
          Timestamp,
          ServiceName,
          SpanName,
          TraceId,
          SpanId,
          ParentSpanId,
          SpanAttributes['projects.operation_id'] AS operation_id,
          SpanAttributes['projects.outcome'] AS outcome,
          SpanAttributes['verself.project_id'] AS project_id
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
          AND ServiceName = 'projects-service'
      )
      WHERE project_id = '' OR project_id = {project_id:String}
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/projects-traces.tsv"

run_id="${run_id}" \
org_id="${org_id}" \
project_id="${project_id}" \
environment_id="${environment_id}" \
window_start="${window_start}" \
window_end="${window_end}" \
artifact_dir="${artifact_dir}" \
python3 - <<'PY' >"${artifact_dir}/run.json"
import json
import os

print(json.dumps({
    "artifact_dir": os.environ["artifact_dir"],
    "environment_id": os.environ["environment_id"],
    "org_id": os.environ["org_id"],
    "project_id": os.environ["project_id"],
    "verification_run_id": os.environ["run_id"],
    "window_end": os.environ["window_end"],
    "window_start": os.environ["window_start"],
}, indent=2, sort_keys=True))
PY

printf 'projects smoke test passed: run_id=%s project_id=%s environment_id=%s artifacts=%s\n' \
  "${run_id}" "${project_id}" "${environment_id}" "${artifact_dir}"
