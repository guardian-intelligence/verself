#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-profile-smoke-test-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/profile-smoke-test}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
profile_log_path="${artifact_dir}/profile-ui.log"
base_url="${TEST_BASE_URL:-https://${VERIFICATION_DOMAIN}}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres" "${artifact_dir}/responses"

suffix="$(python3 - "${run_id}" <<'PY'
import re
import sys
value = re.sub(r"[^A-Za-z0-9]", "", sys.argv[1])[-10:] or "smoke"
print(value)
PY
)"
expected_email="acme-admin@${VERIFICATION_DOMAIN}"
expected_given="Profile${suffix}"
expected_family="Smoke Test"
expected_display="${expected_given} ${expected_family}"
expected_locale="en-GB"
expected_timezone="America/New_York"
expected_time_display="local"
expected_theme="dark"
expected_default_surface="schedules"

remote_json_request() {
  local json="$1"
  printf '%s' "${json}" | base64 -w0
}

remote_profile_api() {
  local method="$1"
  local path="$2"
  local token="$3"
  local output_path="$4"
  local request_b64
  request_b64="$(
    METHOD="${method}" API_PATH="${path}" API_TOKEN="${token}" python3 - <<'PY'
import json
import os
print(json.dumps({
    "method": os.environ["METHOD"],
    "path": os.environ["API_PATH"],
    "token": os.environ["API_TOKEN"],
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
request = urllib.request.Request(
    \"http://127.0.0.1:4258\" + payload[\"path\"],
    method=payload[\"method\"],
    headers={
        \"Authorization\": \"Bearer \" + payload[\"token\"],
        \"Accept\": \"application/json\",
    },
)
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        body = response.read().decode()
        status_code = response.status
except urllib.error.HTTPError as error:
    body = error.read().decode()
    status_code = error.code
json.dump({\"status\": status_code, \"body\": body}, sys.stdout, indent=2, sort_keys=True)
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

verification_print_artifacts "${artifact_dir}" "${profile_log_path}" "${run_json_path}"
verification_wait_for_http "verself-web UI" "${base_url}" "200"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
machine_identity_token="${IDENTITY_SERVICE_ACCESS_TOKEN}"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
escaped_email="${expected_email//\'/\'\'}"

remote_psql profile "
SELECT COALESCE(p.locale, ''), COALESCE(p.timezone, ''), COALESCE(p.time_display, ''), COALESCE(p.theme, ''), COALESCE(p.default_surface, '')
FROM profile_subjects s
LEFT JOIN profile_preferences p ON p.subject_id = s.subject_id
WHERE s.email_cache = '${escaped_email}'
ORDER BY s.updated_at DESC
LIMIT 1;
" "${artifact_dir}/postgres/current-profile-preferences.tsv"
if IFS=$'\t' read -r current_locale current_timezone current_time_display current_theme current_default_surface <"${artifact_dir}/postgres/current-profile-preferences.tsv"; then
  if [[ "${current_locale}" == "${expected_locale}" &&
        "${current_timezone}" == "${expected_timezone}" &&
        "${current_time_display}" == "${expected_time_display}" &&
        "${current_theme}" == "${expected_theme}" &&
        "${current_default_surface}" == "${expected_default_surface}" ]]; then
    expected_locale="fr-FR"
    expected_timezone="Europe/London"
    expected_time_display="utc"
    expected_theme="light"
    expected_default_surface="executions"
  fi
fi

remote_profile_api GET "/api/v1/profile" "${machine_identity_token}" "${artifact_dir}/responses/machine-token-profile-read.json"
python3 - "${artifact_dir}/responses/machine-token-profile-read.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
body = payload.get("body", "")
if "human-profile-required" not in body:
    raise SystemExit("machine token profile read did not fail closed with human-profile-required")
PY

acme_admin_password="$(verification_remote_sudo_cat /etc/credstore/seed-system/acme-admin-password)"
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
  PROFILE_SMOKE_TEST_LOCALE="${expected_locale}" \
  PROFILE_SMOKE_TEST_TIMEZONE="${expected_timezone}" \
  PROFILE_SMOKE_TEST_TIME_DISPLAY="${expected_time_display}" \
  PROFILE_SMOKE_TEST_THEME="${expected_theme}" \
  PROFILE_SMOKE_TEST_DEFAULT_SURFACE="${expected_default_surface}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/profile.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/verself-web" "${artifact_dir}/playwright-results" \
  >"${profile_log_path}" 2>&1
profile_status=$?
set -e
window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_tail_log_on_failure "${profile_status}" "${profile_log_path}" "180"
if [[ "${profile_status}" -ne 0 ]]; then
  exit "${profile_status}"
fi

escaped_given="${expected_given//\'/\'\'}"
escaped_family="${expected_family//\'/\'\'}"
escaped_display="${expected_display//\'/\'\'}"
escaped_locale="${expected_locale//\'/\'\'}"
escaped_timezone="${expected_timezone//\'/\'\'}"
escaped_time_display="${expected_time_display//\'/\'\'}"
escaped_theme="${expected_theme//\'/\'\'}"
escaped_default_surface="${expected_default_surface//\'/\'\'}"

remote_psql profile "
SELECT subject_id, org_id, email_cache, given_name_cache, family_name_cache, display_name_cache, identity_version
FROM profile_subjects
WHERE email_cache = '${escaped_email}'
  AND given_name_cache = '${escaped_given}'
  AND family_name_cache = '${escaped_family}'
  AND display_name_cache = '${escaped_display}'
  AND identity_version >= 1
ORDER BY updated_at DESC
LIMIT 1;
" "${artifact_dir}/postgres/profile-subject.tsv"

subject_id="$(cut -f1 "${artifact_dir}/postgres/profile-subject.tsv" | head -n 1)"
org_id="$(cut -f2 "${artifact_dir}/postgres/profile-subject.tsv" | head -n 1)"
if [[ -z "${subject_id}" || -z "${org_id}" ]]; then
  echo "profile_subjects did not contain the updated profile cache" >&2
  exit 1
fi

remote_psql profile "
SELECT version, locale, timezone, time_display, theme, default_surface
FROM profile_preferences
WHERE subject_id = '${subject_id//\'/\'\'}'
  AND locale = '${escaped_locale}'
  AND timezone = '${escaped_timezone}'
  AND time_display = '${escaped_time_display}'
  AND theme = '${escaped_theme}'
  AND default_surface = '${escaped_default_surface}';
" "${artifact_dir}/postgres/profile-preferences.tsv"
if [[ ! -s "${artifact_dir}/postgres/profile-preferences.tsv" ]]; then
  echo "profile_preferences did not contain the persisted values" >&2
  exit 1
fi

remote_psql profile "
SELECT subject, aggregate_subject_id, aggregate_version
FROM profile_domain_event_outbox
WHERE aggregate_subject_id = '${subject_id//\'/\'\'}'
  AND subject IN ('events.profile.subject.updated', 'events.profile.preferences.updated')
ORDER BY created_at;
" "${artifact_dir}/postgres/profile-outbox.tsv"
python3 - "${artifact_dir}/postgres/profile-outbox.tsv" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
subjects = {row[0] for row in rows if row}
missing = {"events.profile.subject.updated", "events.profile.preferences.updated"} - subjects
if missing:
    raise SystemExit("missing profile outbox subjects: " + ", ".join(sorted(missing)))
PY

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'profile-service'
    AND SpanName IN ('profile.subject.read', 'profile.subject.identity.write', 'profile.preferences.write')
" 3 "${artifact_dir}/clickhouse/profile-business-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'identity-service'
    AND SpanName = 'identity.human_profile.write'
    AND arrayElement(SpanAttributes, 'verself.subject_id') = {subject_id:String}
" 1 "${artifact_dir}/clickhouse/identity-human-profile-spans-count.tsv" \
  --param_subject_id="${subject_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'profile-service'
    AND SpanName = 'auth.spiffe.mtls.client'
    AND endsWith(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), '/svc/identity-service')
" 1 "${artifact_dir}/clickhouse/profile-identity-mtls-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'profile-service'
    AND SpanName = 'auth.spiffe.mtls.client'
    AND endsWith(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), '/svc/governance-service')
" 2 "${artifact_dir}/clickhouse/profile-governance-mtls-spans-count.tsv"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND org_id = {org_id:String}
    AND service_name = 'profile-service'
    AND audit_event IN ('profile.subject.read', 'profile.subject.identity.write', 'profile.preferences.write')
    AND actor_id = {subject_id:String}
" 3 "${artifact_dir}/clickhouse/profile-audit-events-count.tsv" \
  --param_org_id="${org_id}" \
  --param_subject_id="${subject_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'profile-service'
    AND SpanName = 'profile.subject.read'
    AND arrayElement(SpanAttributes, 'profile.outcome') = 'denied'
" 1 "${artifact_dir}/clickhouse/profile-denied-span-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_expected_given="${expected_given}" \
    --param_expected_display="${expected_display}" \
    --query "
      SELECT count()
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName IN ('profile-service', 'identity-service', 'verself-web')
        AND (
          position(toString(SpanAttributes), {expected_given:String}) > 0
          OR position(toString(SpanAttributes), {expected_display:String}) > 0
        )
    "
) >"${artifact_dir}/clickhouse/raw-name-span-leak-count.tsv"
raw_name_span_leaks="$(tail -n 1 "${artifact_dir}/clickhouse/raw-name-span-leak-count.tsv" | tr -d '[:space:]')"
if [[ "${raw_name_span_leaks}" != "0" ]]; then
  echo "raw submitted profile name appeared in span attributes" >&2
  exit 1
fi

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT Timestamp, ServiceName, SpanName, StatusCode, arrayElement(SpanAttributes, 'profile.outcome') AS profile_outcome, arrayElement(SpanAttributes, 'identity.outcome') AS identity_outcome
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName IN ('profile-service', 'identity-service', 'verself-web')
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/otel-traces.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database verself \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_org_id="${org_id}" \
    --query "
      SELECT recorded_at, service_name, operation_id, audit_event, decision, result, actor_id, target_id, changed_fields, before_hash, after_hash
      FROM audit_events
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND org_id = {org_id:String}
        AND service_name IN ('profile-service', 'identity-service')
      ORDER BY recorded_at, sequence
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/profile-audit-events.tsv"

cat >"${run_json_path}" <<EOF
{
  "run_id": "${run_id}",
  "subject_id": "${subject_id}",
  "org_id": "${org_id}",
  "email": "${expected_email}",
  "given_name": "${expected_given}",
  "family_name": "${expected_family}",
  "display_name": "${expected_display}",
  "locale": "${expected_locale}",
  "timezone": "${expected_timezone}",
  "time_display": "${expected_time_display}",
  "theme": "${expected_theme}",
  "default_surface": "${expected_default_surface}",
  "window_start": "${window_start}",
  "window_end": "${window_end}",
  "artifact_dir": "${artifact_dir}"
}
EOF

echo "profile smoke test ok: ${artifact_dir}"
