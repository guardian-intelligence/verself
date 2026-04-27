#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-console-ui-smoke}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
base_url="${TEST_BASE_URL:-}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
smoke_log_path="${artifact_dir}/shell-smoke.log"

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  local count="0"

  for _ in $(seq 1 60); do
    (
      cd "${VERIFICATION_PLATFORM_ROOT}"
      ./scripts/clickhouse.sh \
        --database "${database}" \
        --param_window_start="${started_at}" \
        --param_window_end="${ended_at}" \
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

if [[ -z "${base_url}" ]]; then
  echo "TEST_BASE_URL is required" >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
verification_print_artifacts "${artifact_dir}" "${smoke_log_path}" "${run_json_path}"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

verification_wait_for_http "console UI" "${base_url}" "200"

ceo_password="$(
  verification_remote_sudo_cat /etc/credstore/seed-system/ceo-password
)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${base_url}" \
  TEST_BASE_URL="${base_url}" \
  VERSELF_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="ceo@${VERIFICATION_DOMAIN}" \
  TEST_PASSWORD="${ceo_password}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/shell.live.spec.ts \
      --project=chromium \
      --grep "authenticated shell" \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${smoke_log_path}" 2>&1
smoke_status=$?
set -e
ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

set +e
verification_collect_run_or_window_evidence "${run_json_path}" "${artifact_dir}/evidence" "${started_at}" "${ended_at}"
evidence_status=$?
set -e

verification_tail_log_on_failure "${smoke_status}" "${smoke_log_path}" "160"

if [[ "${smoke_status}" -eq 0 && "${evidence_status}" -ne 0 ]]; then
  echo "evidence collection failed after successful UI smoke: ${artifact_dir}/evidence" >&2
  exit "${evidence_status}"
fi

if [[ "${smoke_status}" -eq 0 ]]; then
  mkdir -p "${artifact_dir}/clickhouse"

  wait_for_clickhouse_count default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String})
      AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
      AND ServiceName = 'console'
      AND SpanName = 'auth.organization.switch'
      AND arrayElement(SpanAttributes, 'auth.previous_org_id') != ''
      AND arrayElement(SpanAttributes, 'auth.selected_org_id') != ''
      AND arrayElement(SpanAttributes, 'auth.previous_org_id') != arrayElement(SpanAttributes, 'auth.selected_org_id')
  " 1 "${artifact_dir}/clickhouse/auth-organization-switch-count.tsv"

  wait_for_clickhouse_count default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String})
      AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
      AND ServiceName = 'console'
      AND SpanName = 'auth.resource_token.exchange'
      AND arrayElement(SpanAttributes, 'auth.selected_org_id') != ''
      AND arrayElement(SpanAttributes, 'auth.audience') != ''
      AND arrayElement(SpanAttributes, 'auth.cache_hit') = 'false'
      AND toUInt32OrZero(arrayElement(SpanAttributes, 'auth.role_assignment_count')) > 0
  " 2 "${artifact_dir}/clickhouse/auth-resource-token-exchange-count.tsv"

  wait_for_clickhouse_count default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String})
      AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
      AND ServiceName IN ('identity-service', 'sandbox-rental-service')
      AND arrayElement(SpanAttributes, 'auth.selected_org_id') != ''
      AND toUInt32OrZero(arrayElement(SpanAttributes, 'auth.role_assignment_count')) > 0
      AND toUInt32OrZero(arrayElement(SpanAttributes, 'auth.role_assignment_org_count')) = 1
  " 2 "${artifact_dir}/clickhouse/service-auth-selected-org-count.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database default \
      --param_window_start="${started_at}" \
      --param_window_end="${ended_at}" \
      --query "
        SELECT
          Timestamp,
          ServiceName,
          SpanName,
          TraceId,
          arrayElement(SpanAttributes, 'auth.audience') AS auth_audience,
          arrayElement(SpanAttributes, 'auth.previous_org_id') AS previous_org_id,
          arrayElement(SpanAttributes, 'auth.selected_org_id') AS selected_org_id,
          arrayElement(SpanAttributes, 'auth.cache_hit') AS cache_hit,
          arrayElement(SpanAttributes, 'auth.role_assignment_count') AS role_assignment_count
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String})
          AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
          AND (
            SpanName IN ('auth.organization.switch', 'auth.resource_token.exchange')
            OR arrayElement(SpanAttributes, 'auth.selected_org_id') != ''
          )
        ORDER BY Timestamp
        FORMAT TSVWithNames
      "
  ) >"${artifact_dir}/clickhouse/auth-org-scope-traces.tsv"
fi

exit "${smoke_status}"
