#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

usage() {
  cat >&2 <<'USAGE'
Usage:
  billing-inspect.sh --kind state --org platform [--product-id sandbox]
  billing-inspect.sh --kind documents --org-id 123 [--product-id sandbox]
  billing-inspect.sh --kind finalizations --org platform [--product-id sandbox]
  billing-inspect.sh --kind events [--org platform|--org-id 123] [--event-type billing_document_issued] [--minutes 60]
USAGE
}

kind="state"
org=""
org_id=""
product_id="${BILLING_PRODUCT_ID:-sandbox}"
event_type=""
minutes="60"
limit="100"
format="table"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --kind)
      kind="${2:-}"
      shift 2
      ;;
    --org)
      org="${2:-}"
      shift 2
      ;;
    --org-id)
      org_id="${2:-}"
      shift 2
      ;;
    --product-id)
      product_id="${2:-}"
      shift 2
      ;;
    --event-type)
      event_type="${2:-}"
      shift 2
      ;;
    --minutes)
      minutes="${2:-}"
      shift 2
      ;;
    --limit)
      limit="${2:-}"
      shift 2
      ;;
    --format)
      format="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    "")
      shift
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

case "${kind}" in
  state|documents|finalizations|events) ;;
  *) echo "ERROR: unsupported --kind ${kind}" >&2; usage; exit 1 ;;
esac
case "${format}" in
  table|csv|tsv) ;;
  *) echo "ERROR: --format must be table, csv, or tsv" >&2; exit 1 ;;
