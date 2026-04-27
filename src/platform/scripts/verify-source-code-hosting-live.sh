#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-source-code-hosting-smoke-test-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/source-code-hosting-smoke-test}"
artifact_dir="${artifact_root}/${run_id}"
run_json_path="${artifact_dir}/run.json"
builds_log_path="${artifact_dir}/builds-ui.log"
source_api_base_url="${SOURCE_CODE_HOSTING_SMOKE_TEST_BASE_URL:-https://source.api.${VERIFICATION_DOMAIN}}"
source_api_base_url="${source_api_base_url%/}"
projects_api_base_url="${PROJECTS_SMOKE_TEST_BASE_URL:-https://projects.api.${VERIFICATION_DOMAIN}}"
projects_api_base_url="${projects_api_base_url%/}"
console_base_url="${TEST_BASE_URL:-https://console.${VERIFICATION_DOMAIN}}"
git_origin="${SOURCE_CODE_HOSTING_SMOKE_TEST_GIT_ORIGIN:-https://git.${VERIFICATION_DOMAIN}}"
git_origin="${git_origin%/}"
clickhouse_timeout_seconds="${SOURCE_CODE_HOSTING_SMOKE_TEST_CLICKHOUSE_TIMEOUT_SECONDS:-180}"
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

wait_for_postgres_rows() {
  local db="$1"
  local sql="$2"
  local output_path="$3"
  local min_rows="$4"
  local label="$5"
  local rows="0"
  local attempts=$((clickhouse_timeout_seconds / 2))
  if (( attempts < 1 )); then
    attempts=1
  fi
  for _ in $(seq 1 "${attempts}"); do
    remote_psql "${db}" "${sql}" "${output_path}"
    rows="$(grep -cv '^[[:space:]]*$' "${output_path}" || true)"
    if (( rows >= min_rows )); then
      return 0
    fi
    sleep 2
  done
  echo "Postgres assertion failed for ${label}: got ${rows}, expected >= ${min_rows}" >&2
  return 1
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
    -H "baggage: verself.verification_run=${run_id}"
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
    "smoke_test_run_id": os.environ["RUN_ID"],
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
delivery = \"source-smoke-test-\" + payload[\"smoke_test_run_id\"]
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

verification_print_artifacts "${artifact_dir}" "${builds_log_path}" "${run_json_path}"
verification_wait_for_loopback_api "source-code-hosting-service" "http://127.0.0.1:4261/readyz" "200"
verification_wait_for_http "source API auth boundary" "${source_api_base_url}/api/v1/repos" "401"
verification_wait_for_http "git origin is headless" "${git_origin}/" "404"
verification_wait_for_http "console UI" "${console_base_url}" "200"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
source_access_token="${SOURCE_CODE_HOSTING_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"
projects_access_token="${PROJECTS_SERVICE_ACCESS_TOKEN:-${IDENTITY_SERVICE_ACCESS_TOKEN}}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

set +e
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  VERIFICATION_RUN_ID="${run_id}" \
  VERIFICATION_RUN_JSON_PATH="${run_json_path}" \
  BASE_URL="${console_base_url}" \
  TEST_BASE_URL="${console_base_url}" \
  VERSELF_DOMAIN="${VERIFICATION_DOMAIN}" \
  ZITADEL_BASE_URL="https://auth.${VERIFICATION_DOMAIN}" \
  TEST_EMAIL="${TEST_EMAIL}" \
  TEST_PASSWORD="${TEST_PASSWORD}" \
  bash -lc '
    cd "$1"
    vp exec playwright test e2e/builds.live.spec.ts \
      --project=chromium \
      --output "$2"
  ' bash "${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/console" "${artifact_dir}/playwright-results" \
  >"${builds_log_path}" 2>&1
builds_status=$?
set -e

verification_tail_log_on_failure "${builds_status}" "${builds_log_path}" "180"
if [[ "${builds_status}" -ne 0 ]]; then
  exit "${builds_status}"
fi

repo_slug="source-smoke-test-$(printf '%s' "${run_id}" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '-' | sed -E 's/^-+|-+$//g' | cut -c1-48)"
if [[ -z "${repo_slug}" ]]; then
  repo_slug="source-smoke-test"
fi

cat >"${artifact_dir}/payloads/create-project.json" <<EOF
{
  "display_name": "Source Smoke Test ${run_id}",
  "slug": "${repo_slug}",
  "description": "Source hosting smoke test project"
}
EOF
curl -fsS \
  -X POST \
  -H "Authorization: Bearer ${projects_access_token}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: source-project:${run_id}" \
  -H "baggage: verself.verification_run=${run_id}" \
  --data-binary "@${artifact_dir}/payloads/create-project.json" \
  "${projects_api_base_url}/api/v1/projects" >"${artifact_dir}/responses/create-project.json"
read -r project_id project_version <<<"$(
  python3 - "${artifact_dir}/responses/create-project.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["project_id"], payload["version"])
PY
)"

