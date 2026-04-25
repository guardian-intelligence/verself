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
git_origin="${SOURCE_CODE_HOSTING_PROOF_GIT_ORIGIN:-https://git.${VERIFICATION_DOMAIN}}"
git_origin="${git_origin%/}"
clickhouse_timeout_seconds="${SOURCE_CODE_HOSTING_PROOF_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/payloads" "${artifact_dir}/postgres" "${artifact_dir}/responses"

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
  local backend_owner="$2"
  local backend_repo="$3"
  local backend_repo_id="$4"
  local body_b64
  body_b64="$(
    RUN_ID="${run_id}" BACKEND_OWNER="${backend_owner}" BACKEND_REPO="${backend_repo}" BACKEND_REPO_ID="${backend_repo_id}" python3 - <<'PY'
import base64
import json
import os

repo_id = int(os.environ["BACKEND_REPO_ID"]) if os.environ["BACKEND_REPO_ID"] else 0
body = json.dumps({
    "proof_run_id": os.environ["RUN_ID"],
    "repository": {
        "id": repo_id,
        "name": os.environ["BACKEND_REPO"],
        "full_name": f"{os.environ['BACKEND_OWNER']}/{os.environ['BACKEND_REPO']}",
        "owner": {"username": os.environ["BACKEND_OWNER"]},
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
verification_wait_for_http "git origin is headless" "${git_origin}/" "404"
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

repo_slug="source-proof-$(printf '%s' "${run_id}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '-' | sed -E 's/^-+|-+$//g' | cut -c1-48)"
if [[ -z "${repo_slug}" ]]; then
  repo_slug="source-proof"
fi

cat >"${artifact_dir}/payloads/create-git-credential.json" <<EOF
{
  "label": "source proof ${run_id}",
  "expires_in_seconds": 3600
}
EOF
credential_raw_path="$(mktemp)"
checkout_raw_path="$(mktemp)"
git_workdir="$(mktemp -d)"
askpass_path="$(mktemp)"
trap 'rm -f "${credential_raw_path}" "${checkout_raw_path}" "${askpass_path}"; rm -rf "${git_workdir}"' EXIT

api_request "POST" "/api/v1/git-credentials" "${credential_raw_path}" "${artifact_dir}/payloads/create-git-credential.json" "source-git-credential:${run_id}"
read -r credential_id org_path git_username git_token <<<"$(
  python3 - "${credential_raw_path}" "${artifact_dir}/responses/create-git-credential.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
token = payload.pop("token", "")
if not token:
    raise SystemExit("git credential response did not include token")
payload["token"] = "[redacted]"
json.dump(payload, open(sys.argv[2], "w", encoding="utf-8"), indent=2, sort_keys=True)
print(payload["credential_id"], payload["org_path"], payload["username"], token)
PY
)"

git_repo_url="${git_origin}/${org_path}/${repo_slug}.git"
cat >"${askpass_path}" <<'SH'
#!/usr/bin/env bash
case "$1" in
  *Username*) printf '%s\n' "${SOURCE_GIT_USERNAME}" ;;
  *Password*) printf '%s\n' "${SOURCE_GIT_TOKEN}" ;;
  *) printf '\n' ;;
esac
SH
chmod 700 "${askpass_path}"

git -C "${git_workdir}" init -b main >/dev/null
git -C "${git_workdir}" config user.name "Forge Metal Source Proof"
git -C "${git_workdir}" config user.email "source-proof@forge-metal.invalid"
printf '# Source proof\n\nrun: %s\n' "${run_id}" >"${git_workdir}/README.md"
mkdir -p "${git_workdir}/.forgejo/workflows"
cat >"${git_workdir}/.forgejo/workflows/proof.yml" <<'EOF'
name: proof
on:
  workflow_dispatch:
jobs:
  proof:
    runs-on: metal-4vcpu-ubuntu-2404
    steps:
      - run: echo source proof
