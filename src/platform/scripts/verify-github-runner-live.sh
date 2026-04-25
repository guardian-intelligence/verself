#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-github-runner-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/github-runner-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

proof_persona="${GITHUB_RUNNER_PROOF_PERSONA:-platform-admin}"
github_repository="${GITHUB_RUNNER_PROOF_REPOSITORY:-guardian-intelligence/forge-metal}"
github_workflow_file="${GITHUB_RUNNER_PROOF_WORKFLOW_FILE:-forge-metal-runner-canary.yml}"
github_workflow_name="${GITHUB_RUNNER_PROOF_WORKFLOW_NAME:-Forge Metal runner canary}"
github_workflow_ref="${GITHUB_RUNNER_PROOF_REF:-main}"
github_app_slug="${GITHUB_RUNNER_PROOF_APP_SLUG:-forge-metal-ci}"
github_app_id="${GITHUB_RUNNER_PROOF_APP_ID:-$(awk -F': *' '/^sandbox_rental_service_github_app_id:/{print $2}' "${VERIFICATION_VARS_FILE}" | tr -d '\"' | tail -n 1)}"
api_base_url="${BASE_URL:-https://sandbox.api.${VERIFICATION_DOMAIN}}"
api_base_url="${api_base_url%/}"
expected_github_webhook_url="${api_base_url}/webhooks/github/actions"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
clickhouse_timeout_seconds="${GITHUB_RUNNER_PROOF_CLICKHOUSE_TIMEOUT_SECONDS:-240}"
github_poll_timeout_seconds="${GITHUB_RUNNER_PROOF_GITHUB_TIMEOUT_SECONDS:-900}"
service_poll_timeout_seconds="${GITHUB_RUNNER_PROOF_SERVICE_TIMEOUT_SECONDS:-600}"

github_org="${github_repository%%/*}"
github_repo_name="${github_repository##*/}"

case "${proof_persona}" in
  platform-admin)
    proof_billing_email="ceo@${VERIFICATION_DOMAIN}"
    proof_billing_org="platform"
    ;;
  *)
    echo "unsupported GITHUB_RUNNER_PROOF_PERSONA=${proof_persona}" >&2
    exit 1
    ;;
esac

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" "${proof_persona}" --print)

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

api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${SANDBOX_RENTAL_TOKEN}"
    -H "baggage: forge_metal.verification_run=${run_id}"
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
  curl "${curl_args[@]}" "${api_base_url}${path}" >"${output_path}"
}

github_api() {
  gh api "$@"
}

