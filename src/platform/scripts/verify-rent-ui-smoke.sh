#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-sandbox-ui-smoke}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
ui_check_dir="${artifact_dir}/ui-check"
run_json_path="${artifact_dir}/run.json"

if [[ -z "${base_url}" ]]; then
  echo "TEST_BASE_URL is required" >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
submit_requested_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_wait_for_http "rent-a-sandbox UI" "${base_url}" "200"

acme_admin_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password
)"

set +e
env \
  VERIFICATION_RUN_ID="${run_id}" \
  TEST_BASE_URL="${base_url}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  ACME_ADMIN_PASSWORD="${acme_admin_password}" \
  ADMIN_UI_ARTIFACT_DIR="${ui_check_dir}" \
  bash -lc '
    cd "$1"
    vp exec node e2e/admin-ui-check.mjs
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/rent-a-sandbox" \
  >"${artifact_dir}/admin-ui-check.log" 2>&1
smoke_status=$?
set -e

terminal_observed_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
results_path="${ui_check_dir}/${run_id}.json"

python3 - "${results_path}" "${run_json_path}" "${run_id}" "${base_url}" "${submit_requested_at}" "${terminal_observed_at}" "${smoke_status}" <<'PY'
import json
import sys
from pathlib import Path

results_path = Path(sys.argv[1])
run_json_path = Path(sys.argv[2])
run_id = sys.argv[3]
base_url = sys.argv[4]
submit_requested_at = sys.argv[5]
terminal_observed_at = sys.argv[6]
smoke_status = int(sys.argv[7])

results = []
if results_path.exists():
    results = json.loads(results_path.read_text(encoding="utf-8"))

failures = []
for result in results:
    if result.get("status") != "ok":
        failures.append(
            result.get("error")
            or "; ".join(result.get("page_errors", []))
            or "; ".join(item.get("failure", "") for item in result.get("failed_requests", []))
            or f"{result.get('label', 'unknown')} failed"
        )

status = "succeeded" if smoke_status == 0 and not failures else "failed"
error = " | ".join(part for part in failures if part)
if smoke_status != 0 and not error:
    error = "admin-ui-check exited non-zero"

run = {
    "verification_run_id": run_id,
    "repo_url": "",
    "ref": "",
    "repo_id": "",
    "bootstrap_generation_id": "",
    "bootstrap_execution_id": "",
    "bootstrap_attempt_id": "",
    "bootstrap_source_sha": "",
    "refresh_generation_id": "",
    "refresh_execution_id": "",
    "refresh_attempt_id": "",
    "refreshed_commit_sha": "",
    "submit_requested_at": submit_requested_at,
    "execution_id": "",
    "attempt_id": "",
    "started_balance": 0,
    "finished_balance": 0,
    "status": status,
    "detail_url": base_url,
    "log_marker": "",
    "terminal_observed_at": terminal_observed_at,
    "error": error,
}

run_json_path.parent.mkdir(parents=True, exist_ok=True)
run_json_path.write_text(json.dumps(run, indent=2) + "\n", encoding="utf-8")
PY

"${script_dir}/collect-sandbox-verification-evidence.sh" "${run_json_path}" "${artifact_dir}/evidence"

exit "${smoke_status}"