cat >"${artifact_dir}/payloads/create-repository.json" <<EOF
{
  "project_id": "${project_id}",
  "description": "Source smoke test ${run_id}",
  "default_branch": "main"
}
EOF
api_request "POST" "/api/v1/repos" "${artifact_dir}/responses/create-repository.json" "${artifact_dir}/payloads/create-repository.json" "source-repo:${run_id}"
read -r repo_id org_id org_slug project_slug git_repo_url <<<"$(
  python3 - "${artifact_dir}/responses/create-repository.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
print(payload["repo_id"], payload["org_id"], payload["org_slug"], payload["project_slug"], payload["git_http_url"])
PY
)"
old_project_slug="${project_slug}"

renamed_project_slug="${repo_slug}-renamed"
cat >"${artifact_dir}/payloads/update-project.json" <<EOF
{
  "display_name": "Source Smoke Test Renamed ${run_id}",
  "slug": "${renamed_project_slug}",
  "description": "Source hosting smoke test project renamed",
  "version": "${project_version}"
}
EOF
curl -fsS \
  -X PATCH \
  -H "Authorization: Bearer ${projects_access_token}" \
  -H "Accept: application/json" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: source-project-rename:${run_id}" \
  -H "baggage: verself.verification_run=${run_id}" \
  --data-binary "@${artifact_dir}/payloads/update-project.json" \
  "${projects_api_base_url}/api/v1/projects/${project_id}" >"${artifact_dir}/responses/update-project.json"
project_version="$(
  python3 - "${artifact_dir}/responses/update-project.json" "${renamed_project_slug}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["slug"] != sys.argv[2]:
    raise SystemExit("project rename did not return the renamed slug")
print(payload["version"])
PY
)"

api_request "GET" "/api/v1/repos/${repo_id}" "${artifact_dir}/responses/get-repository-before-push.json"
read -r project_slug git_repo_url <<<"$(
  python3 - "${artifact_dir}/responses/get-repository-before-push.json" "${renamed_project_slug}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload["project_slug"] != sys.argv[2]:
    raise SystemExit("source repo response did not reflect the renamed project slug")
print(payload["project_slug"], payload["git_http_url"])
PY
)"

cat >"${artifact_dir}/payloads/create-git-credential.json" <<EOF
{
  "label": "source smoke test ${run_id}",
  "expires_in_seconds": 3600
}
EOF
credential_raw_path="$(mktemp)"
checkout_raw_path="$(mktemp)"
git_workdir="$(mktemp -d)"
askpass_path="$(mktemp)"
netrc_path="$(mktemp)"
trap 'rm -f "${credential_raw_path}" "${checkout_raw_path}" "${askpass_path}" "${netrc_path}"; rm -rf "${git_workdir}"' EXIT

api_request "POST" "/api/v1/git-credentials" "${credential_raw_path}" "${artifact_dir}/payloads/create-git-credential.json" "source-git-credential:${run_id}"
read -r credential_id git_username git_token <<<"$(
  python3 - "${credential_raw_path}" "${artifact_dir}/responses/create-git-credential.json" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
token = payload.pop("token", "")
if not token:
    raise SystemExit("git credential response did not include token")
payload["token"] = "[redacted]"
json.dump(payload, open(sys.argv[2], "w", encoding="utf-8"), indent=2, sort_keys=True)
print(payload["credential_id"], payload["username"], token)
PY
)"

git_host="$(
  python3 - "${git_origin}" <<'PY'
import sys
from urllib.parse import urlparse

host = urlparse(sys.argv[1]).hostname
if not host:
    raise SystemExit("git origin did not contain a host")
print(host)
PY
)"
cat >"${netrc_path}" <<EOF
machine ${git_host}
  login ${git_username}
  password ${git_token}
EOF
chmod 600 "${netrc_path}"

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
git -C "${git_workdir}" config user.name "Verself Source Smoke Test"
git -C "${git_workdir}" config user.email "source-smoke-test@verself.invalid"
printf '# Source smoke test\n\nrun: %s\n' "${run_id}" >"${git_workdir}/README.md"
mkdir -p "${git_workdir}/.forgejo/workflows"
cat >"${git_workdir}/.forgejo/workflows/smoke-test.yml" <<'EOF'
name: smoke test
on:
  push:
jobs:
  smoke_test:
    runs-on: [self-hosted, linux, x64, verself-4vcpu-ubuntu-2404]
    steps:
      - name: Check out repository
        run: |
          set -euo pipefail
          workspace="${FORGEJO_WORKSPACE:-${GITHUB_WORKSPACE:-}}"
          if [ -z "$workspace" ] || [ "$workspace" = "/" ]; then
            echo "invalid Forgejo workspace: ${workspace:-<empty>}" >&2
            exit 1
          fi
          mkdir -p "$workspace"
          cd "$workspace"
          server_url="${VERSELF_HOST_SERVICE_HTTP_ORIGIN:-${FORGEJO_SERVER_URL:?}}"
          repo_url="${server_url%/}/${FORGEJO_REPOSITORY:?}.git"
          if [ -d .git ]; then
            git remote set-url origin "$repo_url"
          else
            if [ -n "$(find . -mindepth 1 -maxdepth 1 -print -quit)" ]; then
              echo "Forgejo workspace is not empty and is not a Git checkout: $workspace" >&2
              exit 1
            fi
            git init
            git remote add origin "$repo_url"
          fi
          askpass="$(mktemp)"
          cleanup() { rm -f "$askpass"; }
          trap cleanup EXIT
          cat >"$askpass" <<'SH'
          #!/usr/bin/env sh
          case "$1" in
            *Username*) printf '%s\n' x-access-token ;;
            *Password*) printf '%s\n' "${FORGEJO_TOKEN:?}" ;;
            *) printf '\n' ;;
          esac
          SH
          chmod 0700 "$askpass"
          GIT_ASKPASS="$askpass" GIT_TERMINAL_PROMPT=0 git fetch --depth=1 origin "${FORGEJO_SHA:?}"
          git -c advice.detachedHead=false checkout --detach FETCH_HEAD
          git status --short
      - name: Verify checkout
        run: |
          set -euo pipefail
          test -f README.md
          grep -F "run:" README.md
          printf 'checkout-file=README.md\n'
          git rev-parse --verify HEAD
          echo source smoke test "$(uname -m)"