assert_github_app_configuration() {
  local app_output_path="${artifact_dir}/responses/github-app.json"
  local hook_output_path="${artifact_dir}/responses/github-app-hook-config.json"
  if [[ -z "${github_app_id}" || "${github_app_id}" == "0" ]]; then
    echo "github app id is not configured" >&2
    return 1
  fi
  SECRETS_SERVICE_TOKEN="${SECRETS_SERVICE_TOKEN}" \
    FORGE_METAL_DOMAIN="${FORGE_METAL_DOMAIN}" \
    GITHUB_APP_ID="${github_app_id}" \
    GITHUB_APP_SLUG="${github_app_slug}" \
    EXPECTED_GITHUB_WEBHOOK_URL="${expected_github_webhook_url}" \
    GITHUB_APP_OUTPUT_PATH="${app_output_path}" \
    GITHUB_APP_HOOK_OUTPUT_PATH="${hook_output_path}" \
    python3 - <<'PY'
import json
import os
import time
import urllib.error
import urllib.request

import jwt

secret_name = "sandbox-rental-service.github.private_key"
secrets_url = f"https://secrets.api.{os.environ['FORGE_METAL_DOMAIN']}/api/v1/secrets/{secret_name}"
secret_req = urllib.request.Request(
    secrets_url,
    headers={"Authorization": "Bearer " + os.environ["SECRETS_SERVICE_TOKEN"]},
)
with urllib.request.urlopen(secret_req, timeout=10) as resp:
    private_key = json.load(resp)["value"]

now = int(time.time())
app_jwt = jwt.encode(
    {"iat": now - 60, "exp": now + 9 * 60, "iss": os.environ["GITHUB_APP_ID"]},
    private_key,
    algorithm="RS256",
)
headers = {
    "Authorization": "Bearer " + app_jwt,
    "Accept": "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "forge-metal-verification",
}

def github(path):
    req = urllib.request.Request("https://api.github.com" + path, headers=headers)
    with urllib.request.urlopen(req, timeout=20) as resp:
        return json.load(resp)

app = github("/app")
hook = github("/app/hook/config")
sanitized_hook = dict(hook)
if "secret" in sanitized_hook:
    sanitized_hook["secret"] = "********"
with open(os.environ["GITHUB_APP_OUTPUT_PATH"], "w", encoding="utf-8") as fh:
    json.dump(
        {
            "id": app.get("id"),
            "slug": app.get("slug"),
            "name": app.get("name"),
            "events": app.get("events"),
            "permissions": app.get("permissions"),
            "html_url": app.get("html_url"),
        },
        fh,
        indent=2,
        sort_keys=True,
    )
    fh.write("\n")
with open(os.environ["GITHUB_APP_HOOK_OUTPUT_PATH"], "w", encoding="utf-8") as fh:
    json.dump(sanitized_hook, fh, indent=2, sort_keys=True)
    fh.write("\n")

errors = []
if app.get("slug") != os.environ["GITHUB_APP_SLUG"]:
    errors.append(f"expected GitHub App slug {os.environ['GITHUB_APP_SLUG']!r}, got {app.get('slug')!r}")
if "workflow_job" not in (app.get("events") or []):
    errors.append("GitHub App is not subscribed to workflow_job events")
permissions = app.get("permissions") or {}
if permissions.get("organization_self_hosted_runners") != "write":
    errors.append("GitHub App needs organization self-hosted runners write permission")
if permissions.get("actions") != "read":
    errors.append("GitHub App needs actions read permission")
if permissions.get("contents") != "read":
    errors.append("GitHub App needs contents read permission")
if hook.get("url") != os.environ["EXPECTED_GITHUB_WEBHOOK_URL"]:
    errors.append(
        "GitHub App webhook URL drift: expected "
        + os.environ["EXPECTED_GITHUB_WEBHOOK_URL"]
        + ", got "
        + repr(hook.get("url"))
    )
if hook.get("content_type") != "json":
    errors.append("GitHub App webhook content_type must be json")
if hook.get("insecure_ssl") != "0":
    errors.append("GitHub App webhook insecure_ssl must be 0")
if errors:
    raise SystemExit("\n".join(errors))
PY
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

resolve_installation_id() {
  if [[ -n "${GITHUB_RUNNER_PROOF_INSTALLATION_ID:-}" ]]; then
    printf '%s\n' "${GITHUB_RUNNER_PROOF_INSTALLATION_ID}"
    return 0
  fi
  github_api "/orgs/${github_org}/installations" >"${artifact_dir}/responses/github-installations.json"
  python3 - "${artifact_dir}/responses/github-installations.json" "${github_app_slug}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
needle = sys.argv[2]
for installation in payload.get("installations") or []:
    if installation.get("app_slug") == needle:
        print(installation["id"])
        raise SystemExit(0)
raise SystemExit(f"installation for app_slug={needle!r} not found")
PY
}

connect_installation() {
  local installation_id="$1"
  api_request "POST" "/api/v1/github/installations/connect" "${artifact_dir}/responses/connect-installation.json" "" "${run_id}-connect"
  local state
  state="$(
    python3 - "${artifact_dir}/responses/connect-installation.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["state"])
PY
  )"
  curl -fsS "${api_base_url}/github/installations/callback?state=${state}&installation_id=${installation_id}" >"${artifact_dir}/responses/connect-callback.json"
  python3 - "${artifact_dir}/responses/connect-callback.json" "${installation_id}" "${org_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if str(payload.get("installation_id")) != sys.argv[2]:
    raise SystemExit(f"unexpected installation_id {payload.get('installation_id')!r}")
if payload.get("org_id") != sys.argv[3]:
    raise SystemExit(f"unexpected org_id {payload.get('org_id')!r}")
if not payload.get("active"):
    raise SystemExit("installation is not active")
PY
}