EOF
git -C "${git_workdir}" add README.md .forgejo/workflows/proof.yml
git -C "${git_workdir}" commit -m "source proof ${run_id}" >/dev/null
env \
  GIT_ASKPASS="${askpass_path}" \
  GIT_TERMINAL_PROMPT=0 \
  SOURCE_GIT_USERNAME="${git_username}" \
  SOURCE_GIT_TOKEN="${git_token}" \
  git -C "${git_workdir}" push "${git_repo_url}" main:main >"${artifact_dir}/responses/git-push.log" 2>&1

api_request "GET" "/api/v1/repos" "${artifact_dir}/responses/list-repositories.json"
repo_id="$(
  python3 - "${artifact_dir}/responses/list-repositories.json" "${repo_slug}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
slug = sys.argv[2]
for repo in payload.get("repositories") or []:
    if repo.get("slug") == slug:
        print(repo["repo_id"])
        raise SystemExit(0)
raise SystemExit(f"source proof repo with slug {slug!r} not found")
PY
)"

api_request "GET" "/api/v1/repos/${repo_id}" "${artifact_dir}/responses/get-repository.json"
api_request "GET" "/api/v1/repos/${repo_id}/refs" "${artifact_dir}/responses/list-refs.json"
api_request "GET" "/api/v1/repos/${repo_id}/ci-runs" "${artifact_dir}/responses/list-ci-runs.json"
api_request "GET" "/api/v1/repos/${repo_id}/tree?ref=main" "${artifact_dir}/responses/get-tree.json"

cat >"${artifact_dir}/payloads/create-checkout-grant.json" <<'EOF'
{
  "ref": "main"
}
EOF
api_request "POST" "/api/v1/repos/${repo_id}/checkout-grants" "${checkout_raw_path}" "${artifact_dir}/payloads/create-checkout-grant.json" "source-checkout:${run_id}"
checkout_grant_id="$(
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

escaped_repo_id="${repo_id//\'/\'\'}"
escaped_credential_id="${credential_id//\'/\'\'}"
escaped_grant_id="${checkout_grant_id//\'/\'\'}"

remote_psql source_code_hosting "
SELECT repo_id, org_id, org_path, name, slug, default_branch, visibility, state, last_pushed_at IS NOT NULL AS pushed
FROM source_repositories
WHERE repo_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/repository.tsv"

org_id="$(cut -f2 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
if [[ -z "${org_id}" ]]; then
  echo "source_repositories did not contain the proof repo" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT backend_id, repo_id, backend, backend_owner, backend_repo, backend_repo_id, state
FROM source_repository_backends
WHERE repo_id = '${escaped_repo_id}'::uuid
  AND backend = 'forgejo'
  AND state = 'active';
" "${artifact_dir}/postgres/repository-backend.tsv"
if [[ ! -s "${artifact_dir}/postgres/repository-backend.tsv" ]]; then
  echo "source_repository_backends did not contain the proof Forgejo backend" >&2
  exit 1
fi
backend_owner="$(cut -f4 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"
backend_repo="$(cut -f5 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"
backend_repo_id="$(cut -f6 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"

remote_psql source_code_hosting "
SELECT credential_id, org_id, org_path, actor_id, username, token_prefix, state, expires_at > now() AS unexpired, last_used_at IS NOT NULL AS used
FROM source_git_credentials
WHERE credential_id = '${escaped_credential_id}'::uuid;
" "${artifact_dir}/postgres/git-credential.tsv"
if [[ ! -s "${artifact_dir}/postgres/git-credential.tsv" ]]; then
  echo "source_git_credentials did not contain the proof credential" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT ref_name, commit_sha, is_default
FROM source_ref_heads
WHERE repo_id = '${escaped_repo_id}'::uuid
ORDER BY ref_name;
" "${artifact_dir}/postgres/ref-heads.tsv"
if ! grep -q $'^main\t' "${artifact_dir}/postgres/ref-heads.tsv"; then
  echo "source_ref_heads did not contain main" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT ci_run_id, actor_id, ref_name, commit_sha, trigger_event, state,
       COALESCE(sandbox_execution_id::text, ''), COALESCE(sandbox_attempt_id::text, '')
FROM source_ci_runs
WHERE repo_id = '${escaped_repo_id}'::uuid
ORDER BY created_at DESC;
" "${artifact_dir}/postgres/ci-runs.tsv"
if ! grep -q $'\tmain\t' "${artifact_dir}/postgres/ci-runs.tsv"; then
  echo "source_ci_runs did not contain queued main CI run" >&2
  exit 1