EOF
git -C "${git_workdir}" add README.md .forgejo/workflows/smoke-test.yml
git -C "${git_workdir}" commit -m "source smoke test ${run_id}" >/dev/null
env \
  GIT_ASKPASS="${askpass_path}" \
  GIT_TERMINAL_PROMPT=0 \
  SOURCE_GIT_USERNAME="${git_username}" \
  SOURCE_GIT_TOKEN="${git_token}" \
  git -C "${git_workdir}" push "${git_repo_url}" main:main >"${artifact_dir}/responses/git-push.log" 2>&1

legacy_git_repo_url="${git_origin}/org-${org_id}/${old_project_slug}.git"
legacy_info_refs_url="${legacy_git_repo_url}/info/refs?service=git-upload-pack"
legacy_redirect_status="$(
  curl -sS \
    --netrc-file "${netrc_path}" \
    -o /dev/null \
    -D "${artifact_dir}/responses/git-legacy-redirect.headers" \
    -w '%{http_code}' \
    "${legacy_info_refs_url}"
)"
if [[ "${legacy_redirect_status}" != "308" ]]; then
  echo "legacy Git org path returned ${legacy_redirect_status}, expected 308" >&2
  exit 1
fi
expected_legacy_location="/${org_slug}/${project_slug}.git/info/refs?service=git-upload-pack"
if ! grep -Fqi "Location: ${expected_legacy_location}" "${artifact_dir}/responses/git-legacy-redirect.headers"; then
  echo "legacy Git org path did not redirect to ${expected_legacy_location}" >&2
  exit 1
fi

api_request "GET" "/api/v1/repos?project_id=${project_id}" "${artifact_dir}/responses/list-repositories.json"
python3 - "${artifact_dir}/responses/list-repositories.json" "${repo_id}" "${project_id}" "${org_slug}" "${project_slug}" "${git_repo_url}" <<'PY'
import json
import sys

payload = json.load(open(sys.argv[1], encoding="utf-8"))
repo_id, project_id, org_slug, project_slug, git_repo_url = sys.argv[2:7]
for repo in payload.get("repositories") or []:
    if repo.get("repo_id") == repo_id:
        if repo.get("project_id") != project_id:
            raise SystemExit("source smoke test repo project_id mismatch in list response")
        if repo.get("org_slug") != org_slug or repo.get("project_slug") != project_slug:
            raise SystemExit("source smoke test repo friendly path mismatch in list response")
        if repo.get("git_http_url") != git_repo_url:
            raise SystemExit("source smoke test repo git_http_url mismatch in list response")
        raise SystemExit(0)
raise SystemExit(f"source smoke test repo {repo_id!r} not found")
PY

api_request "GET" "/api/v1/repos/${repo_id}" "${artifact_dir}/responses/get-repository.json"
api_request "GET" "/api/v1/repos/${repo_id}/refs" "${artifact_dir}/responses/list-refs.json"
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
escaped_project_id="${project_id//\'/\'\'}"
escaped_old_project_slug="${old_project_slug//\'/\'\'}"

remote_psql projects_service "
SELECT project_id, org_id, slug, state, version
FROM projects
WHERE project_id = '${escaped_project_id}'::uuid
  AND state = 'active';
" "${artifact_dir}/postgres/project.tsv"
if [[ ! -s "${artifact_dir}/postgres/project.tsv" ]]; then
  echo "projects_service.projects did not contain the smoke test project" >&2
  exit 1
fi
if ! grep -Fq $'\t'"${project_slug}"$'\t' "${artifact_dir}/postgres/project.tsv"; then
  echo "projects_service.projects did not contain the renamed project slug" >&2
  exit 1
fi

remote_psql projects_service "
SELECT org_id, slug, project_id
FROM project_slug_redirects
WHERE org_id = '${org_id}'
  AND slug = '${escaped_old_project_slug}'
  AND project_id = '${escaped_project_id}'::uuid;
" "${artifact_dir}/postgres/project-slug-redirect.tsv"
if [[ ! -s "${artifact_dir}/postgres/project-slug-redirect.tsv" ]]; then
  echo "project_slug_redirects did not contain the old project slug redirect" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT repo_id, org_id, project_id, name, slug, default_branch, visibility, state, last_pushed_at IS NOT NULL AS pushed
