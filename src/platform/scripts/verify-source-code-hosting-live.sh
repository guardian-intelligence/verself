#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-source-code-hosting-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/source-code-hosting-proof}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
source_log_path="${artifact_dir}/source-ui.log"
source_api_base_url="${SOURCE_CODE_HOSTING_PROOF_BASE_URL:-https://source.api.${VERIFICATION_DOMAIN}}"
source_api_base_url="${source_api_base_url%/}"
console_base_url="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
clickhouse_timeout_seconds="${SOURCE_CODE_HOSTING_PROOF_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

remote_json_request() {
  local json="$1"
  printf '%s' "${json}" | base64 -w0
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

api_request() {
  local method="$1"
  local path="$2"
  local output_path="$3"
  local body_path="${4:-}"
  local idempotency_key="${5:-}"
  local curl_args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${source_access_token}"
    -H "Accept: application/json"
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
  curl "${curl_args[@]}" "${source_api_base_url}${path}" >"${output_path}"
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

signed_forgejo_webhook() {
  local output_path="$1"
  local provider_owner="$2"
  local provider_repo="$3"
  local provider_repo_id="$4"
  local body_b64
  body_b64="$(
    RUN_ID="${run_id}" PROVIDER_OWNER="${provider_owner}" PROVIDER_REPO="${provider_repo}" PROVIDER_REPO_ID="${provider_repo_id}" python3 - <<'PY'
import base64
import json
import os

repo_id = int(os.environ["PROVIDER_REPO_ID"]) if os.environ["PROVIDER_REPO_ID"] else 0
body = json.dumps({
    "proof_run_id": os.environ["RUN_ID"],
    "repository": {
        "id": repo_id,
        "name": os.environ["PROVIDER_REPO"],
        "full_name": f"{os.environ['PROVIDER_OWNER']}/{os.environ['PROVIDER_REPO']}",
        "owner": {"username": os.environ["PROVIDER_OWNER"]},
    },
}, sort_keys=True).encode()
print(base64.b64encode(body).decode())
PY
  )"
  printf '%s\n' "${body_b64}" | verification_ssh "python3 -c '
import base64
import hashlib
import hmac
import json
import subprocess
import sys
import urllib.error
import urllib.request

body = base64.b64decode(sys.stdin.readline().strip())
payload = json.loads(body.decode())
secret = subprocess.check_output(
    [\"sudo\", \"cat\", \"/etc/credstore/source-code-hosting-service/webhook-secret\"],
    text=True,
).strip().encode()
signature = \"sha256=\" + hmac.new(secret, body, hashlib.sha256).hexdigest()
delivery = \"source-proof-\" + payload[\"proof_run_id\"]
request = urllib.request.Request(
    \"http://127.0.0.1:4261/webhooks/forgejo\",
    data=body,
    method=\"POST\",
    headers={
        \"Content-Type\": \"application/json\",
        \"X-Forgejo-Delivery\": delivery,
        \"X-Forgejo-Event\": \"repository\",
        \"X-Forgejo-Signature\": signature,
    },
)
try:
    with urllib.request.urlopen(request, timeout=3) as response:
        status_code = response.status
        response.read()
except urllib.error.HTTPError as error:
    status_code = error.code
json.dump({\"status\": status_code}, sys.stdout, sort_keys=True)
print()
'" >"${output_path}"
}

verification_print_artifacts "${artifact_dir}" "${source_log_path}" "${run_json_path}"
verification_wait_for_loopback_api "source-code-hosting-service" "http://127.0.0.1:4261/readyz" "200"
verification_wait_for_http "source API auth boundary" "${source_api_base_url}/api/v1/repos" "401"
verification_wait_for_http "console UI" "${console_base_url}" "200"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
source_access_token="${SOURCE_CODE_HOSTING_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${console_base_url}" \
  TEST_BASE_URL="${console_base_url}" \
  FORGE_METAL_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="${TEST_EMAIL}" \
  TEST_PASSWORD="${TEST_PASSWORD}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/source.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${source_log_path}" 2>&1
source_status=$?
set -e

verification_tail_log_on_failure "${source_status}" "${source_log_path}" "180"
if [[ "${source_status}" -ne 0 ]]; then
  exit "${source_status}"
fi

repo_id="$(
  python3 - "${run_json_path}" <<'PY'
import json
import re
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
repo_id = payload.get("repo_id") or ""
if not re.fullmatch(r"[0-9a-fA-F-]{36}", repo_id):
    raise SystemExit("source proof run json did not include repo_id")
print(repo_id)
PY
)"

api_request "GET" "/api/v1/repos" "${artifact_dir}/responses/list-repositories.json"
api_request "GET" "/api/v1/repos/${repo_id}" "${artifact_dir}/responses/get-repository.json"
api_request "GET" "/api/v1/repos/${repo_id}/refs" "${artifact_dir}/responses/list-refs.json"
api_request "GET" "/api/v1/repos/${repo_id}/tree?ref=main" "${artifact_dir}/responses/get-tree.json"

cat >"${artifact_dir}/payloads/create-checkout-grant.json" <<'EOF'
{
  "ref": "main"
}
EOF
checkout_raw_path="$(mktemp)"
trap 'rm -f "${checkout_raw_path}"' EXIT
api_request "POST" "/api/v1/repos/${repo_id}/checkout-grants" "${checkout_raw_path}" "${artifact_dir}/payloads/create-checkout-grant.json" "source-checkout:${run_id}"
read -r checkout_grant_id <<<"$(
  python3 - "${checkout_raw_path}" "${artifact_dir}/responses/create-checkout-grant.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
token = payload.pop("token", "")
if not token:
    raise SystemExit("checkout grant response did not include token")
payload["token"] = "[redacted]"
json.dump(payload, open(sys.argv[2], "w", encoding="utf-8"), indent=2, sort_keys=True)
print(payload["grant_id"])
PY
)"

cat >"${artifact_dir}/payloads/create-integration.json" <<EOF
{
  "provider": "github",
  "external_repo": "forge-metal/source-proof-${run_id}",
  "credential_ref": ""
}
EOF
api_request "POST" "/api/v1/integrations" "${artifact_dir}/responses/create-integration.json" "${artifact_dir}/payloads/create-integration.json" "source-integration:${run_id}"
integration_id="$(
  python3 - "${artifact_dir}/responses/create-integration.json" <<'PY'
import json
import sys

print(json.load(open(sys.argv[1], encoding="utf-8"))["integration_id"])
PY
)"

escaped_repo_id="${repo_id//\'/\'\'}"
escaped_grant_id="${checkout_grant_id//\'/\'\'}"
escaped_integration_id="${integration_id//\'/\'\'}"

remote_psql source_code_hosting "
SELECT repo_id, org_id, name, slug, default_branch, visibility, state, provider, provider_owner, provider_repo, provider_repo_id
FROM source_repositories
WHERE repo_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/repository.tsv"

org_id="$(cut -f2 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
if [[ -z "${org_id}" ]]; then
  echo "source_repositories did not contain the proof repo" >&2
  exit 1
fi
provider="$(cut -f8 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
provider_owner="$(cut -f9 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
provider_repo="$(cut -f10 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
provider_repo_id="$(cut -f11 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
if [[ "${provider}" != "forgejo" || -z "${provider_owner}" || -z "${provider_repo}" || -z "${provider_repo_id}" ]]; then
  echo "source_repositories row did not include provider-neutral Forgejo coordinates" >&2
  exit 1
fi

signed_forgejo_webhook "${artifact_dir}/responses/forgejo-webhook.json" "${provider_owner}" "${provider_repo}" "${provider_repo_id}"
python3 - "${artifact_dir}/responses/forgejo-webhook.json" <<'PY'
import json
import sys

status = json.load(open(sys.argv[1], encoding="utf-8")).get("status")
if status != 202:
    raise SystemExit(f"expected Forgejo webhook proof to return 202, got {status}")
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

remote_psql source_code_hosting "
SELECT grant_id, repo_id, org_id, actor_id, ref, path_prefix, expires_at, consumed_at IS NOT NULL AS consumed
FROM source_checkout_grants
WHERE grant_id = '${escaped_grant_id}'::uuid
  AND repo_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/checkout-grant.tsv"
if [[ ! -s "${artifact_dir}/postgres/checkout-grant.tsv" ]]; then
  echo "source_checkout_grants did not contain the proof grant" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT integration_id, org_id, provider, external_repo, state
FROM source_external_integrations
WHERE integration_id = '${escaped_integration_id}'::uuid;
" "${artifact_dir}/postgres/integration.tsv"
if [[ ! -s "${artifact_dir}/postgres/integration.tsv" ]]; then
  echo "source_external_integrations did not contain the proof integration" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT event_type, result, trace_id
FROM source_events
WHERE (
    repo_id = '${escaped_repo_id}'::uuid
    AND event_type IN ('source.repo.created', 'source.checkout_grant.created')
  )
  OR (
    event_type = 'source.integration.created'
    AND details->>'integration_id' = '${escaped_integration_id}'
  )
  OR (
    event_type = 'source.webhook.repository'
    AND details->>'delivery' = 'source-proof-${run_id}'
  )
ORDER BY created_at, event_type;
" "${artifact_dir}/postgres/source-events.tsv"
python3 - "${artifact_dir}/postgres/source-events.tsv" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
events = {row[0] for row in rows if row}
missing = {
    "source.repo.created",
    "source.checkout_grant.created",
    "source.integration.created",
    "source.webhook.repository",
} - events
if missing:
    raise SystemExit("missing source event rows: " + ", ".join(sorted(missing)))
if any(row[1] != "allowed" for row in rows if row):
    raise SystemExit("expected all source proof event rows to be allowed")
PY

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.repo.create', 'source.repo.list', 'source.repo.read', 'source.refs.list', 'source.tree.get', 'source.checkout_grant.create', 'source.integration.create')
" 7 "${artifact_dir}/clickhouse/source-business-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.forgejo.repo.create', 'source.forgejo.refs.list', 'source.forgejo.contents.get')
" 3 "${artifact_dir}/clickhouse/source-forgejo-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.pg.repo.create', 'source.pg.repo.list', 'source.pg.repo.get', 'source.pg.checkout_grant.create', 'source.pg.integration.create')
" 5 "${artifact_dir}/clickhouse/source-postgres-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.webhook.receive', 'source.webhook.apply')
" 2 "${artifact_dir}/clickhouse/source-webhook-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'console'
    AND SpanName <> ''
" 1 "${artifact_dir}/clickhouse/console-source-ui-spans-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT count()
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName = 'source-code-hosting-service'
        AND (
          positionCaseInsensitive(toString(SpanAttributes), 'bearer ') > 0
          OR positionCaseInsensitive(toString(SpanAttributes), 'forgejo-token') > 0
          OR positionCaseInsensitive(toString(SpanAttributes), 'checkout-token') > 0
        )
    "
) >"${artifact_dir}/clickhouse/source-secret-span-leak-count.tsv"
secret_span_leaks="$(tail -n 1 "${artifact_dir}/clickhouse/source-secret-span-leak-count.tsv" | tr -d '[:space:]')"
if [[ "${secret_span_leaks}" != "0" ]]; then
  echo "source proof found token-like material in source-code-hosting-service span attributes" >&2
  exit 1
fi

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
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
        arrayElement(SpanAttributes, 'source.operation_id') AS operation_id,
        arrayElement(SpanAttributes, 'source.outcome') AS source_outcome,
        arrayElement(SpanAttributes, 'source.repo_id') AS source_repo_id,
        arrayElement(SpanAttributes, 'source.forgejo_repo') AS forgejo_repo
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName IN ('source-code-hosting-service', 'console')
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/otel-traces.tsv"

python3 - "${run_json_path}" "${org_id}" "${checkout_grant_id}" "${integration_id}" "${window_start}" "${window_end}" "${artifact_dir}" <<'PY'
import json
import sys

path, org_id, grant_id, integration_id, window_start, window_end, artifact_dir = sys.argv[1:8]
payload = json.load(open(path, encoding="utf-8"))
payload.update({
    "org_id": org_id,
    "checkout_grant_id": grant_id,
    "integration_id": integration_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
})
json.dump(payload, open(path, "w", encoding="utf-8"), indent=2, sort_keys=True)
print()
PY

echo "source-code-hosting proof ok: ${artifact_dir}"
