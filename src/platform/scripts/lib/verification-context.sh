#!/usr/bin/env bash

verification_context_init() {
  local caller_path="$1"

  VERIFICATION_SCRIPT_DIR="$(cd "$(dirname "${caller_path}")" && pwd)"
  VERIFICATION_REPO_ROOT="$(cd "${VERIFICATION_SCRIPT_DIR}/../../.." && pwd)"
  VERIFICATION_PLATFORM_ROOT="${VERIFICATION_REPO_ROOT}/src/platform"
  VERIFICATION_INVENTORY="${VERIFICATION_PLATFORM_ROOT}/ansible/inventory/hosts.ini"
  VERIFICATION_VARS_FILE="${VERIFICATION_PLATFORM_ROOT}/ansible/group_vars/all/main.yml"

  if [[ ! -f "${VERIFICATION_INVENTORY}" ]]; then
    echo "inventory not found: ${VERIFICATION_INVENTORY}" >&2
    return 1
  fi

  VERIFICATION_DOMAIN="$(awk -F'"' '/^forge_metal_domain:/{print $2}' "${VERIFICATION_VARS_FILE}")"
  VERIFICATION_REMOTE_HOST="$(grep -m1 'ansible_host=' "${VERIFICATION_INVENTORY}" | sed 's/.*ansible_host=\([^ ]*\).*/\1/')"
  VERIFICATION_REMOTE_USER="$(grep -m1 'ansible_user=' "${VERIFICATION_INVENTORY}" | sed 's/.*ansible_user=\([^ ]*\).*/\1/')"
  VERIFICATION_SSH_OPTS=(-o IPQoS=none -o StrictHostKeyChecking=no)

  if [[ -n "${SSH_OPTS:-}" ]]; then
    read -r -a VERIFICATION_SSH_OPTS <<<"${SSH_OPTS}"
  fi

  if [[ -z "${VERIFICATION_DOMAIN}" || -z "${VERIFICATION_REMOTE_HOST}" || -z "${VERIFICATION_REMOTE_USER}" ]]; then
    echo "failed to resolve verification context from inventory/group vars" >&2
    return 1
  fi
}

verification_ssh() {
  # shellcheck disable=SC2029 # Callers pass fully quoted remote commands by design.
  ssh "${VERIFICATION_SSH_OPTS[@]}" \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" "$@"
}