FROM source_repositories
WHERE repo_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/repository.tsv"

org_id="$(cut -f2 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
row_project_id="$(cut -f3 "${artifact_dir}/postgres/repository.tsv" | head -n 1)"
if [[ -z "${org_id}" || "${row_project_id}" != "${project_id}" ]]; then
  echo "source_repositories did not contain the smoke test repo" >&2
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
  echo "source_repository_backends did not contain the smoke test Forgejo backend" >&2
  exit 1
fi
backend_owner="$(cut -f4 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"
backend_repo="$(cut -f5 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"
backend_repo_id="$(cut -f6 "${artifact_dir}/postgres/repository-backend.tsv" | head -n 1)"
backend_full_name="${backend_owner}/${backend_repo}"
escaped_backend_full_name="${backend_full_name//\'/\'\'}"

remote_psql source_code_hosting "
SELECT credential_id, org_id, actor_id, username, token_prefix, state, expires_at > now() AS unexpired, last_used_at IS NOT NULL AS used
FROM source_git_credentials
WHERE credential_id = '${escaped_credential_id}'::uuid;
" "${artifact_dir}/postgres/git-credential.tsv"
if [[ ! -s "${artifact_dir}/postgres/git-credential.tsv" ]]; then
  echo "source_git_credentials did not contain the smoke test credential" >&2
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

remote_psql sandbox_rental "
SELECT provider, provider_repository_id, org_id, project_id, source_repository_id, provider_owner, provider_repo, repository_full_name, active
FROM runner_provider_repositories
WHERE provider = 'forgejo'
  AND source_repository_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/runner-provider-repository.tsv"
python3 - "${artifact_dir}/postgres/runner-provider-repository.tsv" "${org_id}" "${project_id}" "${backend_repo_id}" "${backend_owner}" "${backend_repo}" <<'PY'
import csv
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
if len(rows) != 1:
    raise SystemExit("expected one sandbox runner repository registration row")
provider, provider_repo_id, org_id, project_id, source_repo_id, provider_owner, provider_repo, repository_full_name, active = rows[0]
expected_org_id, expected_project_id, expected_repo_id, expected_owner, expected_repo = sys.argv[2:7]
if provider != "forgejo" or provider_repo_id != expected_repo_id or org_id != expected_org_id or project_id != expected_project_id:
    raise SystemExit("runner repository registration did not match source backend")
if provider_owner != expected_owner or provider_repo != expected_repo or active != "t":
    raise SystemExit("runner repository registration owner/repo/active mismatch")
PY

if [[ ! "${backend_repo_id}" =~ ^[0-9]+$ ]]; then
  echo "Forgejo backend repo id was not numeric: ${backend_repo_id}" >&2
  exit 1
fi

wait_for_postgres_rows sandbox_rental "
SELECT provider_job_id, provider_repository_id, repository_full_name, status, labels_json
FROM runner_jobs
WHERE provider = 'forgejo'
  AND provider_repository_id = ${backend_repo_id}
ORDER BY updated_at DESC;
" "${artifact_dir}/postgres/forgejo-runner-jobs.tsv" 1 "Forgejo runner job demand"
python3 - "${artifact_dir}/postgres/forgejo-runner-jobs.tsv" <<'PY'
import csv
import json
import sys