wait_for_github_run_id() {
  local dispatch_start="$1"
  local output_path="$2"
  local attempts=$((github_poll_timeout_seconds / 5))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    github_api "/repos/${github_repository}/actions/workflows/${github_workflow_file}/runs?event=workflow_dispatch&per_page=20" >"${output_path}"
    local result
    result="$(
      python3 - "${output_path}" "${dispatch_start}" "${github_workflow_ref}" <<'PY'
import json
import sys
from datetime import datetime, timezone

payload = json.load(open(sys.argv[1], encoding="utf-8"))
dispatch_start = datetime.fromisoformat(sys.argv[2].replace("Z", "+00:00"))
branch = sys.argv[3]
best = None
for item in payload.get("workflow_runs") or []:
    created_at = datetime.fromisoformat(item["created_at"].replace("Z", "+00:00"))
    if item.get("head_branch") != branch:
        continue
    if created_at < dispatch_start:
        continue
    if best is None or created_at > best[1]:
        best = (item["id"], created_at)
if best is not None:
    print(best[0])
PY
    )"
    if [[ -n "${result}" ]]; then
      printf '%s\n' "${result}"
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for GitHub workflow run id after ${dispatch_start}" >&2
  return 1
}

wait_for_github_run_completion() {
  local github_run_id="$1"
  local output_path="$2"
  local attempts=$((github_poll_timeout_seconds / 5))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    github_api "/repos/${github_repository}/actions/runs/${github_run_id}" >"${output_path}"
    local status_line
    status_line="$(
      python3 - "${output_path}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print("\t".join([
    payload.get("status", ""),
    payload.get("conclusion") or "",
    payload.get("head_sha") or "",
    payload.get("html_url") or "",
]))
PY
    )"
    local status conclusion head_sha html_url
    IFS=$'\t' read -r status conclusion head_sha html_url <<<"${status_line}"
    case "${status}" in
      completed)
        if [[ "${conclusion}" != "success" ]]; then
          echo "github workflow run ${github_run_id} concluded ${conclusion}; see ${html_url}" >&2
          return 1
        fi
        printf '%s\t%s\n' "${head_sha}" "${html_url}"
        return 0
        ;;
    esac
    sleep 5
  done
  echo "timed out waiting for GitHub workflow run ${github_run_id} to complete" >&2
  return 1
}

github_job_details() {
  local github_run_id="$1"
  local output_path="$2"
  github_api "/repos/${github_repository}/actions/runs/${github_run_id}/jobs" >"${output_path}"
  python3 - "${output_path}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
jobs = payload.get("jobs") or []
if not jobs:
    raise SystemExit("github run has no jobs")
job = jobs[0]
print("\t".join([str(job["id"]), job.get("name") or "", job.get("status") or "", job.get("conclusion") or ""]))
PY
}

wait_for_service_run() {
  local github_run_id="$1"
  local output_path="$2"
  local attempts=$((service_poll_timeout_seconds / 5))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    api_request "GET" "/api/v1/runs?limit=50&source_kind=github_actions&repository=${github_repository}&branch=${github_workflow_ref}" "${output_path}"
    local result
    result="$(
      python3 - "${output_path}" "${github_run_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
needle = sys.argv[2]
for item in payload.get("runs") or []:
    runner = item.get("runner") or {}
    if runner.get("provider_run_id") != needle:
        continue
    latest = item.get("latest_attempt") or {}
    print("\t".join([
        item.get("execution_id", ""),
        latest.get("attempt_id", ""),
        item.get("status", ""),
        runner.get("provider_job_id", ""),
        runner.get("workflow_name", ""),
        runner.get("job_name", ""),
    ]))
    raise SystemExit(0)
PY
    )"
    if [[ -z "${result}" ]]; then
      sleep 5
      continue
    fi
    local execution_id attempt_id status github_job_id workflow_name job_name
    IFS=$'\t' read -r execution_id attempt_id status github_job_id workflow_name job_name <<<"${result}"
    case "${status}" in
      succeeded)
        printf '%s\t%s\t%s\t%s\t%s\n' "${execution_id}" "${attempt_id}" "${github_job_id}" "${workflow_name}" "${job_name}"
        return 0
        ;;
      failed | canceled | timed_out)
        echo "service run for github_run_id=${github_run_id} finished with status ${status}" >&2
        return 1
        ;;
    esac
    sleep 5
  done
  echo "timed out waiting for service run for github_run_id=${github_run_id}" >&2
  return 1
}

