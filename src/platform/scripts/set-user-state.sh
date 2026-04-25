#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

usage() {
  cat >&2 <<'EOF'
Usage:
  set-user-state.sh --email user@example.com --org platform --state free [--balance-cents 10000]
  set-user-state.sh --email user@example.com --org-id 123 --plan-id sandbox-pro [--business-now 2026-04-13T12:00:00Z]

Sets billing fixture state directly in billing-service PostgreSQL on the target
node. This is an operator/test helper, not a customer API.

Options:
  --email             User billing email to write on orgs.billing_email.
  --org               Billing org id, org display name, or platform shortcut.
  --org-id            Numeric org id. Takes precedence over --org.
  --org-name          Display name when --org-id creates an org row.
  --state             free, hobby, pro, or any active plan tier.
  --plan-id           Exact target plan id; free/none clears paid contracts.
  --product-id        Billing product id. Default: sandbox.
  --balance-units     Exact account purchase balance in ledger units.
  --balance-cents     Exact account purchase balance in cents.
  --business-now      RFC3339/RFC3339Nano org-product billing clock override.
  --overage-policy    Optional org overage policy override.
  --trust-tier        Optional org trust tier override.
EOF
}

email=""
org=""
org_id=""
org_name=""
state=""
plan_id=""
product_id="${BILLING_PRODUCT_ID:-sandbox}"
balance_units=""
balance_cents=""
business_now=""
overage_policy=""
trust_tier=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --email)
      email="${2:-}"
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
    --org-name)
      org_name="${2:-}"
      shift 2
      ;;
    --state)
      state="${2:-}"
      shift 2
      ;;
    --plan-id)
      plan_id="${2:-}"
      shift 2
      ;;
    --product-id)
      product_id="${2:-}"
      shift 2
      ;;
    --balance-units)
      balance_units="${2:-}"
      shift 2
      ;;
    --balance-cents)
      balance_cents="${2:-}"
      shift 2
      ;;
    --business-now)
      business_now="${2:-}"
      shift 2
      ;;
    --overage-policy)
      overage_policy="${2:-}"
      shift 2
      ;;
    --trust-tier)
      trust_tier="${2:-}"
      shift 2
      ;;
    -h | --help)
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

if [[ -z "${email}" ]]; then
  echo "ERROR: --email is required" >&2
  usage
  exit 1
fi
if [[ -z "${org}" && -z "${org_id}" ]]; then
  echo "ERROR: --org or --org-id is required" >&2
  usage
  exit 1
fi
if [[ -z "${state}" && -z "${plan_id}" ]]; then
  state="free"
fi
if [[ -n "${balance_units}" && -n "${balance_cents}" ]]; then
  echo "ERROR: set only one of --balance-units or --balance-cents" >&2
  exit 1
fi

binary_dir="${VERIFICATION_REPO_ROOT}/artifacts/bin"
binary_path="${binary_dir}/billing-set-user-state"
remote_path=""
mkdir -p "${binary_dir}"

(
  cd "${VERIFICATION_REPO_ROOT}/src/billing-service"
  go build -o "${binary_path}" ./cmd/billing-set-user-state
)

remote_path="$(verification_upload_executable "${binary_path}" verself-billing-set-user-state)"
cleanup_remote() {
  verification_remove_remote_path "${remote_path}"
}
trap cleanup_remote EXIT

remote_args=(
  sudo -u billing "${remote_path}"
  --pg-dsn "postgres://billing@/billing?host=/var/run/postgresql&sslmode=disable"
  --email "${email}"
  --product-id "${product_id}"
)

if [[ -n "${org_id}" ]]; then
  remote_args+=(--org-id "${org_id}")
else
  remote_args+=(--org "${org}")
fi
if [[ -n "${org_name}" ]]; then
  remote_args+=(--org-name "${org_name}")
fi
if [[ -n "${state}" ]]; then
  remote_args+=(--state "${state}")
fi
if [[ -n "${plan_id}" ]]; then
  remote_args+=(--plan-id "${plan_id}")
fi
if [[ -n "${balance_units}" ]]; then
  remote_args+=(--balance-units "${balance_units}")
fi
if [[ -n "${balance_cents}" ]]; then
  remote_args+=(--balance-cents "${balance_cents}")
fi
if [[ -n "${business_now}" ]]; then
  remote_args+=(--business-now "${business_now}")
fi
if [[ -n "${overage_policy}" ]]; then
  remote_args+=(--overage-policy "${overage_policy}")
fi
if [[ -n "${trust_tier}" ]]; then
  remote_args+=(--trust-tier "${trust_tier}")
fi

printf -v remote_cmd '%q ' "${remote_args[@]}"
verification_ssh "${remote_cmd}"