rows = list(csv.reader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
labels = set()
for row in rows:
    if len(row) >= 5:
        labels.update(json.loads(row[4] or "[]"))
required = {"self-hosted", "linux", "x64", "verself-4vcpu-ubuntu-2404"}
missing = required - labels
if missing:
    raise SystemExit("Forgejo runner job labels missing: " + ", ".join(sorted(missing)))
PY

wait_for_postgres_rows sandbox_rental "
SELECT e.execution_id, a.attempt_id, j.provider_job_id, ra.allocation_id,
       e.state, ra.state, e.source_kind, e.workload_kind, e.external_provider, e.external_task_id,
       e.org_id, e.actor_id, j.labels_json, COALESCE(a.billing_job_id, 0),
       COALESCE(bw.window_count, 0), COALESCE(bw.reserved_charge_units, 0), COALESCE(bw.billed_charge_units, 0),
       COALESCE(bw.writeoff_charge_units, 0)
FROM runner_allocations ra
JOIN executions e ON e.execution_id = ra.execution_id
JOIN execution_attempts a ON a.execution_id = e.execution_id
LEFT JOIN runner_jobs j ON j.provider = ra.provider AND j.provider_job_id = ra.requested_for_provider_job_id
LEFT JOIN LATERAL (
  SELECT count(*) AS window_count,
         sum(reserved_charge_units) AS reserved_charge_units,
         sum(billed_charge_units) AS billed_charge_units,
         sum(writeoff_charge_units) AS writeoff_charge_units
  FROM execution_billing_windows w
  WHERE w.attempt_id = a.attempt_id
) bw ON true
WHERE ra.provider = 'forgejo'
  AND ra.provider_repository_id = ${backend_repo_id}
  AND e.source_ref = '${escaped_backend_full_name}'
  AND e.state = 'succeeded'
ORDER BY e.updated_at DESC
LIMIT 1;
" "${artifact_dir}/postgres/forgejo-runner-execution.tsv" 1 "Forgejo runner execution"

IFS=$'\t' read -r forgejo_execution_id forgejo_attempt_id forgejo_provider_job_id forgejo_allocation_id forgejo_execution_state _forgejo_allocation_state forgejo_source_kind forgejo_workload_kind forgejo_external_provider forgejo_external_task_id forgejo_org_id forgejo_actor_id forgejo_labels_json forgejo_billing_job_id forgejo_billing_window_count forgejo_reserved_charge_units _forgejo_billed_charge_units _forgejo_writeoff_charge_units <"${artifact_dir}/postgres/forgejo-runner-execution.tsv"
python3 - "${forgejo_execution_id}" "${forgejo_attempt_id}" "${forgejo_provider_job_id}" "${forgejo_allocation_id}" "${forgejo_execution_state}" "${forgejo_source_kind}" "${forgejo_workload_kind}" "${forgejo_external_provider}" "${forgejo_external_task_id}" "${forgejo_org_id}" "${org_id}" "${forgejo_actor_id}" "${backend_repo_id}" "${forgejo_labels_json}" "${forgejo_billing_job_id}" "${forgejo_billing_window_count}" "${forgejo_reserved_charge_units}" <<'PY'
import json
import sys
from uuid import UUID

execution_id, attempt_id, provider_job_id, allocation_id, execution_state, source_kind, workload_kind, external_provider, external_task_id, actual_org_id, expected_org_id, actor_id, backend_repo_id, labels_json, billing_job_id, billing_window_count, reserved_charge_units = sys.argv[1:18]
UUID(execution_id)
UUID(attempt_id)
UUID(allocation_id)
if execution_state != "succeeded":
    raise SystemExit("Forgejo runner execution did not succeed")
if source_kind != "forgejo_actions" or workload_kind != "runner" or external_provider != "forgejo":
    raise SystemExit("Forgejo runner execution metadata was not provider-neutral runner metadata")
if provider_job_id != external_task_id:
    raise SystemExit("Forgejo runner external_task_id did not match provider job id")
if actual_org_id != expected_org_id:
    raise SystemExit("Forgejo runner execution org attribution mismatch")
if actor_id != "forgejo-actions:" + backend_repo_id:
    raise SystemExit("Forgejo runner execution actor attribution mismatch")
labels = set(json.loads(labels_json or "[]"))
if "verself-4vcpu-ubuntu-2404" not in labels:
    raise SystemExit("Forgejo runner execution did not preserve runner-class label")
if int(billing_job_id) <= 0 or int(billing_window_count) < 1 or int(reserved_charge_units) <= 0:
    raise SystemExit("Forgejo runner execution did not record billing attribution")
PY

signed_forgejo_webhook "${artifact_dir}/responses/forgejo-webhook.json" "${backend_owner}" "${backend_repo}" "${backend_repo_id}"
python3 - "${artifact_dir}/responses/forgejo-webhook.json" <<'PY'
import json
import sys

status = json.load(open(sys.argv[1], encoding="utf-8")).get("status")
if status != 202:
    raise SystemExit(f"expected Forgejo webhook smoke test to return 202, got {status}")
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

remote_psql source_code_hosting "
SELECT grant_id, repo_id, org_id, actor_id, ref, path_prefix, expires_at, consumed_at IS NOT NULL AS consumed
FROM source_checkout_grants
WHERE grant_id = '${escaped_grant_id}'::uuid
  AND repo_id = '${escaped_repo_id}'::uuid;
" "${artifact_dir}/postgres/checkout-grant.tsv"
if [[ ! -s "${artifact_dir}/postgres/checkout-grant.tsv" ]]; then
  echo "source_checkout_grants did not contain the smoke test grant" >&2
  exit 1
fi

remote_psql source_code_hosting "
SELECT event_type, result, trace_id
FROM source_events
WHERE (
    repo_id = '${escaped_repo_id}'::uuid
    AND event_type IN ('source.repo.created', 'source.git.refs_refreshed', 'source.checkout_grant.created', 'source.webhook.repository')
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
    "source.checkout_grant.created",
    "source.webhook.repository",
} - events
if missing:
    raise SystemExit("missing source event rows: " + ", ".join(sorted(missing)))
if any(row[1] != "allowed" for row in rows if row):
    raise SystemExit("expected all source smoke test event rows to be allowed")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count verself "
  SELECT count()
  FROM job_events
	  WHERE execution_id = toUUID({forgejo_execution_id:String})
	    AND org_id = {org_id:UInt64}
	    AND source_kind = 'forgejo_actions'
	    AND workload_kind = 'runner'
	    AND external_provider = 'forgejo'
	    AND provider = 'forgejo'
	    AND external_task_id = {forgejo_provider_job_id:String}
	    AND provider_job_id = toUInt64({forgejo_provider_job_id:String})
	    AND billing_job_id > 0
	    AND status = 'succeeded'
	" 1 "${artifact_dir}/clickhouse/forgejo-job-event-count.tsv" \
	  --param_forgejo_execution_id="${forgejo_execution_id}" \
	  --param_forgejo_provider_job_id="${forgejo_provider_job_id}" \
	  --param_org_id="${org_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'caddy'
    AND arrayElement(LogAttributes, 'http_host') = '10.255.0.1:18080'
    AND arrayElement(LogAttributes, 'user_agent') LIKE 'git/%'
    AND arrayElement(LogAttributes, 'http_status') = '200'
    AND arrayElement(LogAttributes, 'client_ip') != ''
" 2 "${artifact_dir}/clickhouse/forgejo-checkout-caddy-log-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'forgejo'
    AND position(Body, {backend_git_path:String}) > 0
    AND position(Body, 'git-upload-pack') > 0
    AND position(Body, '200 OK') > 0
" 2 "${artifact_dir}/clickhouse/forgejo-checkout-git-log-count.tsv" \
  --param_backend_git_path="/${backend_full_name}.git/"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName IN (
      'forgejo.webhook.actions',
      'forgejo.runner.repository.sync',
      'forgejo.capacity.reconcile',
      'forgejo.runner.allocate',
      'runner.bootstrap.consume',
      'sandbox-rental.execution.run'
    )
" 6 "${artifact_dir}/clickhouse/forgejo-runner-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'forgejo.webhook.actions'
    AND SpanAttributes['forgejo.event'] = 'push'
    AND SpanAttributes['forgejo.repository_id'] = {forgejo_repository_id:String}
" 1 "${artifact_dir}/clickhouse/forgejo-push-webhook-span-count.tsv" \
  --param_forgejo_repository_id="${backend_repo_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.execution.run'
    AND SpanAttributes['execution.id'] = {forgejo_execution_id:String}
" 1 "${artifact_dir}/clickhouse/forgejo-execution-run-span-count.tsv" \
  --param_forgejo_execution_id="${forgejo_execution_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'vm-orchestrator'
    AND SpanName IN ('rpc.AcquireLease', 'rpc.StartExec', 'rpc.WaitExec', 'rpc.ReleaseLease')
    AND TraceId IN (
      SELECT trace_id
      FROM verself.job_events
      WHERE execution_id = toUUID({forgejo_execution_id:String})
        AND trace_id != ''
    )
" 4 "${artifact_dir}/clickhouse/forgejo-vm-orchestrator-spans-count.tsv" \
  --param_forgejo_execution_id="${forgejo_execution_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database verself \
    --param_forgejo_execution_id="${forgejo_execution_id}" \
    --query "
      SELECT
        execution_id,
        external_provider,
        source_kind,
        workload_kind,
	        runner_class,
	        provider,
	        provider_job_id,
	        repository_full_name,
	        job_name,
	        billing_job_id,
	        reserved_charge_units,
	        billed_charge_units,
	        writeoff_charge_units,
	        duration_ms,
        started_at,
        completed_at
      FROM job_events
      WHERE execution_id = toUUID({forgejo_execution_id:String})
      ORDER BY created_at DESC
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/forgejo-job-timing.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT
        multiIf(
          ServiceName = 'sandbox-rental-service' AND SpanName = 'forgejo.webhook.actions', '01_webhook',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'forgejo.runner.repository.sync', '02_repository_sync',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'forgejo.capacity.reconcile', '03_capacity_reconcile',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'forgejo.runner.allocate', '04_runner_allocate',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'sandbox-rental.execution.submit', '05_execution_submit',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'runner.bootstrap.consume', '06_runner_bootstrap',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'sandbox-rental.execution.run', '07_execution_run',
          ''
        ) AS stage,
        Timestamp,
        ServiceName,
        SpanName,
        TraceId,
        SpanId,
        ParentSpanId,
        SpanAttributes['execution.id'] AS execution_id,
        SpanAttributes['runner.allocation_id'] AS allocation_id,
        SpanAttributes['forgejo.repository_id'] AS forgejo_repository_id,
        SpanAttributes['forgejo.job_id'] AS forgejo_job_id
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
        AND stage != ''
      ORDER BY Timestamp, stage
      FORMAT TSVWithNames
	    "
	) >"${artifact_dir}/clickhouse/forgejo-runner-boundary-sequence.tsv"
	python3 - "${artifact_dir}/clickhouse/forgejo-runner-boundary-sequence.tsv" <<'PY'
import csv
import sys

rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
if not any(row["stage"] == "01_webhook" for row in rows):
    raise SystemExit("missing Forgejo webhook span")

scheduler_required = [
    "02_repository_sync",
    "03_capacity_reconcile",
    "04_runner_allocate",
    "05_execution_submit",
    "07_execution_run",
]
by_trace = {}
for index, row in enumerate(rows):
    by_trace.setdefault(row["TraceId"], []).append((index, row))
for trace_id, trace_rows in by_trace.items():
    first = {}
    for index, row in trace_rows:
        first.setdefault(row["stage"], (index, row))
    if all(stage in first for stage in scheduler_required):
        positions = [first[stage][0] for stage in scheduler_required]
        if positions == sorted(positions):
            allocation_id = first["04_runner_allocate"][1]["allocation_id"]
            bootstrap_rows = [row for _, row in trace_rows if row["stage"] == "06_runner_bootstrap"]
            if any(row["allocation_id"] == allocation_id for row in bootstrap_rows):
                raise SystemExit(0)
            raise SystemExit("Forgejo bootstrap span was not observed in the scheduler trace for allocation " + allocation_id)
raise SystemExit("missing ordered Forgejo scheduler boundary trace with stages: " + ", ".join(scheduler_required))
PY

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND SpanName IN ('source.git_credential.create', 'source.git.receive', 'source.git.auth', 'source.git.path.resolve', 'source.git.redirect', 'source.identity.organization.resolve', 'source.git.repository.ensure', 'source.git.receive.apply', 'source.runner.repository.register', 'source.repo.list', 'source.repo.read', 'source.refs.list', 'source.tree.get', 'source.checkout_grant.create')
" 10 "${artifact_dir}/clickhouse/source-business-spans-count.tsv"

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
    AND SpanName IN ('source.pg.git_credential.create', 'source.pg.git_credential.mark_used', 'source.pg.repo.create', 'source.pg.repo.list', 'source.pg.repo.get', 'source.pg.repo.get_by_project', 'source.pg.refs.replace', 'source.pg.refs.list_cached', 'source.pg.checkout_grant.create')
" 9 "${artifact_dir}/clickhouse/source-postgres-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND (
      (ServiceName = 'source-code-hosting-service' AND SpanName = 'source.projects.resolve' AND SpanAttributes['verself.project_id'] = {project_id:String})
      OR (ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'projects-service') > 0)
      OR (ServiceName = 'projects-service' AND SpanName IN ('auth.spiffe.mtls.server', 'projects.project.resolve', 'projects.pg.project.resolve'))
    )
" 4 "${artifact_dir}/clickhouse/source-projects-boundary-spans-count.tsv" \
  --param_project_id="${project_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_project_id="${project_id}" \
    --query "
      SELECT *
      FROM (
        SELECT
          multiIf(
            ServiceName = 'source-code-hosting-service' AND SpanName = 'source.projects.resolve', '01_source_resolve_project',
            ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'projects-service') > 0, '02_source_mtls_client',
            ServiceName = 'projects-service' AND SpanName = 'auth.spiffe.mtls.server', '03_projects_mtls_server',
            ServiceName = 'projects-service' AND SpanName = 'projects.project.resolve', '04_projects_resolve',
            ServiceName = 'projects-service' AND SpanName = 'projects.pg.project.resolve', '05_projects_pg_resolve',
            ''
          ) AS stage,
          Timestamp,
          ServiceName,
          SpanName,
          TraceId,
          SpanId,
          ParentSpanId,
          arrayElement(SpanAttributes, 'verself.project_id') AS project_id
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
      )
      WHERE stage != ''
        AND (project_id = '' OR project_id = {project_id:String})
      ORDER BY Timestamp, stage
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/source-projects-boundary-sequence.tsv"
python3 - "${artifact_dir}/clickhouse/source-projects-boundary-sequence.tsv" <<'PY'
import csv
import sys

required = [
    "01_source_resolve_project",
    "02_source_mtls_client",
    "03_projects_mtls_server",
    "04_projects_resolve",
    "05_projects_pg_resolve",
]
rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
by_trace = {}
for index, row in enumerate(rows):
    by_trace.setdefault(row["TraceId"], []).append((index, row))
for trace_rows in by_trace.values():
    first = {}
    for index, row in trace_rows:
        first.setdefault(row["stage"], (index, row))
    if all(stage in first for stage in required):
        positions = [first[stage][0] for stage in required]
        if positions == sorted(positions):
            raise SystemExit(0)
raise SystemExit("missing ordered source->projects boundary stages: " + ", ".join(required))
PY

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND (
      (ServiceName = 'source-code-hosting-service' AND SpanName = 'source.identity.organization.resolve' AND arrayElement(SpanAttributes, 'verself.org_id') = {org_id:String})
      OR (ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'identity-service') > 0)
      OR (ServiceName = 'identity-service' AND SpanName IN ('auth.spiffe.mtls.server', 'identity.organization.resolve', 'identity.pg.organization_profile.resolve') AND (arrayElement(SpanAttributes, 'verself.org_id') = '' OR arrayElement(SpanAttributes, 'verself.org_id') = {org_id:String}))
    )
" 5 "${artifact_dir}/clickhouse/source-identity-boundary-spans-count.tsv" \
  --param_org_id="${org_id}"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_org_id="${org_id}" \
    --query "
      SELECT *
      FROM (
        SELECT
          multiIf(
            ServiceName = 'source-code-hosting-service' AND SpanName = 'source.identity.organization.resolve', '01_source_resolve_org',
            ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'identity-service') > 0, '02_source_mtls_client',
            ServiceName = 'identity-service' AND SpanName = 'auth.spiffe.mtls.server', '03_identity_mtls_server',
            ServiceName = 'identity-service' AND SpanName = 'identity.organization.resolve', '04_identity_resolve',
            ServiceName = 'identity-service' AND SpanName = 'identity.pg.organization_profile.resolve', '05_identity_pg_resolve',
            ''
          ) AS stage,
          Timestamp,
          ServiceName,
          SpanName,
          TraceId,
          SpanId,
          ParentSpanId,
          arrayElement(SpanAttributes, 'verself.org_id') AS org_id
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
      )
      WHERE stage != ''
        AND (org_id = '' OR org_id = {org_id:String})
      ORDER BY Timestamp, stage
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/source-identity-boundary-sequence.tsv"
python3 - "${artifact_dir}/clickhouse/source-identity-boundary-sequence.tsv" <<'PY'
import csv
import sys

