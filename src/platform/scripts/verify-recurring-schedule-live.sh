#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-recurring-schedule-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/recurring-schedule-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

interval_seconds="${RECURRING_SCHEDULE_INTERVAL_SECONDS:-15}"
paused_probe_seconds="${RECURRING_SCHEDULE_PAUSED_PROBE_SECONDS:-20}"
dispatch_timeout_seconds="${RECURRING_SCHEDULE_DISPATCH_TIMEOUT_SECONDS:-180}"
clickhouse_timeout_seconds="${RECURRING_SCHEDULE_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
proof_persona="${RECURRING_SCHEDULE_PERSONA:-platform-admin}"
workflow_path=".forgejo/workflows/recurring-proof.yml"
sandbox_api_base_url="${BASE_URL:-https://sandbox.api.${VERIFICATION_DOMAIN}}"
sandbox_api_base_url="${sandbox_api_base_url%/}"
source_api_base_url="${SOURCE_CODE_HOSTING_PROOF_BASE_URL:-https://source.api.${VERIFICATION_DOMAIN}}"
source_api_base_url="${source_api_base_url%/}"
projects_api_base_url="${PROJECTS_PROOF_BASE_URL:-https://projects.api.${VERIFICATION_DOMAIN}}"
projects_api_base_url="${projects_api_base_url%/}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
sandbox_service_spiffe_id="spiffe://${trust_domain}/svc/sandbox-rental-service"

case "${proof_persona}" in
  platform-admin)
    proof_billing_email="ceo@${VERIFICATION_DOMAIN}"
    proof_billing_org="platform"
    ;;
  acme-admin | acme-member)
    proof_billing_email="acme-admin@${VERIFICATION_DOMAIN}"
    proof_billing_org="Acme Corp"
    ;;
  *)
    echo "unsupported RECURRING_SCHEDULE_PERSONA=${proof_persona}" >&2
    exit 1
    ;;
esac

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" "${proof_persona}" --print)
source_access_token="${SOURCE_CODE_HOSTING_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"
projects_access_token="${PROJECTS_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"

billing_fixture_path="${artifact_dir}/billing-fixture.json"
"${script_dir}/set-user-state.sh" \
  --email "${proof_billing_email}" \
  --org "${proof_billing_org}" \
  --product-id "sandbox" \
  --state "pro" \
  --balance-units "500000000000" >"${billing_fixture_path}"

org_id="$(
  python3 - "${billing_fixture_path}" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["org_id"])
PY
)"

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

sandbox_api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}"
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
  curl "${curl_args[@]}" "${sandbox_api_base_url}${path}" >"${output_path}"
}

source_api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${source_access_token}"
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
  curl "${curl_args[@]}" "${source_api_base_url}${path}" >"${output_path}"
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

create_forgejo_workflow_file() {
  local provider_owner="$1"
  local provider_repo="$2"
  local request_b64
  request_b64="$(
    RUN_ID="${run_id}" WORKFLOW_PATH="${workflow_path}" PROVIDER_OWNER="${provider_owner}" PROVIDER_REPO="${provider_repo}" python3 - <<'PY'
import base64
import json
import os

content = f"""name: Recurring Proof
on:
  workflow_dispatch:
    inputs:
      verification_run_id:
        description: Verification run id
        required: true
        type: string
jobs:
  proof:
    runs-on: docker
    steps:
      - run: echo verself-recurring-proof run_id=${{ inputs.verification_run_id }}
"""
payload = {
    "owner": os.environ["PROVIDER_OWNER"],
    "repo": os.environ["PROVIDER_REPO"],
    "path": os.environ["WORKFLOW_PATH"],
    "content_b64": base64.b64encode(content.encode()).decode(),
}
print(base64.b64encode(json.dumps(payload).encode()).decode())
PY
  )"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64
import json
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request