wait_for_sticky_disk_inventory() {
  local execution_id="$1"
  local output_path="$2"
  local attempts=$((service_poll_timeout_seconds / 5))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    api_request "GET" "/api/v1/sticky-disks?limit=50&repository=${github_repository}" "${output_path}"
    local result
    result="$(
      python3 - "${output_path}" "${execution_id}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
execution_id = sys.argv[2]
disks = payload.get("disks") or []
matching = [disk for disk in disks if disk.get("last_execution_id") == execution_id and disk.get("last_save_state") == "committed"]
if not matching:
    raise SystemExit(0)
disk = matching[0]
print("\t".join([
    disk["installation_id"],
    disk["repository_id"],
    disk["key_hash"],
    str(len(disks)),
]))
PY
    )"
    if [[ -n "${result}" ]]; then
      printf '%s\n' "${result}"
      return 0
    fi
    sleep 5
  done
  echo "timed out waiting for sticky disk inventory to include execution ${execution_id}" >&2
  return 1
}

reset_repository_sticky_disks() {
  local list_path="${artifact_dir}/responses/preflight-sticky-disks.json"
  api_request "GET" "/api/v1/sticky-disks?limit=200&repository=${github_repository}" "${list_path}"
  local reset_rows
  reset_rows="$(
    python3 - "${list_path}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
for disk in payload.get("disks") or []:
    print("\t".join([
        disk.get("installation_id") or "",
        disk.get("repository_id") or "",
        disk.get("key_hash") or "",
    ]))
PY
  )"
  if [[ -z "${reset_rows}" ]]; then
    return 0
  fi
  local idx=0
  while IFS=$'\t' read -r sticky_installation_id sticky_repository_id sticky_key_hash; do
    [[ -n "${sticky_key_hash}" ]] || continue
    local payload_path="${artifact_dir}/payloads/preflight-reset-sticky-disk-${idx}.json"
    python3 - "${sticky_installation_id}" "${sticky_repository_id}" "${sticky_key_hash}" >"${payload_path}" <<'PY'
import json
import sys

installation_id, repository_id, key_hash = sys.argv[1:4]
print(json.dumps({
    "installation_id": installation_id,
    "repository_id": repository_id,
    "key_hash": key_hash,
}, indent=2, sort_keys=True))
PY
    api_request "POST" "/api/v1/sticky-disks/reset" "${artifact_dir}/responses/preflight-reset-sticky-disk-${idx}.json" "${payload_path}" "${run_id}-preflight-reset-${idx}"
    idx=$((idx + 1))
  done <<<"${reset_rows}"
}

dispatch_and_prove_run() {
  local prefix="$1"
  local dispatch_start
  dispatch_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  github_api --method POST "/repos/${github_repository}/actions/workflows/${github_workflow_file}/dispatches" -f ref="${github_workflow_ref}" >/dev/null
  local github_run_id
  github_run_id="$(wait_for_github_run_id "${dispatch_start}" "${artifact_dir}/responses/${prefix}-github-runs.json")"
  local github_completion
  github_completion="$(wait_for_github_run_completion "${github_run_id}" "${artifact_dir}/responses/${prefix}-github-run.json")"
  local head_sha html_url
  IFS=$'\t' read -r head_sha html_url <<<"${github_completion}"
  local job_details
  job_details="$(github_job_details "${github_run_id}" "${artifact_dir}/responses/${prefix}-github-jobs.json")"
  local github_job_id github_job_name github_job_status github_job_conclusion
  IFS=$'\t' read -r github_job_id github_job_name github_job_status github_job_conclusion <<<"${job_details}"
  if [[ "${github_job_status}" != "completed" || "${github_job_conclusion}" != "success" ]]; then
    echo "github job ${github_job_id} finished unexpectedly: status=${github_job_status} conclusion=${github_job_conclusion}" >&2
    return 1
  fi
  local service_result
  service_result="$(wait_for_service_run "${github_run_id}" "${artifact_dir}/responses/${prefix}-runs.json")"
  local execution_id attempt_id service_job_id workflow_name job_name
  IFS=$'\t' read -r execution_id attempt_id service_job_id workflow_name job_name <<<"${service_result}"
  api_request "GET" "/api/v1/runs/${execution_id}" "${artifact_dir}/responses/${prefix}-run-detail.json"
  api_request "GET" "/api/v1/run-logs/search?run_id=${execution_id}&limit=20" "${artifact_dir}/responses/${prefix}-logs-search.json"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "${dispatch_start}" \
    "${github_run_id}" \
    "${github_job_id}" \
    "${head_sha}" \
    "${html_url}" \
    "${execution_id}" \
    "${attempt_id}" \
    "${workflow_name}" \
    "${job_name}"
}

verification_wait_for_loopback_api "sandbox-rental-service" \
  "http://127.0.0.1:4243/api/v1/billing/entitlements" "401"