required = [
    "01_source_resolve_org",
    "02_source_mtls_client",
    "03_identity_mtls_server",
    "04_identity_resolve",
    "05_identity_pg_resolve",
]
rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8"), delimiter="\t"))
by_trace = {}
for index, row in enumerate(rows):
    by_trace.setdefault(row["TraceId"], []).append((index, row))
for trace_rows in by_trace.values():
    first = {}
    for index, row in trace_rows:
        first.setdefault(row["stage"], (index, row))
    if all(stage in first for stage in required):
        positions = [first[stage][0] for stage in required]
        if positions == sorted(positions):
            raise SystemExit(0)
raise SystemExit("missing ordered source->identity boundary stages: " + ", ".join(required))
PY

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 45 SECOND
    AND ServiceName = 'source-code-hosting-service'
    AND (
      (SpanName = 'source.git.path.resolve' AND arrayElement(SpanAttributes, 'source.git.redirected') = 'true')
      OR (SpanName = 'source.git.redirect' AND arrayElement(SpanAttributes, 'source.git.redirect_location') = {redirect_location:String})
    )
" 2 "${artifact_dir}/clickhouse/source-git-redirect-spans-count.tsv" \
  --param_redirect_location="${expected_legacy_location}"

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
      OR (ServiceName = 'sandbox-rental-service' AND SpanName IN ('auth.spiffe.mtls.server', 'sandbox-rental.runner_repository.register'))
    )