verification_remote_temp_path() {
  local prefix="$1"
  local remote_staging_dir="${VERIFICATION_REMOTE_STAGING_DIR:-/opt/forge-metal/staging}"

  if [[ ! "${prefix}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
    echo "unsafe remote temp prefix: ${prefix}" >&2
    return 1
  fi

  local remote_staging_dir_q
  remote_staging_dir_q="$(printf '%q' "${remote_staging_dir}")"
  verification_ssh "sudo install -d -m 0755 ${remote_staging_dir_q} && sudo mktemp -u ${remote_staging_dir_q}/${prefix}.XXXXXX"
}

verification_upload_executable() {
  local local_path="$1"
  local prefix="$2"
  local remote_path
  local remote_path_q

  remote_path="$(verification_remote_temp_path "${prefix}")"
  remote_path_q="$(printf '%q' "${remote_path}")"
  verification_ssh "sudo tee ${remote_path_q} >/dev/null && sudo chmod 0755 ${remote_path_q}" <"${local_path}"
  printf '%s\n' "${remote_path}"
}

verification_remove_remote_path() {
  local remote_path="$1"
  if [[ -z "${remote_path}" ]]; then
    return 0
  fi
  local remote_path_q
  remote_path_q="$(printf '%q' "${remote_path}")"
  verification_ssh "sudo rm -f ${remote_path_q}" >/dev/null 2>&1 || true
}

verification_remote_sudo_cat() {
  local remote_path="$1"
  verification_ssh "sudo cat '${remote_path}'"
}

verification_print_artifacts() {
  local artifact_dir="$1"
  local log_path="$2"
  local run_json_path="${3:-}"

  echo "artifacts: ${artifact_dir}"
  echo "log: ${log_path}"
  if [[ -n "${run_json_path}" ]]; then
    echo "run json: ${run_json_path}"
  fi
}

verification_tail_log_on_failure() {
  local status="$1"
  local log_path="$2"
  local lines="${3:-160}"

  if [[ "${status}" -eq 0 || ! -f "${log_path}" ]]; then
    return 0
  fi

  echo "verification failed with status ${status}; last ${lines} log lines from ${log_path}:" >&2
  tail -n "${lines}" "${log_path}" >&2 || true
}

verification_collect_window_evidence() {
  local output_dir="$1"
  local window_start="$2"
  local window_end="$3"
  local billing_db="${BILLING_DB:-billing}"

  mkdir -p "${output_dir}/clickhouse" "${output_dir}/postgres"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database default \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          Timestamp,
          ServiceName,
          SeverityText,
          Body,
          toString(LogAttributes) AS attrs
        FROM otel_logs
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
          AND ServiceName IN ('rent-a-sandbox', 'sandbox-rental-service', 'billing-service', 'vm-orchestrator')
        ORDER BY Timestamp
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/otel_logs.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database default \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          Timestamp,
          ServiceName,
          SpanName,
          StatusCode,
          intDiv(Duration, 1000000) AS duration_ms,
          SpanAttributes['http.method'] AS http_method,
          SpanAttributes['http.target'] AS http_target,
          SpanAttributes['http.status_code'] AS http_status_code
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
          AND ServiceName IN ('rent-a-sandbox', 'sandbox-rental-service', 'billing-service', 'vm-orchestrator')
        ORDER BY Timestamp
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/otel_traces.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          event_id,
          event_type,
          aggregate_type,
          aggregate_id,
          contract_id,
          cycle_id,
          finalization_id,
          document_id,
          org_id,
          product_id,
          occurred_at,
          payload,
          recorded_at
        FROM billing_events
        WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
        ORDER BY recorded_at, event_id
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/billing_events.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          execution_id,
          attempt_id,
          org_id,
          source_kind,
          workload_kind,
          runner_class,
          repository_full_name,
          workflow_name,
          job_name,
          head_branch,
          schedule_id,
          status,
          duration_ms,
          reserved_charge_units,
          billed_charge_units,
          writeoff_charge_units,
          rootfs_provisioned_bytes,
          boot_time_us,
          block_write_bytes,
          net_tx_bytes,
          trace_id,
          created_at
        FROM job_events
        WHERE created_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
        ORDER BY created_at, execution_id
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/job_events.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          execution_id,
          attempt_id,
          org_id,
          source_kind,
          runner_class,
          repository_full_name,
          workflow_name,
          job_name,
          head_branch,
          schedule_id,
          seq,
          stream,
          chunk,
          created_at
        FROM job_logs
        WHERE created_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
        ORDER BY created_at, execution_id, attempt_id, seq
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/job_logs.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          org_id,
          product_id,
          source_type,
          source_ref,
          reservation_shape,
          reserved_quantity,
          actual_quantity,
          billable_quantity,
          charge_units,
          writeoff_charge_units,
          cost_per_unit,
          trace_id,
          started_at,
          ended_at,
          recorded_at
        FROM metering
        WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
        ORDER BY recorded_at
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/metering.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/clickhouse.sh \
      --database forge_metal \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT
          lease_id,
          exec_id,
          trace_id,
          service_name,
          evidence_type,
          diagnostic_kind,
          reason_code,
          reason,
          expected_seq,
          observed_seq,
          missing_samples,
          evidence_time
        FROM vm_lease_evidence
        WHERE evidence_time BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String})
        ORDER BY evidence_time, lease_id, exec_id
        FORMAT TSVWithNames
      "
  ) >"${output_dir}/clickhouse/vm_lease_evidence.tsv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/pg.sh "${billing_db}" --query "
      COPY (
        SELECT
          cycle_id,
          org_id,
          product_id,
          cadence_kind,
          status,
          starts_at,
          ends_at,
          finalized_at,
          created_at
        FROM billing_cycles
        WHERE created_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
           OR starts_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
           OR ends_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
        ORDER BY starts_at, cycle_id
      ) TO STDOUT WITH CSV HEADER;
    "
  ) >"${output_dir}/postgres/billing_cycles.csv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/pg.sh "${billing_db}" --query "
      COPY (
        SELECT
          finalization_id,
          subject_type,
          subject_id,
          COALESCE(cycle_id, '') AS cycle_id,
          COALESCE(document_id, '') AS document_id,
          document_kind,
          state,
          customer_visible,
          has_usage,
          has_financial_activity,
          started_at,
          completed_at,
          created_at
        FROM billing_finalizations
        WHERE created_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
           OR started_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
        ORDER BY started_at, finalization_id
      ) TO STDOUT WITH CSV HEADER;
    "
  ) >"${output_dir}/postgres/billing_finalizations.csv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/pg.sh "${billing_db}" --query "
      COPY (
        SELECT
          document_id,
          COALESCE(document_number, '') AS document_number,
          document_kind,
          COALESCE(finalization_id, '') AS finalization_id,
          COALESCE(cycle_id, '') AS cycle_id,
          status,
          payment_status,
          period_start,
          period_end,
          issued_at,
          total_due_units,
          created_at
        FROM billing_documents
        WHERE created_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
           OR issued_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
        ORDER BY period_start, document_id
      ) TO STDOUT WITH CSV HEADER;
    "
  ) >"${output_dir}/postgres/billing_documents.csv"

  (
    cd "${VERIFICATION_PLATFORM_ROOT}" || return
    ./scripts/pg.sh "${billing_db}" --query "
      COPY (
        SELECT
          scope_kind,
          scope_id,
          business_now,
          reason,
          updated_at
        FROM billing_clock_overrides
        WHERE updated_at BETWEEN '${window_start}'::timestamptz AND '${window_end}'::timestamptz
        ORDER BY updated_at, scope_kind, scope_id
      ) TO STDOUT WITH CSV HEADER;
    "
  ) >"${output_dir}/postgres/billing_clock_overrides.csv"
}