assert_github_app_configuration
installation_id="$(resolve_installation_id)"
connect_installation "${installation_id}"
reset_repository_sticky_disks

run1_info="$(dispatch_and_prove_run run1)"
IFS=$'\t' read -r run1_dispatch_start run1_github_run_id run1_github_job_id run1_head_sha run1_html_url run1_execution_id run1_attempt_id run1_workflow_name run1_job_name <<<"${run1_info}"

run1_sticky_info="$(wait_for_sticky_disk_inventory "${run1_execution_id}" "${artifact_dir}/responses/run1-sticky-disks.json")"
IFS=$'\t' read -r sticky_installation_id sticky_repository_id sticky_key_hash sticky_disk_count_before <<<"${run1_sticky_info}"

run2_info="$(dispatch_and_prove_run run2)"
IFS=$'\t' read -r run2_dispatch_start run2_github_run_id run2_github_job_id run2_head_sha run2_html_url run2_execution_id run2_attempt_id run2_workflow_name run2_job_name <<<"${run2_info}"

run2_sticky_info="$(wait_for_sticky_disk_inventory "${run2_execution_id}" "${artifact_dir}/responses/run2-sticky-disks.json")"
IFS=$'\t' read -r _ _ _ sticky_disk_count_after_second <<<"${run2_sticky_info}"

api_request "GET" "/api/v1/run-analytics/jobs?start=${window_start}" "${artifact_dir}/responses/jobs-analytics.json"
api_request "GET" "/api/v1/run-analytics/costs?start=${window_start}" "${artifact_dir}/responses/costs-analytics.json"
api_request "GET" "/api/v1/run-analytics/caches?start=${window_start}" "${artifact_dir}/responses/caches-analytics-before-reset.json"
api_request "GET" "/api/v1/run-analytics/runner-sizing?start=${window_start}" "${artifact_dir}/responses/runner-sizing-analytics.json"

python3 - \
  "${artifact_dir}/responses/run1-run-detail.json" \
  "${artifact_dir}/responses/run1-logs-search.json" \
  "${artifact_dir}/responses/run2-run-detail.json" \
  "${artifact_dir}/responses/run2-logs-search.json" \
  "${artifact_dir}/responses/jobs-analytics.json" \
  "${artifact_dir}/responses/costs-analytics.json" \
  "${artifact_dir}/responses/caches-analytics-before-reset.json" \
  "${artifact_dir}/responses/runner-sizing-analytics.json" \
  "${installation_id}" \
  "${run1_github_run_id}" \
  "${run2_github_run_id}" <<'PY'
import json
import sys

run1_detail = json.load(open(sys.argv[1], encoding="utf-8"))
run1_logs = json.load(open(sys.argv[2], encoding="utf-8"))
run2_detail = json.load(open(sys.argv[3], encoding="utf-8"))
run2_logs = json.load(open(sys.argv[4], encoding="utf-8"))
jobs_analytics = json.load(open(sys.argv[5], encoding="utf-8"))
costs_analytics = json.load(open(sys.argv[6], encoding="utf-8"))
caches_analytics = json.load(open(sys.argv[7], encoding="utf-8"))
runner_sizing = json.load(open(sys.argv[8], encoding="utf-8"))
installation_id = sys.argv[9]
run1_id = sys.argv[10]
run2_id = sys.argv[11]

for payload, run_id in ((run1_detail, run1_id), (run2_detail, run2_id)):
    runner = payload.get("runner") or {}
    if runner.get("provider_installation_id") != installation_id:
        raise SystemExit(f"unexpected installation_id in run detail: {runner.get('provider_installation_id')!r}")
    if runner.get("provider_run_id") != run_id:
        raise SystemExit(f"unexpected github run_id in run detail: {runner.get('provider_run_id')!r}")
    summary = payload.get("billing_summary") or {}
    if int(summary.get("window_count") or 0) < 1:
        raise SystemExit("expected billing_summary.window_count >= 1")
for payload in (run1_logs, run2_logs):
    if not (payload.get("results") or []):
        raise SystemExit("expected non-empty run log search results")

def bucket_count(payload, key):
    for bucket in payload:
        if bucket.get("key") == key:
            return int(bucket.get("count") or "0")
    return 0

if bucket_count(jobs_analytics.get("by_source") or [], "github_actions") < 2:
    raise SystemExit("expected jobs analytics github_actions bucket >= 2")
if bucket_count(costs_analytics.get("by_source") or [], "github_actions") < 2:
    raise SystemExit("expected costs analytics github_actions bucket >= 2")