esac
if ! [[ "${minutes}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: --minutes must be a positive integer" >&2
  exit 1
fi
if ! [[ "${limit}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: --limit must be a positive integer" >&2
  exit 1
fi
if (( limit > 1000 )); then
  echo "ERROR: --limit must be <= 1000" >&2
  exit 1
fi
if [[ -n "${event_type}" && ! "${event_type}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
  echo "ERROR: --event-type must contain only letters, numbers, dot, underscore, or dash" >&2
  exit 1
fi
if [[ -n "${product_id}" && ! "${product_id}" =~ ^[A-Za-z0-9_.-]+$ ]]; then
  echo "ERROR: --product-id must contain only letters, numbers, dot, underscore, or dash" >&2
  exit 1
fi
if [[ -n "${org_id}" && ! "${org_id}" =~ ^[1-9][0-9]*$ ]]; then
  echo "ERROR: --org-id must be an unsigned integer" >&2
  exit 1
fi

psql_scalar() {
  local sql="$1"
  "${script_dir}/pg.sh" billing -X -A -t -P footer=off -c "${sql}" | tr -d '[:space:]'
}

psql_query() {
  local sql="$1"
  case "${format}" in
    csv)
      "${script_dir}/pg.sh" billing -X -A -P footer=off -c "COPY (${sql}) TO STDOUT WITH (FORMAT csv, HEADER true)"
      ;;
    tsv)
      "${script_dir}/pg.sh" billing -X -A -t -F $'\t' -P footer=off -c "${sql}"
      ;;
    table)
      "${script_dir}/pg.sh" billing --query "${sql}"
      ;;
  esac
}

sql_literal() {
  local value="$1"
  value="${value//\'/\'\'}"
  printf "'%s'" "${value}"
}

resolve_org_id() {
  if [[ -n "${org_id}" ]]; then
    printf '%s\n' "${org_id}"
    return 0
  fi
  if [[ -z "${org}" ]]; then
    if [[ "${kind}" == "events" ]]; then
      printf '\n'
      return 0
    fi
    echo "ERROR: --org or --org-id is required" >&2
    exit 1
  fi
  if [[ "${org}" =~ ^[1-9][0-9]*$ ]]; then
    printf '%s\n' "${org}"
    return 0
  fi
  local sql
  if [[ "${org}" == "platform" ]]; then
    sql="SELECT string_agg(org_id, ',') FROM (SELECT org_id FROM orgs WHERE trust_tier = 'platform' ORDER BY created_at, org_id LIMIT 2) s"
  else
    local quoted
    quoted="$(sql_literal "${org}")"
    sql="SELECT string_agg(org_id, ',') FROM (SELECT org_id FROM orgs WHERE display_name = ${quoted} OR metadata->>'org_key' = ${quoted} OR billing_email = ${quoted} ORDER BY created_at, org_id LIMIT 2) s"
  fi
  local matches
  matches="$(psql_scalar "${sql}")"
  if [[ -z "${matches}" ]]; then
    echo "ERROR: org ${org} not found" >&2
    exit 1
  fi
  if [[ "${matches}" == *,* ]]; then
    echo "ERROR: org ${org} matched multiple billing orgs; pass ORG_ID" >&2
    exit 1
  fi
  printf '%s\n' "${matches}"
}

resolved_org_id="$(resolve_org_id)"
product_literal="$(sql_literal "${product_id}")"
org_literal="$(sql_literal "${resolved_org_id}")"

case "${kind}" in
  state)
    cat <<EOF2
== billing state ==
org_id=${resolved_org_id}
product_id=${product_id}
EOF2
    psql_query "
      SELECT org_id, display_name, billing_email, state, trust_tier, overage_policy, overage_consent_at, created_at, updated_at
      FROM orgs
      WHERE org_id = ${org_literal}
    "
    psql_query "
      SELECT scope_kind, scope_id, business_now, reason, generation, updated_at
      FROM billing_clock_overrides
      WHERE scope_id IN (${org_literal} || ':' || ${product_literal}, ${org_literal})
      ORDER BY scope_kind, scope_id
    "
    psql_query "
      SELECT cycle_id, status, cadence_kind, starts_at, ends_at, finalization_due_at, active_finalization_id, metadata
      FROM billing_cycles
      WHERE org_id = ${org_literal}
        AND product_id = ${product_literal}
      ORDER BY starts_at DESC, cycle_id DESC
      LIMIT ${limit}
    "
    psql_query "
      SELECT c.contract_id, c.state, c.payment_state, c.entitlement_state, c.starts_at, c.cancel_at, p.phase_id, p.plan_id, p.state AS phase_state, p.effective_start, p.effective_end
      FROM contracts c
      LEFT JOIN contract_phases p ON p.contract_id = c.contract_id AND p.state IN ('active','grace','scheduled')
      WHERE c.org_id = ${org_literal}
        AND c.product_id = ${product_literal}
      ORDER BY c.created_at DESC, p.effective_start DESC NULLS LAST
      LIMIT ${limit}
    "
    psql_query "
      SELECT source, scope_type, COALESCE(scope_product_id,''), COALESCE(scope_bucket_id,''), COALESCE(scope_sku_id,''), ledger_posting_state, count(*) AS grants, sum(amount) AS total_units
      FROM credit_grants
      WHERE org_id = ${org_literal}
        AND closed_at IS NULL
        AND (${product_literal} = '' OR COALESCE(scope_product_id, ${product_literal}) = ${product_literal} OR scope_type = 'account')
      GROUP BY source, scope_type, COALESCE(scope_product_id,''), COALESCE(scope_bucket_id,''), COALESCE(scope_sku_id,''), ledger_posting_state
      ORDER BY source, scope_type, 3, 4, 5, ledger_posting_state
    "
    ;;
  documents)
    psql_query "
      SELECT document_id, COALESCE(document_number,'') AS document_number, document_kind, COALESCE(finalization_id,'') AS finalization_id, COALESCE(cycle_id,'') AS cycle_id, status, payment_status, period_start, period_end, issued_at, total_due_units, COALESCE(stripe_hosted_invoice_url,'') AS stripe_hosted_invoice_url, COALESCE(stripe_invoice_pdf_url,'') AS stripe_invoice_pdf_url
      FROM billing_documents
      WHERE org_id = ${org_literal}
        AND (${product_literal} = '' OR product_id = ${product_literal})
      ORDER BY period_start DESC, issued_at DESC NULLS LAST, document_id DESC
      LIMIT ${limit}
    "
    ;;
  finalizations)
    psql_query "
      SELECT finalization_id, subject_type, subject_id, COALESCE(cycle_id,'') AS cycle_id, COALESCE(document_id,'') AS document_id, document_kind, state, customer_visible, has_usage, has_financial_activity, started_at, completed_at, last_error
      FROM billing_finalizations
      WHERE org_id = ${org_literal}
        AND (${product_literal} = '' OR product_id = ${product_literal})
      ORDER BY started_at DESC, finalization_id DESC
      LIMIT ${limit}
    "
    ;;
  events)
    event_format="PrettyCompact"
    if [[ "${format}" == "csv" ]]; then
      event_format="CSVWithNames"
    elif [[ "${format}" == "tsv" ]]; then
      event_format="TSVWithNames"
    fi
    "${script_dir}/clickhouse.sh" \
      --database verself \
      --param_org_id="${resolved_org_id}" \
      --param_product_id="${product_id}" \
      --param_event_type="${event_type}" \
      --param_minutes="${minutes}" \
      --param_row_limit="${limit}" \
      --query "
        SELECT event_id, event_type, aggregate_type, aggregate_id, org_id, product_id, occurred_at, recorded_at, payload
        FROM verself.billing_events
        WHERE recorded_at > now() - toIntervalMinute({minutes:UInt32})
          AND ({org_id:String} = '' OR org_id = {org_id:String})
          AND ({product_id:String} = '' OR product_id = {product_id:String})
          AND ({event_type:String} = '' OR event_type = {event_type:String})
        ORDER BY recorded_at DESC, event_id DESC
        LIMIT {row_limit:UInt32}
        FORMAT ${event_format}
      "
    ;;
esac