verification_collect_run_or_window_evidence() {
  local run_json_path="$1"
  local output_dir="$2"
  local window_start="$3"
  local window_end="$4"

  if [[ -f "${run_json_path}" ]]; then
    window_start="$(python3 -c 'import json, sys; print((json.load(open(sys.argv[1], encoding="utf-8")).get("submitted_at") or sys.argv[2]))' "${run_json_path}" "${window_start}")"
    window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  else
    echo "run json not found; collecting fallback evidence for ${window_start}..${window_end}" >&2
  fi
  verification_collect_window_evidence "${output_dir}" "${window_start}" "${window_end}"
}

verification_wait_for_http() {
  local name="$1"
  local url="$2"
  local expected_status="${3:-200}"

  for _ in $(seq 1 60); do
    local code
    code="$(curl -k -L -s -o /dev/null -w '%{http_code}' "${url}" || true)"
    if [[ "${code}" == "${expected_status}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "${name} did not return ${expected_status} in time: ${url}" >&2
  return 1
}

verification_wait_for_loopback_api() {
  local name="$1"
  local url="$2"
  local expected_status="$3"

  verification_ssh \
    "for _ in \$(seq 1 60); do \
       code=\$(curl -s -o /dev/null -w '%{http_code}' '${url}' || true); \
       if [[ \"\${code}\" == '${expected_status}' ]]; then exit 0; fi; \
       sleep 1; \
     done; \
     echo '${name} did not return ${expected_status} in time' >&2; \
     exit 1"
}

verification_source_env_file_if_present() {
  local env_file="$1"

  if [[ -f "${env_file}" ]]; then
    # shellcheck disable=SC1090
    source "${env_file}"
  fi
}