if int(caches_analytics.get("checkout_requests") or "0") < 2:
    raise SystemExit("expected checkout_requests >= 2")
if int(caches_analytics.get("sticky_save_requests") or "0") < 2:
    raise SystemExit("expected sticky_save_requests >= 2")
if int(caches_analytics.get("sticky_commits") or "0") < 2:
    raise SystemExit("expected sticky_commits >= 2")
if int(caches_analytics.get("sticky_restore_hits") or "0") < 1:
    raise SystemExit("expected sticky_restore_hits >= 1 before reset")
if int(caches_analytics.get("sticky_restore_misses") or "0") < 1:
    raise SystemExit("expected sticky_restore_misses >= 1 before reset")
if not (runner_sizing.get("by_runner_class") or []):
    raise SystemExit("expected runner sizing analytics rows")
PY

reset_payload_path="${artifact_dir}/payloads/reset-sticky-disk.json"
python3 - "${sticky_installation_id}" "${sticky_repository_id}" "${sticky_key_hash}" >"${reset_payload_path}" <<'PY'
import json
import sys

installation_id, repository_id, key_hash = sys.argv[1:4]
print(json.dumps({
    "installation_id": installation_id,
    "repository_id": repository_id,
    "key_hash": key_hash,
}, indent=2, sort_keys=True))
PY

api_request "POST" "/api/v1/sticky-disks/reset" "${artifact_dir}/responses/reset-sticky-disk.json" "${reset_payload_path}" "${run_id}-reset-sticky-disk"
api_request "GET" "/api/v1/sticky-disks?limit=50&repository=${github_repository}" "${artifact_dir}/responses/sticky-disks-after-reset.json"

python3 - \
  "${artifact_dir}/responses/reset-sticky-disk.json" \
  "${artifact_dir}/responses/sticky-disks-after-reset.json" \
  "${sticky_key_hash}" \
  "${sticky_disk_count_before}" <<'PY'
import json
import sys

reset_payload = json.load(open(sys.argv[1], encoding="utf-8"))
inventory = json.load(open(sys.argv[2], encoding="utf-8"))
key_hash = sys.argv[3]
count_before = int(sys.argv[4])
if reset_payload.get("key_hash") != key_hash:
    raise SystemExit("sticky disk reset response returned unexpected key_hash")
for disk in inventory.get("disks") or []:
    if disk.get("key_hash") == key_hash:
        raise SystemExit("sticky disk still present after reset")
count_after = len(inventory.get("disks") or [])
if count_before > 0 and count_after >= count_before:
    raise SystemExit("expected sticky disk inventory count to decrease after reset")
PY

run3_info="$(dispatch_and_prove_run run3)"
IFS=$'\t' read -r run3_dispatch_start run3_github_run_id run3_github_job_id run3_head_sha run3_html_url run3_execution_id run3_attempt_id run3_workflow_name run3_job_name <<<"${run3_info}"

api_request "GET" "/api/v1/run-analytics/caches?start=${window_start}" "${artifact_dir}/responses/caches-analytics-after-reset.json"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

remote_psql sandbox_rental "
SELECT installation_id, org_id, account_login, account_type, active
FROM github_installations
WHERE installation_id = ${installation_id};
" >"${artifact_dir}/postgres/github_installation.tsv"

remote_psql sandbox_rental "
SELECT provider_job_id, provider_installation_id, provider_repository_id, repository_full_name, provider_run_id, job_name, workflow_name, status, conclusion, runner_id, runner_name
FROM runner_jobs
WHERE provider = 'github'
  AND provider_run_id IN (${run1_github_run_id}, ${run2_github_run_id}, ${run3_github_run_id})
ORDER BY provider_run_id, provider_job_id;
" >"${artifact_dir}/postgres/runner_jobs.tsv"

remote_psql sandbox_rental "
SELECT allocation_id, provider_installation_id, provider_repository_id, runner_class, runner_name, requested_for_provider_job_id, provider_runner_id, execution_id, attempt_id, state
FROM runner_allocations
WHERE provider = 'github'
  AND execution_id IN ('${run1_execution_id}'::uuid, '${run2_execution_id}'::uuid, '${run3_execution_id}'::uuid)
ORDER BY created_at;
" >"${artifact_dir}/postgres/runner_allocations.tsv"