fi
read -r ci_run_id ci_actor_id sandbox_execution_id sandbox_attempt_id <<<"$(
  python3 - "${artifact_dir}/postgres/ci-runs.tsv" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
for row in rows:
    if len(row) >= 8 and row[2] == "main":
        print("\t".join([row[0], row[1], row[6], row[7]]))
        raise SystemExit(0)
raise SystemExit("main CI run row missing")
PY
)"
if [[ -z "${sandbox_execution_id}" || -z "${sandbox_attempt_id}" ]]; then
  echo "source_ci_runs main row was not submitted to sandbox-rental-service" >&2
  exit 1
fi

escaped_sandbox_execution_id="${sandbox_execution_id//\'/\'\'}"
escaped_sandbox_attempt_id="${sandbox_attempt_id//\'/\'\'}"

remote_psql sandbox_rental "
SELECT e.execution_id, a.attempt_id, e.org_id, e.actor_id, e.source_kind, e.workload_kind,
       e.external_provider, e.external_task_id, e.source_ref, e.state, a.state
FROM executions e
JOIN execution_attempts a ON a.execution_id = e.execution_id
WHERE e.execution_id = '${escaped_sandbox_execution_id}'::uuid
  AND a.attempt_id = '${escaped_sandbox_attempt_id}'::uuid;
" "${artifact_dir}/postgres/sandbox-source-ci-execution.tsv"
python3 - "${artifact_dir}/postgres/sandbox-source-ci-execution.tsv" "${org_id}" "${ci_actor_id}" "${ci_run_id}" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
if len(rows) != 1:
    raise SystemExit("expected one sandbox execution row for source CI")
row = rows[0]
_, _, org_id, actor_id, source_kind, workload_kind, external_provider, external_task_id, source_ref, execution_state, attempt_state = row
expected_org_id, expected_actor_id, expected_ci_run_id = sys.argv[2:5]
if org_id != expected_org_id or actor_id != expected_actor_id:
    raise SystemExit("sandbox source CI attribution mismatch")
if source_kind != "source_code_hosting" or workload_kind != "direct":
    raise SystemExit("sandbox source CI source/workload kind mismatch")
if external_provider != "source-code-hosting-service" or external_task_id != expected_ci_run_id:
    raise SystemExit("sandbox source CI external task linkage mismatch")
if "source-code-hosting://repos/" not in source_ref:
    raise SystemExit("sandbox source CI source_ref did not identify source repository")
if execution_state not in {"queued", "reserved", "launching", "running", "finalizing", "succeeded", "failed"}:
    raise SystemExit("unexpected sandbox execution state " + execution_state)
if attempt_state not in {"queued", "reserved", "launching", "running", "finalizing", "succeeded", "failed"}:
    raise SystemExit("unexpected sandbox attempt state " + attempt_state)
PY

signed_forgejo_webhook "${artifact_dir}/responses/forgejo-webhook.json" "${backend_owner}" "${backend_repo}" "${backend_repo_id}"
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
SELECT event_type, result, trace_id
FROM source_events
WHERE (
    repo_id = '${escaped_repo_id}'::uuid
    AND event_type IN ('source.repo.created', 'source.git.refs_refreshed', 'source.ci_run.queued', 'source.checkout_grant.created', 'source.webhook.repository')
  )
  OR (
    repo_id = '${escaped_repo_id}'::uuid
    AND event_type = 'source.ci_run.sandbox_submitted'
    AND details->>'ci_run_id' = '${ci_run_id}'
  )
  OR (
    event_type IN ('source.git_credential.created', 'source.git_credential.used')
    AND details->>'credential_id' = '${escaped_credential_id}'
  )
