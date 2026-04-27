#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-notifications-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/notifications-proof}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
notifications_log_path="${artifact_dir}/notifications-ui.log"
base_url="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/postgres" "${artifact_dir}/responses"

remote_json_request() {
  local json="$1"
  printf '%s' "${json}" | base64 -w0
}

remote_notifications_api() {
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
    \"http://127.0.0.1:4260\" + payload[\"path\"],
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

verification_print_artifacts "${artifact_dir}" "${notifications_log_path}" "${run_json_path}"
verification_wait_for_http "console UI" "${base_url}" "200"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
machine_identity_token="${IDENTITY_SERVICE_ACCESS_TOKEN}"
expected_email="acme-admin@${VERIFICATION_DOMAIN}"

remote_notifications_api GET "/api/v1/notifications/summary" "${machine_identity_token}" "${artifact_dir}/responses/machine-token-notifications-read.json"
python3 - "${artifact_dir}/responses/machine-token-notifications-read.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
body = payload.get("body", "")
if "human-notification-inbox-required" not in body:
    raise SystemExit("machine token notification read did not fail closed with human-notification-inbox-required")
PY

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
    vp exec playwright test e2e/notifications.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${notifications_log_path}" 2>&1
notifications_status=$?
set -e
window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_tail_log_on_failure "${notifications_status}" "${notifications_log_path}" "180"
if [[ "${notifications_status}" -ne 0 ]]; then
  exit "${notifications_status}"
fi

remote_psql notifications_service "
SELECT notification_id::text, org_id, recipient_subject_id, recipient_sequence, event_source, event_id::text, created_at
FROM user_notifications
WHERE title = 'Notification test'
  AND created_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz + INTERVAL '45 seconds'
ORDER BY created_at DESC
LIMIT 1;
" "${artifact_dir}/postgres/user-notification.tsv"

if ! IFS=$'\t' read -r notification_id org_id recipient_subject_id recipient_sequence event_source event_id created_at <"${artifact_dir}/postgres/user-notification.tsv"; then
  echo "notifications_service.user_notifications did not contain the synthetic notification" >&2
  exit 1
fi
if [[ -z "${notification_id}" || -z "${org_id}" || -z "${recipient_subject_id}" || -z "${event_id}" ]]; then
  echo "notifications_service.user_notifications returned incomplete notification evidence" >&2
  exit 1
fi

remote_psql notifications_service "
SELECT event_source, event_id::text, kind, priority, processed_at IS NOT NULL AS processed
FROM notification_events
WHERE event_source = '${event_source//\'/\'\'}'
  AND event_id = '${event_id//\'/\'\'}'::uuid;
" "${artifact_dir}/postgres/domain-event.tsv"

remote_psql notifications_service "
SELECT event_type, projected_at IS NOT NULL AS projected
FROM notification_projection_queue
WHERE org_id = '${org_id//\'/\'\'}'
  AND recipient_subject_id = '${recipient_subject_id//\'/\'\'}'
  AND (notification_id = '${notification_id//\'/\'\'}'::uuid OR event_id = '${event_id//\'/\'\'}'::uuid)
ORDER BY occurred_at, event_type;
" "${artifact_dir}/postgres/projection-queue.tsv"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM notification_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND org_id = {org_id:String}
    AND recipient_subject_id = {recipient_subject_id:String}
    AND source_event_id = toUUID({event_id:String})
    AND event_type IN ('notification.event.received', 'notification.inbox.created', 'notification.read_cursor_advanced')
" 3 "${artifact_dir}/clickhouse/notification-ledger-count.tsv" \
  --param_org_id="${org_id}" \
  --param_recipient_subject_id="${recipient_subject_id}" \
  --param_event_id="${event_id}"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM notification_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND org_id = {org_id:String}
    AND recipient_subject_id = {recipient_subject_id:String}
    AND event_type = 'notification.inbox.read'
" 1 "${artifact_dir}/clickhouse/notification-read-count.tsv" \
  --param_org_id="${org_id}" \
  --param_recipient_subject_id="${recipient_subject_id}"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM notification_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND org_id = {org_id:String}
    AND recipient_subject_id = {recipient_subject_id:String}
    AND event_type = 'notification.inbox.dismissed'
" 1 "${artifact_dir}/clickhouse/notification-dismissed-count.tsv" \
  --param_org_id="${org_id}" \
  --param_recipient_subject_id="${recipient_subject_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'notifications-service'
    AND SpanName IN (
      'notifications.api.publish-test-notification',
      'notifications.synthetic.publish',
      'notifications.nats.publish',
      'notifications.nats.consume',
      'notifications.event.persist',
      'notifications.event.fanout',
      'notifications.api.advance-notification-read-cursor',
      'notifications.api.mark-notification-read',
      'notifications.inbox.read',
      'notifications.api.clear-notifications',
      'notifications.inbox.clear',
      'notifications.clickhouse.project_pending'
    )
" 12 "${artifact_dir}/clickhouse/notifications-trace-span-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'console'
    AND position(toString(SpanAttributes), '/api/v1/notifications') > 0
" 1 "${artifact_dir}/clickhouse/rent-notifications-boundary-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database verself \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_org_id="${org_id}" \
    --param_recipient_subject_id="${recipient_subject_id}" \
    --query "
      SELECT recorded_at, event_type, org_id, recipient_subject_id, notification_id, recipient_sequence,
             event_source, source_subject, source_event_id, kind, priority, status, reason,
             trace_id, span_id, traceparent
      FROM notification_events
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND org_id = {org_id:String}
        AND recipient_subject_id = {recipient_subject_id:String}
      ORDER BY recorded_at, event_type
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/notification-events.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT Timestamp, ServiceName, SpanName, StatusCode,
             arrayElement(SpanAttributes, 'notification.event_id') AS notification_event_id,
             arrayElement(SpanAttributes, 'notification.id') AS notification_id,
             arrayElement(SpanAttributes, 'messaging.system') AS messaging_system
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName IN ('notifications-service', 'console')
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/otel-traces.tsv"

cat >"${run_json_path}" <<EOF
{
  "run_id": "${run_id}",
  "notification_id": "${notification_id}",
  "org_id": "${org_id}",
  "recipient_subject_id": "${recipient_subject_id}",
  "recipient_sequence": "${recipient_sequence}",
  "event_source": "${event_source}",
  "event_id": "${event_id}",
  "created_at": "${created_at}",
  "window_start": "${window_start}",
  "window_end": "${window_end}",
  "artifact_dir": "${artifact_dir}"
}
EOF

echo "notifications proof ok: ${artifact_dir}"