payload_in = json.loads(base64.b64decode(sys.stdin.readline()).decode())
owner = payload_in[\"owner\"]
repo = payload_in[\"repo\"]
path = payload_in[\"path\"]
content_b64 = payload_in[\"content_b64\"]
token = subprocess.check_output(
    [\"sudo\", \"cat\", \"/etc/credstore/forgejo/automation-token\"],
    text=True,
).strip()
url = \"http://127.0.0.1:3000/api/v1/repos/{}/{}/contents/{}\".format(
    urllib.parse.quote(owner, safe=\"\"),
    urllib.parse.quote(repo, safe=\"\"),
    urllib.parse.quote(path, safe=\"/\"),
)
payload = json.dumps({
    \"branch\": \"main\",
    \"content\": content_b64,
    \"message\": \"Add recurring schedule proof workflow\",
}).encode()
request = urllib.request.Request(
    url,
    data=payload,
    method=\"POST\",
    headers={
        \"Authorization\": \"token \" + token,
        \"Content-Type\": \"application/json\",
        \"Accept\": \"application/json\",
    },
)
try:
    with urllib.request.urlopen(request, timeout=5) as response:
        status = response.status
        response.read()
except urllib.error.HTTPError as error:
    body = error.read().decode(errors=\"replace\")
    if error.code != 409:
        sys.stderr.write(body)
        raise
    status = error.code
json.dump({\"status\": status, \"path\": path}, sys.stdout, sort_keys=True)
print()
'"
}

verification_wait_for_loopback_api "sandbox-rental-service" \
  "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"
verification_wait_for_loopback_api "source-code-hosting-service" \
  "http://127.0.0.1:4261/readyz" "200"

project_slug="recurring-proof-$(printf '%s' "${run_id}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '-' | sed -E 's/^-+|-+$//g' | cut -c1-48)"
if [[ -z "${project_slug}" ]]; then
  project_slug="recurring-proof"
fi
project_payload_path="${artifact_dir}/payloads/create-project.json"
python3 - "${run_id}" "${project_slug}" >"${project_payload_path}" <<'PY'
import json
import sys

run_id, project_slug = sys.argv[1:3]
print(json.dumps({
    "display_name": f"Recurring Proof {run_id}",
    "slug": project_slug,
    "description": "Recurring schedule proof project",
}, indent=2, sort_keys=True))
PY
curl -fsS \
  -X POST \
  -H "Authorization: Bearer ${projects_access_token}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: recurring-project:${run_id}" \
  -H "baggage: verself.verification_run=${run_id}" \
  --data-binary "@${project_payload_path}" \
  "${projects_api_base_url}/api/v1/projects" >"${artifact_dir}/responses/create-project.json"
project_id="$(
  python3 - "${artifact_dir}/responses/create-project.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["project_id"])
PY
)"

source_repo_payload_path="${artifact_dir}/payloads/create-source-repo.json"
python3 - "${run_id}" "${project_id}" >"${source_repo_payload_path}" <<'PY'
import json
import sys

run_id, project_id = sys.argv[1:3]
print(json.dumps({
    "project_id": project_id,
    "name": f"Recurring Proof {run_id}",
    "description": "Recurring schedule workflow dispatch proof",
    "default_branch": "main",
}, indent=2, sort_keys=True))
PY
source_api_request "POST" "/api/v1/repos" "${artifact_dir}/responses/create-source-repo.json" "${source_repo_payload_path}" "recurring-source-repo:${run_id}"
source_repo_id="$(
  python3 - "${artifact_dir}/responses/create-source-repo.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["repo_id"])
PY
)"

escaped_source_repo_id="${source_repo_id//\'/\'\'}"
remote_psql source_code_hosting "
SELECT r.repo_id, r.org_id, r.project_id, b.backend, b.backend_owner, b.backend_repo, b.backend_repo_id
FROM source_repositories r
JOIN source_repository_backends b ON b.repo_id = r.repo_id
WHERE r.repo_id = '${escaped_source_repo_id}'::uuid
  AND b.backend = 'forgejo';
" >"${artifact_dir}/postgres/source_repository.tsv"
read -r _source_repo_id _source_org_id _source_project_id provider provider_owner provider_repo provider_repo_id <"${artifact_dir}/postgres/source_repository.tsv"
if [[ "${_source_org_id}" != "${org_id}" || "${_source_project_id}" != "${project_id}" || "${provider}" != "forgejo" || -z "${provider_owner}" || -z "${provider_repo}" || -z "${provider_repo_id}" ]]; then
  echo "source repository row did not include expected provider-neutral coordinates" >&2
  exit 1
fi
create_forgejo_workflow_file "${provider_owner}" "${provider_repo}" >"${artifact_dir}/responses/create-forgejo-workflow-file.json"

payload_path="${artifact_dir}/payloads/create-schedule.json"
python3 - "${run_id}" "${project_id}" "${source_repo_id}" "${workflow_path}" "${interval_seconds}" >"${payload_path}" <<'PY'
import json
import sys

run_id, project_id, source_repo_id, workflow_path, interval_seconds = sys.argv[1:6]
print(json.dumps({
    "display_name": f"Recurring proof {run_id}",
    "idempotency_key": run_id,
    "interval_seconds": int(interval_seconds),
    "inputs": {"verification_run_id": run_id},
    "paused": True,
    "project_id": project_id,
    "ref": "main",
    "source_repository_id": source_repo_id,
    "workflow_path": workflow_path,
}, indent=2, sort_keys=True))
PY

sandbox_api_request "POST" "/api/v1/execution-schedules" "${artifact_dir}/responses/create-schedule.json" "${payload_path}"

read -r schedule_id temporal_schedule_id created_state created_project_id created_source_repo_id created_workflow_path <<<"$(
  python3 - "${artifact_dir}/responses/create-schedule.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["schedule_id"], payload["temporal_schedule_id"], payload["state"], payload["project_id"], payload["source_repository_id"], payload["workflow_path"])
PY
)"

if [[ "${created_state}" != "paused" || "${created_project_id}" != "${project_id}" || "${created_source_repo_id}" != "${source_repo_id}" || "${created_workflow_path}" != "${workflow_path}" ]]; then
  echo "created recurring schedule response did not match source workflow request" >&2
  exit 1
fi

sandbox_api_request "GET" "/api/v1/execution-schedules" "${artifact_dir}/responses/list-before-resume.json"
python3 - "${artifact_dir}/responses/list-before-resume.json" "${schedule_id}" <<'PY'
import json
import sys

schedule_id = sys.argv[2]
for item in json.load(open(sys.argv[1], encoding="utf-8")):
    if item.get("schedule_id") == schedule_id:
        if item.get("state") != "paused" or len(item.get("dispatches") or []) != 0:
            raise SystemExit("paused schedule listed with unexpected state or dispatches")
        raise SystemExit(0)
raise SystemExit(f"schedule {schedule_id} missing from list response")
PY

sleep "${paused_probe_seconds}"
sandbox_api_request "GET" "/api/v1/execution-schedules/${schedule_id}" "${artifact_dir}/responses/detail-paused.json"
python3 - "${artifact_dir}/responses/detail-paused.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("state") != "paused":
    raise SystemExit(f"expected paused schedule state, got {payload.get('state')!r}")
if payload.get("dispatches"):
    raise SystemExit("paused recurring schedule dispatched unexpectedly before resume")
PY

sandbox_api_request \
  "POST" \
  "/api/v1/execution-schedules/${schedule_id}/resume" \
  "${artifact_dir}/responses/resume-schedule.json" \
  "" \
  "${run_id}-resume"

resumed_state="$(
  python3 - "${artifact_dir}/responses/resume-schedule.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["state"])
PY
)"
if [[ "${resumed_state}" != "active" ]]; then
  echo "expected resumed recurring schedule to be active, got ${resumed_state}" >&2
  exit 1
fi

dispatch_id=""
source_workflow_run_id=""
temporal_workflow_id=""
temporal_run_id=""
for _ in $(seq 1 $((dispatch_timeout_seconds / 2))); do
  sandbox_api_request "GET" "/api/v1/execution-schedules/${schedule_id}" "${artifact_dir}/responses/detail-active.json"
  dispatch_state="$(
    python3 - "${artifact_dir}/responses/detail-active.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
dispatches = payload.get("dispatches") or []
if not dispatches:
    print("waiting")
    raise SystemExit(0)
dispatch = dispatches[0]
if dispatch.get("state") == "failed":
    reason = dispatch.get("failure_reason") or "unknown"
    raise SystemExit(f"dispatch failed: {reason}")
if dispatch.get("state") == "submitted" and dispatch.get("source_workflow_run_id"):
    print("\t".join([
        "submitted",
        dispatch["dispatch_id"],
        dispatch["source_workflow_run_id"],
        dispatch.get("workflow_state", ""),
        dispatch.get("temporal_workflow_id", ""),
        dispatch.get("temporal_run_id", ""),
    ]))
    raise SystemExit(0)
print("waiting")
PY
  )"
  if [[ "${dispatch_state}" == "waiting" ]]; then
    sleep 2
    continue
  fi
  read -r dispatch_status dispatch_id source_workflow_run_id workflow_state temporal_workflow_id temporal_run_id <<<"${dispatch_state}"
  if [[ "${dispatch_status}" == "submitted" ]]; then
    break
  fi
done

if [[ -z "${source_workflow_run_id}" || -z "${dispatch_id}" || -z "${temporal_workflow_id}" || -z "${temporal_run_id}" ]]; then
  echo "recurring schedule did not dispatch a source workflow in time" >&2
  exit 1
fi
if [[ "${workflow_state}" != "dispatched" ]]; then
  echo "expected source workflow state dispatched, got ${workflow_state}" >&2
  exit 1
fi

sandbox_api_request "GET" "/api/v1/execution-schedules" "${artifact_dir}/responses/list-after-dispatch.json"
python3 - "${artifact_dir}/responses/list-after-dispatch.json" "${schedule_id}" "${source_workflow_run_id}" <<'PY'
import json
import sys

schedule_id, source_workflow_run_id = sys.argv[2:4]
for item in json.load(open(sys.argv[1], encoding="utf-8")):
    if item.get("schedule_id") == schedule_id:
        dispatches = item.get("dispatches") or []
        if not any(dispatch.get("source_workflow_run_id") == source_workflow_run_id for dispatch in dispatches):
            raise SystemExit("schedule list response did not include source workflow dispatch")
        raise SystemExit(0)
raise SystemExit(f"schedule {schedule_id} missing from list response")
PY

sandbox_api_request \
  "POST" \
  "/api/v1/execution-schedules/${schedule_id}/pause" \
  "${artifact_dir}/responses/pause-schedule.json" \
  "" \
  "${run_id}-pause"

paused_cleanup_state="$(
  python3 - "${artifact_dir}/responses/pause-schedule.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["state"])
PY
)"
if [[ "${paused_cleanup_state}" != "paused" ]]; then
  echo "expected recurring schedule cleanup pause to return paused state, got ${paused_cleanup_state}" >&2
  exit 1
fi

source_api_request "GET" "/api/v1/workflow-runs/${source_workflow_run_id}" "${artifact_dir}/responses/source-workflow-run.json"
python3 - "${artifact_dir}/responses/source-workflow-run.json" "${project_id}" "${source_repo_id}" "${workflow_path}" "${run_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
project_id, source_repo_id, workflow_path, run_id = sys.argv[2:6]
if payload.get("project_id") != project_id:
    raise SystemExit("source workflow run project_id mismatch")
if payload.get("repo_id") != source_repo_id:
    raise SystemExit("source workflow run repo_id mismatch")
if payload.get("workflow_path") != workflow_path:
    raise SystemExit("source workflow run workflow_path mismatch")
if payload.get("state") != "dispatched":
    raise SystemExit(f"expected dispatched source workflow run, got {payload.get('state')!r}")
if (payload.get("inputs") or {}).get("verification_run_id") != run_id:
    raise SystemExit("source workflow run inputs missing verification_run_id")
PY

source_api_request "GET" "/api/v1/repos/${source_repo_id}/workflow-runs" "${artifact_dir}/responses/source-workflow-runs-list.json"
python3 - "${artifact_dir}/responses/source-workflow-runs-list.json" "${source_workflow_run_id}" <<'PY'
import json
import sys

workflow_run_id = sys.argv[2]
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if not any(item.get("workflow_run_id") == workflow_run_id for item in payload.get("workflow_runs") or []):
    raise SystemExit("source workflow run list did not include scheduled dispatch")
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
escaped_schedule_id="${schedule_id//\'/\'\'}"
escaped_dispatch_id="${dispatch_id//\'/\'\'}"
escaped_source_workflow_run_id="${source_workflow_run_id//\'/\'\'}"

remote_psql sandbox_rental "
SELECT schedule_id, org_id, state, temporal_schedule_id, temporal_namespace, task_queue,
       interval_seconds, project_id, source_repository_id, workflow_path, ref, inputs_json
FROM execution_schedules
WHERE schedule_id = '${escaped_schedule_id}';
" >"${artifact_dir}/postgres/execution_schedule.tsv"

remote_psql sandbox_rental "
SELECT dispatch_id, schedule_id, temporal_workflow_id, temporal_run_id,
       source_workflow_run_id, project_id, workflow_state, state, failure_reason
FROM execution_schedule_dispatches
WHERE dispatch_id = '${escaped_dispatch_id}';
" >"${artifact_dir}/postgres/execution_schedule_dispatch.tsv"

remote_psql source_code_hosting "
SELECT workflow_run_id, org_id, project_id, repo_id, actor_id, provider, workflow_path, ref, state, failure_reason, trace_id
FROM source_workflow_runs
WHERE workflow_run_id = '${escaped_source_workflow_run_id}'::uuid;
" >"${artifact_dir}/postgres/source_workflow_run.tsv"

remote_psql source_code_hosting "
SELECT event_type, result, trace_id
FROM source_events
WHERE repo_id = '${escaped_source_repo_id}'::uuid
  AND event_type IN ('source.workflow.dispatch.requested', 'source.workflow.dispatched')
ORDER BY created_at, event_type;
" >"${artifact_dir}/postgres/source_workflow_events.tsv"

remote_psql temporal_visibility "
SELECT workflow_id, run_id, workflow_type_name, task_queue, status, start_time, close_time, execution_duration, history_length
FROM executions_visibility
WHERE workflow_id = '${temporal_workflow_id}'
  AND run_id = '${temporal_run_id}';
" >"${artifact_dir}/postgres/temporal_visibility.tsv"

python3 - \
  "${artifact_dir}/postgres/execution_schedule.tsv" \
  "${artifact_dir}/postgres/execution_schedule_dispatch.tsv" \
  "${artifact_dir}/postgres/source_workflow_run.tsv" \
  "${artifact_dir}/postgres/source_workflow_events.tsv" \
  "${artifact_dir}/postgres/temporal_visibility.tsv" \
  "${org_id}" \
  "${project_id}" \
  "${source_repo_id}" \
  "${workflow_path}" \
  "${temporal_schedule_id}" \
  "${source_workflow_run_id}" \
  "${temporal_workflow_id}" \
  "${temporal_run_id}" <<'PY'
import csv
import io
import sys

schedule_path, dispatch_path, workflow_path_tsv, events_path, temporal_path = sys.argv[1:6]
org_id, project_id, source_repo_id, workflow_path, temporal_schedule_id, source_workflow_run_id, temporal_workflow_id, temporal_run_id = sys.argv[6:14]

def one_row(path, label):
    text = open(path, encoding="utf-8").read().rstrip("\r\n")
    if not text:
        raise SystemExit(f"{label} row missing")
    return [cell.strip() for cell in next(csv.reader(io.StringIO(text), delimiter="\t"))]

schedule = one_row(schedule_path, "execution_schedules")
_, row_org_id, state, row_temporal_schedule_id, temporal_namespace, task_queue, interval_seconds, row_project_id, row_source_repo_id, row_workflow_path, ref, inputs_json = schedule
if row_org_id != org_id or row_project_id != project_id or row_temporal_schedule_id != temporal_schedule_id or row_source_repo_id != source_repo_id:
    raise SystemExit("execution_schedules linkage mismatch")
if state != "paused" or temporal_namespace != "sandbox-rental-service" or task_queue != "sandbox-rental-service.recurring-vm":
    raise SystemExit("execution_schedules state/Temporal metadata mismatch")
if row_workflow_path != workflow_path or ref != "main" or int(interval_seconds) < 15 or "verification_run_id" not in inputs_json:
    raise SystemExit("execution_schedules workflow fields mismatch")

dispatch = one_row(dispatch_path, "execution_schedule_dispatches")
_, _, row_temporal_workflow_id, row_temporal_run_id, row_source_workflow_run_id, row_project_id, workflow_state, dispatch_state, failure_reason = dispatch
if row_temporal_workflow_id != temporal_workflow_id or row_temporal_run_id != temporal_run_id:
    raise SystemExit("execution_schedule_dispatches temporal linkage mismatch")
if row_source_workflow_run_id != source_workflow_run_id or row_project_id != project_id or workflow_state != "dispatched" or dispatch_state != "submitted" or failure_reason:
    raise SystemExit("execution_schedule_dispatches source workflow state mismatch")

workflow_run = one_row(workflow_path_tsv, "source_workflow_runs")
row_workflow_run_id, row_workflow_org_id, row_project_id, row_repo_id, _actor_id, provider, row_workflow_path, row_ref, row_state, failure_reason, trace_id = workflow_run
if row_workflow_run_id != source_workflow_run_id or row_workflow_org_id != org_id or row_project_id != project_id or row_repo_id != source_repo_id:
    raise SystemExit("source_workflow_runs linkage mismatch")
if provider != "forgejo" or row_workflow_path != workflow_path or row_ref != "main" or row_state != "dispatched" or failure_reason or not trace_id:
    raise SystemExit("source_workflow_runs state/provider/trace mismatch")

events = {row[0]: row for row in csv.reader(open(events_path, encoding="utf-8"), delimiter="\t") if row}
for event_type in ("source.workflow.dispatch.requested", "source.workflow.dispatched"):
    if event_type not in events:
        raise SystemExit(f"missing {event_type} event")
    if events[event_type][1] != "allowed" or not events[event_type][2]:
        raise SystemExit(f"{event_type} did not have allowed result and trace id")

temporal = one_row(temporal_path, "temporal visibility")
if temporal[0] != temporal_workflow_id or temporal[1] != temporal_run_id:
    raise SystemExit("temporal visibility linkage mismatch")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.execution_schedule.create'
    AND SpanAttributes['sandbox.schedule_id'] = {schedule_id:String}
    AND SpanAttributes['verself.project_id'] = {project_id:String}
" 1 "${artifact_dir}/clickhouse/create-span-count.tsv" --param_schedule_id="${schedule_id}" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {sandbox_service_spiffe_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-mtls-count.tsv" --param_sandbox_service_spiffe_id="${sandbox_service_spiffe_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['temporal.namespace'] = 'sandbox-rental-service'
    AND SpanAttributes['temporal.authz.decision'] = 'allow'
" 1 "${artifact_dir}/clickhouse/temporal-authz-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'sandbox-rental-recurring-worker'
    AND SpanName = 'sandbox-rental.execution_schedule.dispatch.submit'
    AND SpanAttributes['sandbox.dispatch_id'] = {dispatch_id:String}
    AND SpanAttributes['source.workflow_run_id'] = {source_workflow_run_id:String}
    AND SpanAttributes['verself.project_id'] = {project_id:String}
" 1 "${artifact_dir}/clickhouse/dispatch-span-count.tsv" --param_dispatch_id="${dispatch_id}" --param_source_workflow_run_id="${source_workflow_run_id}" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'sandbox-rental-recurring-worker'
    AND SpanName = 'sandbox-rental.source.workflow.dispatch'
    AND SpanAttributes['source.workflow_run_id'] = {source_workflow_run_id:String}
    AND SpanAttributes['verself.project_id'] = {project_id:String}
" 1 "${artifact_dir}/clickhouse/source-dispatch-client-span-count.tsv" --param_source_workflow_run_id="${source_workflow_run_id}" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.workflow.dispatch', 'source.forgejo.workflow.dispatch', 'source.pg.workflow_run.create')
    AND SpanAttributes['source.workflow_run_id'] = {source_workflow_run_id:String}
    AND (SpanAttributes['verself.project_id'] = '' OR SpanAttributes['verself.project_id'] = {project_id:String})
" 3 "${artifact_dir}/clickhouse/source-workflow-span-count.tsv" --param_source_workflow_run_id="${source_workflow_run_id}" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND (
      (ServiceName = 'source-code-hosting-service' AND SpanName = 'source.projects.resolve' AND SpanAttributes['verself.project_id'] = {project_id:String})
      OR (ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'projects-service') > 0)
      OR (ServiceName = 'projects-service' AND SpanName IN ('auth.spiffe.mtls.server', 'projects.project.resolve', 'projects.pg.project.resolve'))
    )
