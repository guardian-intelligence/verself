#!/usr/bin/env bash
set -euo pipefail

# Upload the most recent deploy event JSON to ClickHouse.
# Reuses scripts/clickhouse.sh for SSH + auth + clickhouse-client.
#
# Usage:
#   ./scripts/upload-deploy-event.sh               # latest event
#   ./scripts/upload-deploy-event.sh path/to.json   # specific file

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_EVENT_DIR="${DEPLOY_EVENT_DIR:-${HOME}/.cache/forge-metal/deploy-events}"

if [[ $# -ge 1 ]]; then
  event_file="$1"
else
  event_file="$(ls -1t "${DEPLOY_EVENT_DIR}"/*.json 2>/dev/null | head -1)"
  if [[ -z "$event_file" ]]; then
    echo "deploy_events: no event files in ${DEPLOY_EVENT_DIR}" >&2
    exit 0
  fi
fi

if [[ ! -f "$event_file" ]]; then
  echo "deploy_events: $event_file not found" >&2
  exit 1
fi

# Transform Python JSON to ClickHouse JSONEachRow:
#   - booleans (ok, dirty) → UInt8
#   - nested objects (hosts, slowest_tasks) → JSON strings
row=$(jq -c '{
  deploy_id: .deploy_id,
  playbook: .playbook,
  plays: .plays,
  commit_sha: .commit_sha,
  branch: .branch,
  commit_message: .commit_message,
  author: .author,
  dirty: (if .dirty then 1 else 0 end),
  started_at: .started_at,
  completed_at: .completed_at,
  total_ns: .total_ns,
  ok: (if .ok then 1 else 0 end),
  tasks_ok: .tasks_ok,
  tasks_failed: .tasks_failed,
  tasks_skipped: .tasks_skipped,
  tasks_changed: .tasks_changed,
  tasks_unreachable: .tasks_unreachable,
  task_count: .task_count,
  hosts: (.hosts | tostring),
  slowest_tasks: (.slowest_tasks | tostring),
  ansible_version: .ansible_version
}' "$event_file")

"${SCRIPT_DIR}/clickhouse.sh" \
  --database forge_metal \
  --query "INSERT INTO deploy_events FORMAT JSONEachRow" \
  <<< "$row"

echo "deploy_events: uploaded $(basename "$event_file")"
rm -f "$event_file"