remote_psql sandbox_rental "
SELECT e.execution_id, a.attempt_id, e.org_id, e.source_kind, e.workload_kind, e.source_ref, e.state, a.trace_id
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.execution_id IN ('${run1_execution_id}'::uuid, '${run2_execution_id}'::uuid, '${run3_execution_id}'::uuid)
ORDER BY e.created_at;
" >"${artifact_dir}/postgres/execution_state.tsv"

remote_psql sandbox_rental "
SELECT attempt_id, mount_id, key_hash, mount_path, base_generation, committed_generation, save_requested, save_state, failure_reason
FROM execution_sticky_disk_mounts
WHERE attempt_id IN ('${run1_attempt_id}'::uuid, '${run2_attempt_id}'::uuid, '${run3_attempt_id}'::uuid)
ORDER BY created_at, mount_name;
" >"${artifact_dir}/postgres/sticky_disk_mounts.tsv"

remote_psql sandbox_rental "
SELECT provider_installation_id, provider_repository_id, key_hash, key, current_generation, current_source_ref
FROM runner_sticky_disk_generations
WHERE provider = 'github'
  AND provider_installation_id = ${installation_id}
ORDER BY key_hash;
" >"${artifact_dir}/postgres/sticky_disk_generations.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
	  FROM job_events
	  WHERE execution_id IN (toUUID({run1_execution_id:String}), toUUID({run2_execution_id:String}), toUUID({run3_execution_id:String}))
	    AND source_kind = 'github_actions'
	    AND provider = 'github'
	    AND provider_installation_id = toUInt64({installation_id:String})
	    AND provider_run_id IN (toUInt64({run1_github_run_id:String}), toUInt64({run2_github_run_id:String}), toUInt64({run3_github_run_id:String}))
	    AND provider_job_id IN (toUInt64({run1_github_job_id:String}), toUInt64({run2_github_job_id:String}), toUInt64({run3_github_job_id:String}))
	    AND status = 'succeeded'
	" 3 "${artifact_dir}/clickhouse/job_event_count.tsv" \
	  --param_run1_execution_id="${run1_execution_id}" \
	  --param_run2_execution_id="${run2_execution_id}" \
	  --param_run3_execution_id="${run3_execution_id}" \
	  --param_installation_id="${installation_id}" \
	  --param_run1_github_run_id="${run1_github_run_id}" \
	  --param_run2_github_run_id="${run2_github_run_id}" \
	  --param_run3_github_run_id="${run3_github_run_id}" \
	  --param_run1_github_job_id="${run1_github_job_id}" \
	  --param_run2_github_job_id="${run2_github_job_id}" \
	  --param_run3_github_job_id="${run3_github_job_id}"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM job_logs
  WHERE execution_id IN (toUUID({run1_execution_id:String}), toUUID({run2_execution_id:String}), toUUID({run3_execution_id:String}))