" 3 "${artifact_dir}/clickhouse/source-sandbox-boundary-spans-count.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT
        multiIf(
          ServiceName = 'source-code-hosting-service' AND SpanName = 'source.runner.repository.register', '01_source_register',
          ServiceName = 'source-code-hosting-service' AND SpanName = 'auth.spiffe.mtls.client' AND position(arrayElement(SpanAttributes, 'spiffe.expected_server_id'), 'sandbox-rental-service') > 0, '02_source_mtls_client',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'auth.spiffe.mtls.server', '03_sandbox_mtls_server',
          ServiceName = 'sandbox-rental-service' AND SpanName = 'sandbox-rental.runner_repository.register', '04_sandbox_runner_repository_register',
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
    "01_source_register",
    "02_source_mtls_client",
    "03_sandbox_mtls_server",
    "04_sandbox_runner_repository_register",
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
" 1 "${artifact_dir}/clickhouse/console-builds-ui-spans-count.tsv"

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
  echo "source smoke test found token-like material in source-code-hosting-service span attributes" >&2
  exit 1
fi

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
        AND ServiceName = 'sandbox-rental-service'
        AND (
          positionCaseInsensitive(toString(SpanAttributes), 'runner_token') > 0
          OR positionCaseInsensitive(toString(SpanAttributes), 'bootstrap_token') > 0
          OR positionCaseInsensitive(toString(SpanAttributes), 'VERSELF_RUNNER_BOOTSTRAP_TOKEN') > 0
          OR positionCaseInsensitive(toString(SpanAttributes), 'token_url') > 0
        )
    "
) >"${artifact_dir}/clickhouse/sandbox-runner-secret-span-leak-count.tsv"
sandbox_runner_secret_span_leaks="$(tail -n 1 "${artifact_dir}/clickhouse/sandbox-runner-secret-span-leak-count.tsv" | tr -d '[:space:]')"
if [[ "${sandbox_runner_secret_span_leaks}" != "0" ]]; then
  echo "source smoke test found runner-token-like material in sandbox-rental-service span attributes" >&2
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