" 4 "${artifact_dir}/clickhouse/source-projects-boundary-spans-count.tsv" --param_project_id="${project_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {sandbox_service_spiffe_id:String}
" 1 "${artifact_dir}/clickhouse/source-internal-mtls-count.tsv" --param_sandbox_service_spiffe_id="${sandbox_service_spiffe_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_project_id="${project_id}" \
    --param_schedule_id="${schedule_id}" \
    --param_source_workflow_run_id="${source_workflow_run_id}" \
    --param_sandbox_service_spiffe_id="${sandbox_service_spiffe_id}" \
    --query "
      SELECT
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId,
        SpanAttributes['sandbox.schedule_id'] AS schedule_id,
        SpanAttributes['sandbox.dispatch_id'] AS dispatch_id,
        SpanAttributes['verself.project_id'] AS project_id,
        SpanAttributes['source.workflow_run_id'] AS source_workflow_run_id,
        SpanAttributes['source.workflow_path'] AS source_workflow_path,
        SpanAttributes['temporal.schedule_id'] AS temporal_schedule_id,
        SpanAttributes['spiffe.peer_id'] AS spiffe_peer_id
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND (
          SpanAttributes['sandbox.schedule_id'] = {schedule_id:String}
          OR SpanAttributes['verself.project_id'] = {project_id:String}
          OR SpanAttributes['source.workflow_run_id'] = {source_workflow_run_id:String}
          OR SpanAttributes['spiffe.peer_id'] = {sandbox_service_spiffe_id:String}
        )
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/recurring_schedule_spans.tsv"

run_id="${run_id}" \
org_id="${org_id}" \
project_id="${project_id}" \
source_repo_id="${source_repo_id}" \
source_workflow_run_id="${source_workflow_run_id}" \
schedule_id="${schedule_id}" \
dispatch_id="${dispatch_id}" \
temporal_schedule_id="${temporal_schedule_id}" \
temporal_workflow_id="${temporal_workflow_id}" \
temporal_run_id="${temporal_run_id}" \
window_start="${window_start}" \
window_end="${window_end}" \
artifact_dir="${artifact_dir}" \
python3 - <<'PY' >"${artifact_dir}/run.json"
import json
import os

print(json.dumps({
    "artifact_dir": os.environ["artifact_dir"],
    "dispatch_id": os.environ["dispatch_id"],
    "org_id": int(os.environ["org_id"]),
    "project_id": os.environ["project_id"],
    "schedule_id": os.environ["schedule_id"],
    "source_repo_id": os.environ["source_repo_id"],
    "source_workflow_run_id": os.environ["source_workflow_run_id"],
    "temporal_run_id": os.environ["temporal_run_id"],
    "temporal_schedule_id": os.environ["temporal_schedule_id"],
    "temporal_workflow_id": os.environ["temporal_workflow_id"],
    "verification_run_id": os.environ["run_id"],
    "window_end": os.environ["window_end"],
    "window_start": os.environ["window_start"],
}, indent=2, sort_keys=True))
PY

printf 'recurring schedule proof passed: run_id=%s schedule_id=%s source_workflow_run_id=%s artifacts=%s\n' \
  "${run_id}" "${schedule_id}" "${source_workflow_run_id}" "${artifact_dir}"