ORDER BY created_at, event_type;
" "${artifact_dir}/postgres/source-events.tsv"
python3 - "${artifact_dir}/postgres/source-events.tsv" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
events = {row[0] for row in rows if row}
missing = {
    "source.git_credential.created",
    "source.git_credential.used",
    "source.repo.created",
    "source.git.refs_refreshed",
    "source.ci_run.queued",
    "source.ci_run.sandbox_submitted",
    "source.checkout_grant.created",
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
    AND SpanName IN ('source.git_credential.create', 'source.git.receive', 'source.git.auth', 'source.git.repository.ensure', 'source.git.receive.apply', 'source.sandbox.ci_submit', 'source.repo.list', 'source.repo.read', 'source.refs.list', 'source.ci_runs.list', 'source.tree.get', 'source.checkout_grant.create')
" 12 "${artifact_dir}/clickhouse/source-business-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.forgejo.repo.create', 'source.forgejo.repo.get', 'source.forgejo.git.proxy', 'source.forgejo.refs.list', 'source.forgejo.contents.get')
" 5 "${artifact_dir}/clickhouse/source-forgejo-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.pg.git_credential.create', 'source.pg.git_credential.mark_used', 'source.pg.repo.create_from_git', 'source.pg.repo.list', 'source.pg.repo.get', 'source.pg.repo.get_by_slug', 'source.pg.refs.replace', 'source.pg.refs.list_cached', 'source.pg.ci_run.create_queued', 'source.pg.ci_run.mark_sandbox_submitted', 'source.pg.ci_run.list', 'source.pg.checkout_grant.create')
" 12 "${artifact_dir}/clickhouse/source-postgres-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND (
      (ServiceName = 'source-code-hosting-service' AND SpanName IN ('source.secrets.git_credential.create', 'source.secrets.git_credential.verify'))
      OR (ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'secrets-service') > 0)
      OR (ServiceName = 'secrets-service' AND SpanName IN ('auth.spiffe.mtls.server', 'secrets.credential.internal_create', 'secrets.credential.internal_verify', 'secrets.credential.create', 'secrets.credential.verify', 'secrets.bao.transit.hmac', 'secrets.bao.transit.verify_hmac'))
    )
" 8 "${artifact_dir}/clickhouse/source-secrets-boundary-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND (
      (ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'sandbox-rental-service') > 0)
      OR (ServiceName = 'sandbox-rental-service' AND SpanName IN ('auth.spiffe.mtls.server', 'sandbox-rental.source_ci.submit', 'sandbox-rental.execution.submit'))
    )
" 4 "${artifact_dir}/clickhouse/source-sandbox-boundary-spans-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT
        multiIf(
          ServiceName = 'source-code-hosting-service' AND SpanName = 'source.sandbox.ci_submit', '01_source_submit',
          ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'sandbox-rental-service') > 0, '02_source_mtls_client',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'auth.spiffe.mtls.server', '03_sandbox_mtls_server',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'sandbox-rental.source_ci.submit', '04_sandbox_source_ci_submit',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'sandbox-rental.execution.submit', '05_sandbox_execution_submit',
          ''
        ) AS stage,
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND stage != ''
      ORDER BY Timestamp, stage
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/source-sandbox-boundary-sequence.tsv"
python3 - "${artifact_dir}/clickhouse/source-sandbox-boundary-sequence.tsv" <<'PY'
import csv
import sys

required = [
    "01_source_submit",
    "02_source_mtls_client",
    "03_sandbox_mtls_server",
    "04_sandbox_source_ci_submit",
    "05_sandbox_execution_submit",
]
rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
first = {}
for index, row in enumerate(rows):
    first.setdefault(row["stage"], (index, row))
missing = [stage for stage in required if stage not in first]
if missing:
    raise SystemExit("missing source->sandbox boundary stages: " + ", ".join(missing))
positions = [first[stage][0] for stage in required]
if positions != sorted(positions):
    raise SystemExit("source->sandbox boundary spans were not observed in the expected order")