" 3 "${artifact_dir}/clickhouse/job_logs_count.tsv" \
  --param_run1_execution_id="${run1_execution_id}" \
  --param_run2_execution_id="${run2_execution_id}" \
  --param_run3_execution_id="${run3_execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'github.checkout.bundle'
    AND SpanAttributes['execution.id'] = {run1_execution_id:String}
" 1 "${artifact_dir}/clickhouse/run1-checkout-span-count.tsv" --param_run1_execution_id="${run1_execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'github.checkout.bundle'
    AND SpanAttributes['execution.id'] = {run2_execution_id:String}
    AND SpanAttributes['github.checkout.cache_hit'] = 'true'
" 1 "${artifact_dir}/clickhouse/run2-checkout-hit-count.tsv" --param_run2_execution_id="${run2_execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'github.stickydisk.compile'
    AND SpanAttributes['github.job_id'] = {run2_github_job_id:String}
    AND toUInt64OrZero(SpanAttributes['github.stickydisk.restore_hit_count']) >= 1
" 1 "${artifact_dir}/clickhouse/run2-sticky-hit-count.tsv" --param_run2_github_job_id="${run2_github_job_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'github.stickydisk.compile'
    AND SpanAttributes['github.job_id'] = {run3_github_job_id:String}
    AND toUInt64OrZero(SpanAttributes['github.stickydisk.restore_miss_count']) >= 1
" 1 "${artifact_dir}/clickhouse/run3-sticky-miss-count.tsv" --param_run3_github_job_id="${run3_github_job_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'github.stickydisk.commit_zfs'
    AND SpanAttributes['execution.id'] IN ({run1_execution_id:String}, {run2_execution_id:String}, {run3_execution_id:String})
    AND SpanAttributes['github.stickydisk.state'] = 'committed'
" 3 "${artifact_dir}/clickhouse/sticky-commit-count.tsv" \
  --param_run1_execution_id="${run1_execution_id}" \
  --param_run2_execution_id="${run2_execution_id}" \
  --param_run3_execution_id="${run3_execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.runs.list'
" 3 "${artifact_dir}/clickhouse/runs-list-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.runs.get'
" 3 "${artifact_dir}/clickhouse/runs-get-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.logs.search'
" 3 "${artifact_dir}/clickhouse/log-search-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName IN (
      'sandbox-rental.analytics.jobs',
      'sandbox-rental.analytics.costs',
      'sandbox-rental.analytics.caches',
      'sandbox-rental.analytics.runner_sizing',
      'sandbox-rental.stickydisks.list',
      'sandbox-rental.stickydisks.reset'
    )
" 6 "${artifact_dir}/clickhouse/read-api-span-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_run1_execution_id="${run1_execution_id}" \
    --param_run2_execution_id="${run2_execution_id}" \
    --param_run3_execution_id="${run3_execution_id}" \
    --param_run1_github_run_id="${run1_github_run_id}" \
    --param_run2_github_run_id="${run2_github_run_id}" \
    --param_run3_github_run_id="${run3_github_run_id}" \
    --query "
      SELECT
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId,
        SpanAttributes['execution.id'] AS execution_id,
        SpanAttributes['github.job_id'] AS github_job_id,
        SpanAttributes['github.run_id'] AS github_run_id,
        SpanAttributes['github.checkout.cache_hit'] AS checkout_cache_hit,
        SpanAttributes['github.stickydisk.restore_hit_count'] AS sticky_restore_hit_count,
        SpanAttributes['github.stickydisk.restore_miss_count'] AS sticky_restore_miss_count,
        SpanAttributes['github.stickydisk.state'] AS sticky_state
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
        AND (
          SpanAttributes['execution.id'] IN ({run1_execution_id:String}, {run2_execution_id:String}, {run3_execution_id:String})
          OR SpanAttributes['github.run_id'] IN ({run1_github_run_id:String}, {run2_github_run_id:String}, {run3_github_run_id:String})
        )
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/github_runner_spans.tsv"

run_id="${run_id}" \
artifact_dir="${artifact_dir}" \
org_id="${org_id}" \
installation_id="${installation_id}" \
run1_github_run_id="${run1_github_run_id}" \
run1_execution_id="${run1_execution_id}" \
run1_attempt_id="${run1_attempt_id}" \
run2_github_run_id="${run2_github_run_id}" \
run2_execution_id="${run2_execution_id}" \
run2_attempt_id="${run2_attempt_id}" \
run3_github_run_id="${run3_github_run_id}" \
run3_execution_id="${run3_execution_id}" \
run3_attempt_id="${run3_attempt_id}" \
sticky_key_hash="${sticky_key_hash}" \
window_start="${window_start}" \
window_end="${window_end}" \
python3 - <<'PY' >"${artifact_dir}/run.json"
import json
import os

print(json.dumps({
    "artifact_dir": os.environ["artifact_dir"],
    "installation_id": int(os.environ["installation_id"]),
    "org_id": int(os.environ["org_id"]),
    "run1": {
        "attempt_id": os.environ["run1_attempt_id"],
        "execution_id": os.environ["run1_execution_id"],
        "github_run_id": int(os.environ["run1_github_run_id"]),
    },
    "run2": {
        "attempt_id": os.environ["run2_attempt_id"],
        "execution_id": os.environ["run2_execution_id"],
        "github_run_id": int(os.environ["run2_github_run_id"]),
    },
    "run3": {
        "attempt_id": os.environ["run3_attempt_id"],
        "execution_id": os.environ["run3_execution_id"],
        "github_run_id": int(os.environ["run3_github_run_id"]),
    },
    "sticky_key_hash_reset": os.environ["sticky_key_hash"],
    "verification_run_id": os.environ["run_id"],
    "window_end": os.environ["window_end"],
    "window_start": os.environ["window_start"],
}, indent=2, sort_keys=True))
PY

printf 'github runner proof passed: run_id=%s github_runs=%s,%s,%s artifacts=%s\n' \
  "${run_id}" "${run1_github_run_id}" "${run2_github_run_id}" "${run3_github_run_id}" "${artifact_dir}"
