#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-organization-sync-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/organization-sync-proof}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
organization_log_path="${artifact_dir}/organization-ui.log"
base_url="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres" "${artifact_dir}/responses"

remote_json_request() {
  local json="$1"
  printf '%s' "${json}" | base64 -w0
}

remote_identity_api() {
  local method="$1"
  local path="$2"
  local token="$3"
  local output_path="$4"
  local idempotency_key="${5:-}"
  local body="${6:-}"
  local request_b64
  request_b64="$(
    METHOD="${method}" API_PATH="${path}" API_TOKEN="${token}" IDEMPOTENCY_KEY="${idempotency_key}" BODY_JSON="${body}" python3 - <<'PY'
import json
import os
print(json.dumps({
    "method": os.environ["METHOD"],
    "path": os.environ["API_PATH"],
    "token": os.environ["API_TOKEN"],
    "idempotency_key": os.environ.get("IDEMPOTENCY_KEY", ""),
    "body": os.environ.get("BODY_JSON", ""),
}))
PY
  )"
  request_b64="$(remote_json_request "${request_b64}")"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64
import json
import sys
import urllib.error
import urllib.request

payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
body = payload.get(\"body\") or \"\"
data = body.encode() if body else None
headers = {
    \"Authorization\": \"Bearer \" + payload[\"token\"],
    \"Accept\": \"application/json\",
}
if data is not None:
    headers[\"Content-Type\"] = \"application/json\"
if payload.get(\"idempotency_key\"):
    headers[\"Idempotency-Key\"] = payload[\"idempotency_key\"]
request = urllib.request.Request(
    \"http://127.0.0.1:4248\" + payload[\"path\"],
    data=data,
    method=payload[\"method\"],
    headers=headers,
)
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        response_body = response.read().decode()
        status_code = response.status
except urllib.error.HTTPError as error:
    response_body = error.read().decode()
    status_code = error.code
json.dump({\"status\": status_code, \"body\": response_body}, sys.stdout, indent=2, sort_keys=True)
print()
'" >"${output_path}"
}

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

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  shift 4
  local extra_args=("$@")
  local count="0"
  for _ in $(seq 1 60); do
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

assert_api_status() {
  local path="$1"
  local expected="$2"
  python3 - "${path}" "${expected}" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
expected = int(sys.argv[2])
actual = int(payload.get("status") or 0)
if actual != expected:
    raise SystemExit(f"{sys.argv[1]} status {actual}, expected {expected}: {payload.get('body', '')[:500]}")
PY
}

verification_print_artifacts "${artifact_dir}" "${organization_log_path}" "${run_json_path}"
verification_wait_for_http "console UI" "${base_url}" "200"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
identity_token="${IDENTITY_SERVICE_ACCESS_TOKEN}"
expected_email="acme-admin@${VERIFICATION_DOMAIN}"
acme_admin_password="$(verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password)"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${base_url}" \
  TEST_BASE_URL="${base_url}" \
  VERSELF_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="${expected_email}" \
  TEST_PASSWORD="${acme_admin_password}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/organization.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${organization_log_path}" 2>&1
organization_status=$?
set -e
verification_tail_log_on_failure "${organization_status}" "${organization_log_path}" "180"
if [[ "${organization_status}" -ne 0 ]]; then
  exit "${organization_status}"
fi

remote_identity_api GET "/api/v1/organization" "${identity_token}" "${artifact_dir}/responses/organization-before.json"
remote_identity_api GET "/api/v1/organization/members" "${identity_token}" "${artifact_dir}/responses/members-before.json"
assert_api_status "${artifact_dir}/responses/organization-before.json" 200
assert_api_status "${artifact_dir}/responses/members-before.json" 200

org_id=""
target_user_id=""
target_email=""
original_role_keys_json=""
python3 - "${artifact_dir}/responses/organization-before.json" "${artifact_dir}/responses/members-before.json" "${VERIFICATION_DOMAIN}" "${artifact_dir}/member-state.env" "${artifact_dir}/responses/member-role-update.json" "${artifact_dir}/responses/member-role-stale-update.json" "${artifact_dir}/responses/organization-profile-update.json" "${run_id}" <<'PY'
import json
import shlex
import sys

organization = json.loads(json.load(open(sys.argv[1], encoding="utf-8"))["body"])
members = json.loads(json.load(open(sys.argv[2], encoding="utf-8"))["body"])
domain = sys.argv[3]
env_path = sys.argv[4]
success_body_path = sys.argv[5]
stale_body_path = sys.argv[6]
organization_update_path = sys.argv[7]
run_id = sys.argv[8]
for key in ("display_name", "slug", "version"):
    if key not in organization:
        raise SystemExit(f"organization response missing {key}")

target_email = f"acme-user@{domain}"
target = None
for member in members.get("members") or []:
    if member.get("email") == target_email:
        target = member
        break
if target is None:
    for member in members.get("members") or []:
        roles = member.get("role_keys") or []
        if "owner" not in roles:
            target = member
            break
if target is None:
    raise SystemExit("no assignable member row found")

original_roles = sorted(set(target.get("role_keys") or ["member"]))
primary = "admin" if "admin" in original_roles else "member"
target_role = "member" if primary == "admin" else "admin"
org_acl_version = int(organization["org_acl_version"])
success_body = {
    "expected_org_acl_version": org_acl_version,
    "expected_role_keys": original_roles,
    "role_keys": [target_role],
}
stale_body = {
    "expected_org_acl_version": org_acl_version,
    "expected_role_keys": original_roles,
    "role_keys": original_roles,
}
json.dump(success_body, open(success_body_path, "w", encoding="utf-8"), sort_keys=True)
json.dump(stale_body, open(stale_body_path, "w", encoding="utf-8"), sort_keys=True)
json.dump({
    "display_name": f"Acme Verification {run_id}",
    "slug": organization["slug"],
    "version": int(organization["version"]),
}, open(organization_update_path, "w", encoding="utf-8"), sort_keys=True)
with open(env_path, "w", encoding="utf-8") as output:
    for key, value in {
        "org_id": str(organization["org_id"]),
        "org_display_name": organization["display_name"],
        "org_slug": organization["slug"],
        "target_user_id": target["user_id"],
        "target_email": target.get("email", ""),
        "original_role_keys_json": json.dumps(original_roles),
        "target_role": target_role,
    }.items():
        output.write(f"{key}={shlex.quote(value)}\n")
PY

# shellcheck disable=SC1090
# shellcheck disable=SC1091
source "${artifact_dir}/member-state.env"
: "${org_display_name:?}"
: "${org_slug:?}"
success_body="$(<"${artifact_dir}/responses/member-role-update.json")"
stale_body="$(<"${artifact_dir}/responses/member-role-stale-update.json")"
organization_update_body="$(<"${artifact_dir}/responses/organization-profile-update.json")"

remote_identity_api PATCH "/api/v1/organization" "${identity_token}" "${artifact_dir}/responses/organization-profile-update-response.json" "${run_id}-organization-profile-update" "${organization_update_body}"
assert_api_status "${artifact_dir}/responses/organization-profile-update-response.json" 200
python3 - "${artifact_dir}/responses/organization-before.json" "${artifact_dir}/responses/organization-profile-update-response.json" "${artifact_dir}/responses/organization-profile-restore.json" <<'PY'
import json
import sys

original = json.loads(json.load(open(sys.argv[1], encoding="utf-8"))["body"])
updated = json.loads(json.load(open(sys.argv[2], encoding="utf-8"))["body"])
json.dump({
    "display_name": original["display_name"],
    "slug": original["slug"],
    "version": int(updated["version"]),
}, open(sys.argv[3], "w", encoding="utf-8"), sort_keys=True)
PY

remote_identity_api PUT "/api/v1/organization/members/${target_user_id}/roles" "${identity_token}" "${artifact_dir}/responses/member-role-update-response.json" "${run_id}-member-role-accepted" "${success_body}"
assert_api_status "${artifact_dir}/responses/member-role-update-response.json" 200

remote_identity_api PUT "/api/v1/organization/members/${target_user_id}/roles" "${identity_token}" "${artifact_dir}/responses/member-role-stale-update-response.json" "${run_id}-member-role-rejected" "${stale_body}"
assert_api_status "${artifact_dir}/responses/member-role-stale-update-response.json" 409

remote_identity_api GET "/api/v1/organization" "${identity_token}" "${artifact_dir}/responses/organization-after.json"
remote_identity_api GET "/api/v1/organization/members" "${identity_token}" "${artifact_dir}/responses/members-after.json"
assert_api_status "${artifact_dir}/responses/organization-after.json" 200
assert_api_status "${artifact_dir}/responses/members-after.json" 200

python3 - "${artifact_dir}/responses/organization-after.json" "${artifact_dir}/responses/members-after.json" "${target_user_id}" "${original_role_keys_json}" "${artifact_dir}/responses/member-role-restore.json" <<'PY'
import json
import sys

organization = json.loads(json.load(open(sys.argv[1], encoding="utf-8"))["body"])
members = json.loads(json.load(open(sys.argv[2], encoding="utf-8"))["body"])
target_user_id = sys.argv[3]
original_roles = json.loads(sys.argv[4])
target = None
for member in members.get("members") or []:
    if member.get("user_id") == target_user_id:
        target = member
        break
if target is None:
    raise SystemExit("target member disappeared before restore")
restore_body = {
    "expected_org_acl_version": int(organization["org_acl_version"]),
    "expected_role_keys": sorted(set(target.get("role_keys") or [])),
    "role_keys": original_roles,
}
json.dump(restore_body, open(sys.argv[5], "w", encoding="utf-8"), sort_keys=True)
PY
restore_body="$(<"${artifact_dir}/responses/member-role-restore.json")"
remote_identity_api PUT "/api/v1/organization/members/${target_user_id}/roles" "${identity_token}" "${artifact_dir}/responses/member-role-restore-response.json" "${run_id}-member-role-restore" "${restore_body}"
assert_api_status "${artifact_dir}/responses/member-role-restore-response.json" 200
organization_profile_restore_body="$(<"${artifact_dir}/responses/organization-profile-restore.json")"
remote_identity_api PATCH "/api/v1/organization" "${identity_token}" "${artifact_dir}/responses/organization-profile-restore-response.json" "${run_id}-organization-profile-restore" "${organization_profile_restore_body}"
assert_api_status "${artifact_dir}/responses/organization-profile-restore-response.json" 200
window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

escaped_org_id="${org_id//\'/\'\'}"
escaped_target_user_id="${target_user_id//\'/\'\'}"
remote_psql identity_service "
SELECT org_id, display_name, slug, state, version
FROM identity_organizations
WHERE org_id = '${escaped_org_id}';
" "${artifact_dir}/postgres/organization-profile.tsv"
if ! grep -Fq "${escaped_org_id}"$'\t'"${org_display_name}"$'\t'"${org_slug}"$'\tactive\t' "${artifact_dir}/postgres/organization-profile.tsv"; then
  echo "identity_organizations did not contain the restored organization profile" >&2
  exit 1
fi

remote_psql identity_service "
SELECT version, updated_by
FROM identity_org_acl_state
WHERE org_id = '${escaped_org_id}';
" "${artifact_dir}/postgres/org-acl-state.tsv"

remote_psql identity_service "
SELECT r.result, r.reason, r.aggregate_version, o.event_type, o.projected_at IS NOT NULL AS projected
FROM identity_command_results r
JOIN identity_domain_event_outbox o ON o.command_id = r.command_id
WHERE r.org_id = '${escaped_org_id}'
  AND r.target_user_id = '${escaped_target_user_id}'
  AND r.created_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz + INTERVAL '45 seconds'
ORDER BY r.created_at, o.event_type;
" "${artifact_dir}/postgres/member-role-command-ledger.tsv"

if [[ ! -s "${artifact_dir}/postgres/member-role-command-ledger.tsv" ]]; then
  echo "identity_service command/outbox tables did not record member role changes" >&2
  exit 1
fi

wait_for_clickhouse_count verself "
  SELECT count()
  FROM domain_update_ledger
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND service_name = 'identity-service'
    AND org_id = {org_id:String}
    AND target_id = {target_user_id:String}
    AND event_type IN ('identity.organization.member.roles.write.accepted', 'identity.organization.member.roles.write.rejected')
" 2 "${artifact_dir}/clickhouse/domain-update-ledger-count.tsv" \
  --param_org_id="${org_id}" \
  --param_target_user_id="${target_user_id}"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM domain_update_ledger
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND service_name = 'identity-service'
    AND org_id = {org_id:String}
    AND target_id = {target_user_id:String}
    AND result = 'rejected'
    AND reason = 'stale_org_acl_version'
" 1 "${artifact_dir}/clickhouse/domain-update-ledger-conflict-count.tsv" \
  --param_org_id="${org_id}" \
  --param_target_user_id="${target_user_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'identity-service'
    AND SpanName IN (
      'identity.organization.update',
      'identity.pg.organization_profile.update',
      'identity.pg.organization_profile.get'
    )
    AND (arrayElement(SpanAttributes, 'verself.org_id') = '' OR arrayElement(SpanAttributes, 'verself.org_id') = {org_id:String})
" 3 "${artifact_dir}/clickhouse/identity-organization-profile-traces-count.tsv" \
  --param_org_id="${org_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'identity-service'
    AND SpanName IN (
      'identity.member_roles.command',
      'identity.domain_ledger.project_pending',
      'identity.domain_ledger.project_event'
    )
" 3 "${artifact_dir}/clickhouse/identity-sync-traces-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database verself \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_org_id="${org_id}" \
    --param_target_user_id="${target_user_id}" \
    --query "
      SELECT recorded_at, event_type, result, reason, aggregate_version, target_id
      FROM domain_update_ledger
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND service_name = 'identity-service'
        AND org_id = {org_id:String}
        AND target_id = {target_user_id:String}
      ORDER BY recorded_at, event_type
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/domain-update-ledger.tsv"

printf 'organization sync proof passed for org %s member %s (%s)\n' "${org_id}" "${target_user_id}" "${target_email}"