python3 - "${run_json_path}" "${org_id}" "${project_id}" "${repo_id}" "${credential_id}" "${checkout_grant_id}" "${backend_repo_id}" "${forgejo_execution_id}" "${forgejo_attempt_id}" "${forgejo_provider_job_id}" "${forgejo_allocation_id}" "${window_start}" "${window_end}" "${artifact_dir}" "${git_repo_url}" <<'PY'
import json
import sys

path, org_id, project_id, repo_id, credential_id, grant_id, backend_repo_id, forgejo_execution_id, forgejo_attempt_id, forgejo_provider_job_id, forgejo_allocation_id, window_start, window_end, artifact_dir, git_repo_url = sys.argv[1:16]
payload = json.load(open(path, encoding="utf-8"))
payload.update({
    "org_id": org_id,
    "project_id": project_id,
    "repo_id": repo_id,
    "git_credential_id": credential_id,
    "checkout_grant_id": grant_id,
    "forgejo_repository_id": backend_repo_id,
    "forgejo_runner_execution_id": forgejo_execution_id,
    "forgejo_runner_attempt_id": forgejo_attempt_id,
    "forgejo_runner_provider_job_id": forgejo_provider_job_id,
    "forgejo_runner_allocation_id": forgejo_allocation_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
    "git_repo_url": git_repo_url,
})
json.dump(payload, open(path, "w", encoding="utf-8"), indent=2, sort_keys=True)
print()
PY

echo "source-code-hosting smoke test ok: ${artifact_dir}"