trace_ids = {first[stage][1]["TraceId"] for stage in required}
if len(trace_ids) != 1:
    raise SystemExit("source->sandbox boundary spans did not share one trace")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT
        multiIf(
          ServiceName = 'source-code-hosting-service' AND SpanName = 'source.secrets.git_credential.create', '01_source_create',
          ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'secrets-service') > 0, '02_source_mtls_client',
          ServiceName = 'secrets-service' AND SpanName = 'auth.spiffe.mtls.server', '03_secrets_mtls_server',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.credential.internal_create', '04_secrets_internal_create',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.credential.create', '05_secrets_create',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.bao.transit.hmac', '06_openbao_hmac',
          ServiceName = 'source-code-hosting-service' AND SpanName = 'source.secrets.git_credential.verify', '07_source_verify',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.credential.internal_verify', '08_secrets_internal_verify',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.credential.verify', '09_secrets_verify',
          ServiceName = 'secrets-service' AND SpanName = 'secrets.bao.transit.verify_hmac', '10_openbao_verify_hmac',
          ''
        ) AS stage,
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND stage != ''
      ORDER BY Timestamp, stage
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/source-secrets-boundary-sequence.tsv"
python3 - "${artifact_dir}/clickhouse/source-secrets-boundary-sequence.tsv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
required_any_trace = [
    ["01_source_create", "02_source_mtls_client", "03_secrets_mtls_server", "04_secrets_internal_create", "05_secrets_create", "06_openbao_hmac"],
    ["07_source_verify", "02_source_mtls_client", "03_secrets_mtls_server", "08_secrets_internal_verify", "09_secrets_verify", "10_openbao_verify_hmac"],
]
for required in required_any_trace:
    by_trace = {}
    for index, row in enumerate(rows):
        by_trace.setdefault(row["TraceId"], []).append((index, row["stage"]))
    matched = False
    for trace_id, trace_rows in by_trace.items():
        first = {}
        for index, stage in trace_rows:
            first.setdefault(stage, index)
        if all(stage in first for stage in required):
            positions = [first[stage] for stage in required]
            if positions == sorted(positions):
                matched = True
                break
    if not matched:
        raise SystemExit("missing ordered source->secrets boundary stages: " + ", ".join(required))
PY

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM job_events
  WHERE created_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND execution_id = toUUID({sandbox_execution_id:String})
    AND attempt_id = toUUID({sandbox_attempt_id:String})
    AND org_id = toUInt64({org_id:String})
    AND actor_id = {ci_actor_id:String}
    AND source_kind = 'source_code_hosting'
    AND external_provider = 'source-code-hosting-service'
    AND external_task_id = {ci_run_id:String}
" 1 "${artifact_dir}/clickhouse/sandbox-source-ci-job-events-count.tsv" \
  --param_sandbox_execution_id="${sandbox_execution_id}" \
  --param_sandbox_attempt_id="${sandbox_attempt_id}" \
  --param_org_id="${org_id}" \
  --param_ci_actor_id="${ci_actor_id}" \
  --param_ci_run_id="${ci_run_id}"

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
          OR positionCaseInsensitive(toString(SpanAttributes), 'fmgt_') > 0
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
        arrayElement(SpanAttributes, 'source.slug') AS source_slug,
        arrayElement(SpanAttributes, 'source.git_service') AS git_service
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
        AND ServiceName IN ('source-code-hosting-service', 'console')
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/otel-traces.tsv"

python3 - "${run_json_path}" "${org_id}" "${repo_id}" "${credential_id}" "${checkout_grant_id}" "${ci_run_id}" "${sandbox_execution_id}" "${sandbox_attempt_id}" "${window_start}" "${window_end}" "${artifact_dir}" "${git_repo_url}" <<'PY'
import json
import sys

path, org_id, repo_id, credential_id, grant_id, ci_run_id, sandbox_execution_id, sandbox_attempt_id, window_start, window_end, artifact_dir, git_repo_url = sys.argv[1:13]
payload = json.load(open(path, encoding="utf-8"))
payload.update({
    "org_id": org_id,
    "repo_id": repo_id,
    "git_credential_id": credential_id,
    "checkout_grant_id": grant_id,
    "ci_run_id": ci_run_id,
    "sandbox_execution_id": sandbox_execution_id,
    "sandbox_attempt_id": sandbox_attempt_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
    "git_repo_url": git_repo_url,
})
json.dump(payload, open(path, "w", encoding="utf-8"), indent=2, sort_keys=True)
print()
PY

echo "source-code-hosting proof ok: ${artifact_dir}"
