#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-secrets-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/secrets-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/responses" "${artifact_dir}/payloads" "${artifact_dir}/clickhouse" "${artifact_dir}/postgres"

tmp_files=()
cleanup() {
  for path in "${tmp_files[@]}"; do
    rm -f "${path}" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)
admin_secrets_token="${SECRETS_SERVICE_ACCESS_TOKEN}"
admin_identity_token="${IDENTITY_SERVICE_ACCESS_TOKEN}"
sandbox_rental_project_id="${SANDBOX_RENTAL_AUTH_PROJECT_ID}"
secrets_service_project_id="${SECRETS_SERVICE_AUTH_PROJECT_ID}"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-member --print)
member_secrets_token="${SECRETS_SERVICE_ACCESS_TOKEN}"

billing_fixture_path="${artifact_dir}/billing-fixture.json"
"${script_dir}/set-user-state.sh" \
  --email "acme-admin@${VERIFICATION_DOMAIN}" \
  --org "Acme Corp" \
  --product-id "sandbox" \
  --state "pro" \
  --balance-units "500000000000" >"${billing_fixture_path}"

org_id="$(
  python3 - "${billing_fixture_path}" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["org_id"])
PY
)"

remote_secrets_api() {
  local method="$1"
  local path="$2"
  local token="$3"
  local body_path="$4"
  local output_path="$5"
  local idempotency_key="${6:-}"
  local body_b64=""
  if [[ -n "${body_path}" ]]; then
    body_b64="$(base64 -w0 "${body_path}")"
  fi
  local request_b64
  request_b64="$(
    METHOD="${method}" API_PATH="${path}" API_TOKEN="${token}" BODY_B64="${body_b64}" IDEMPOTENCY_KEY="${idempotency_key}" python3 - <<'PY'
import base64
import json
import os
print(base64.b64encode(json.dumps({
    "method": os.environ["METHOD"],
    "path": os.environ["API_PATH"],
    "token": os.environ["API_TOKEN"],
    "body_b64": os.environ.get("BODY_B64", ""),
    "idempotency_key": os.environ.get("IDEMPOTENCY_KEY", ""),
}).encode()).decode())
PY
  )"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64, json, subprocess, sys
payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
body = base64.b64decode(payload.get(\"body_b64\") or \"\")
cmd = [
    \"curl\", \"-fsS\", \"-X\", payload[\"method\"],
    \"-H\", \"Authorization: Bearer \" + payload[\"token\"],
    \"-H\", \"Content-Type: application/json\",
]
if payload.get(\"idempotency_key\"):
    cmd += [\"-H\", \"Idempotency-Key: \" + payload[\"idempotency_key\"]]
if body:
    cmd += [\"--data-binary\", \"@-\"]
cmd.append(\"http://127.0.0.1:4251\" + payload[\"path\"])
result = subprocess.run(cmd, input=body if body else None, check=False)
if result.returncode != 0:
    raise SystemExit(\"curl failed for {} {}\".format(payload[\"method\"], payload[\"path\"]))
'" >"${output_path}"
}

remote_identity_api() {
  local method="$1"
  local path="$2"
  local token="$3"
  local body_path="$4"
  local output_path="$5"
  local idempotency_key="${6:-}"
  local body_b64=""
  if [[ -n "${body_path}" ]]; then
    body_b64="$(base64 -w0 "${body_path}")"
  fi
  local request_b64
  request_b64="$(
    METHOD="${method}" API_PATH="${path}" API_TOKEN="${token}" BODY_B64="${body_b64}" IDEMPOTENCY_KEY="${idempotency_key}" python3 - <<'PY'
import base64
import json
import os
print(base64.b64encode(json.dumps({
    "method": os.environ["METHOD"],
    "path": os.environ["API_PATH"],
    "token": os.environ["API_TOKEN"],
    "body_b64": os.environ.get("BODY_B64", ""),
    "idempotency_key": os.environ.get("IDEMPOTENCY_KEY", ""),
}).encode()).decode())
PY
  )"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64, json, subprocess, sys
payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
body = base64.b64decode(payload.get(\"body_b64\") or \"\")
cmd = [
    \"curl\", \"-fsS\", \"-X\", payload[\"method\"],
    \"-H\", \"Authorization: Bearer \" + payload[\"token\"],
    \"-H\", \"Content-Type: application/json\",
]
if payload.get(\"idempotency_key\"):
    cmd += [\"-H\", \"Idempotency-Key: \" + payload[\"idempotency_key\"]]
if body:
    cmd += [\"--data-binary\", \"@-\"]
cmd.append(\"http://127.0.0.1:4248\" + payload[\"path\"])
result = subprocess.run(cmd, input=body if body else None, check=False)
if result.returncode != 0:
    raise SystemExit(\"curl failed for {} {}\".format(payload[\"method\"], payload[\"path\"]))
'" >"${output_path}"
}

remote_governance_api() {
  local path="$1"
  local token="$2"
  local output_path="$3"
  local request_b64
  request_b64="$(
    API_PATH="${path}" API_TOKEN="${token}" python3 - <<'PY'
import base64
import json
import os
print(base64.b64encode(json.dumps({
    "path": os.environ["API_PATH"],
    "token": os.environ["API_TOKEN"],
}).encode()).decode())
PY
  )"
  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64, json, subprocess, sys
payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
cmd = [
    \"curl\", \"-fsS\",
    \"-H\", \"Authorization: Bearer \" + payload[\"token\"],
    \"http://127.0.0.1:4250\" + payload[\"path\"],
]
result = subprocess.run(cmd, check=False)
if result.returncode != 0:
    raise SystemExit(\"curl failed for governance {}\".format(payload[\"path\"]))
'" >"${output_path}"
}

mint_api_credential_token() {
  local client_id="$1"
  local client_secret="$2"
  local audience="$3"
  local token_b64
  token_b64="$(
    CLIENT_ID="${client_id}" CLIENT_SECRET="${client_secret}" AUDIENCE="${audience}" DOMAIN="${VERIFICATION_DOMAIN}" python3 - <<'PY'
import base64
import json
import os
print(base64.b64encode(json.dumps({
    "client_id": os.environ["CLIENT_ID"],
    "client_secret": os.environ["CLIENT_SECRET"],
    "audience": os.environ["AUDIENCE"],
    "domain": os.environ["DOMAIN"],
}).encode()).decode())
PY
  )"
  printf '%s\n' "${token_b64}" | verification_ssh "python3 -c '
import base64, json, sys, urllib.parse, urllib.request
payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
scope = \" \".join([
    \"openid\",
    \"profile\",
    \"urn:zitadel:iam:org:project:id:{}:aud\".format(payload[\"audience\"]),
    \"urn:zitadel:iam:org:projects:roles\",
])
body = urllib.parse.urlencode({\"grant_type\": \"client_credentials\", \"scope\": scope}).encode()
basic = base64.b64encode((payload[\"client_id\"] + \":\" + payload[\"client_secret\"]).encode()).decode()
request = urllib.request.Request(
    \"http://127.0.0.1:8085/oauth/v2/token\",
    data=body,
    headers={
        \"Host\": \"auth.\" + payload[\"domain\"],
        \"Authorization\": \"Basic \" + basic,
        \"Content-Type\": \"application/x-www-form-urlencoded\",
    },
    method=\"POST\",
)
with urllib.request.urlopen(request, timeout=5) as response:
    print(json.loads(response.read().decode())[\"access_token\"])
'"
}

remote_openbao_direct_verify_policy_probe() {
  local token="$1"
  local org="$2"
  local secret="$3"
  local transit_key="$4"
  local output_path="$5"
  local probe_b64
  probe_b64="$(
    API_TOKEN="${token}" ORG_ID="${org}" SECRET_NAME="${secret}" TRANSIT_KEY="${transit_key}" python3 - <<'PY'
import base64
import json
import os
print(base64.b64encode(json.dumps({
    "token": os.environ["API_TOKEN"],
    "org_id": os.environ["ORG_ID"],
    "secret_name": os.environ["SECRET_NAME"],
    "transit_key": os.environ["TRANSIT_KEY"],
}).encode()).decode())
PY
  )"
  printf '%s\n' "${probe_b64}" | verification_ssh "sudo python3 -c '
import base64, json, ssl, sys, urllib.error, urllib.request
payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
base = \"https://127.0.0.1:8200\"
ctx = ssl._create_unverified_context()

def request(path, token, body=None):
    headers = {\"X-Vault-Token\": token}
    data = None
    method = \"GET\"
    if body is not None:
        headers[\"Content-Type\"] = \"application/json\"
        data = json.dumps(body).encode()
        method = \"POST\"
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=5) as response:
            return response.status, json.loads(response.read().decode() or \"{}\")
    except urllib.error.HTTPError as exc:
        return exc.code, {}

login_status, login_payload = request(
    \"/v1/auth/jwt-\" + payload[\"org_id\"] + \"/login\",
    \"\",
    {\"role\": \"secrets-direct-verify-with-transit-key\", \"jwt\": payload[\"token\"]},
)
client_token = login_payload.get(\"auth\", {}).get(\"client_token\", \"\")
if login_status != 200 or not client_token:
    raise SystemExit(\"OpenBao direct verify role login failed\")

secret_status, _ = request(
    \"/v1/kv-\" + payload[\"org_id\"] + \"/data/secret/org/\" + payload[\"secret_name\"],
    client_token,
)
metadata_status, _ = request(
    \"/v1/kv-\" + payload[\"org_id\"] + \"/data/_platform/transit/\" + payload[\"transit_key\"],
    client_token,
)
if secret_status != 403:
    raise SystemExit(\"direct verify OpenBao token read app secret with status {}\".format(secret_status))
if metadata_status != 200:
    raise SystemExit(\"direct verify OpenBao token could not read transit metadata, status {}\".format(metadata_status))

json.dump({
    \"role\": \"secrets-direct-verify-with-transit-key\",
    \"secret_read_status\": secret_status,
    \"transit_metadata_status\": metadata_status,
}, sys.stdout, indent=2, sort_keys=True)
print()
'" >"${output_path}"
}

secret_name="proof-${run_id}"
transit_key_name="proof-${run_id}"
secret_value="$(python3 - <<'PY'
import secrets
print(secrets.token_hex(32))
PY
)"
secret_hash="$(printf '%s' "${secret_value}" | sha256sum | awk '{print $1}')"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

put_body="$(mktemp)"
tmp_files+=("${put_body}")
SECRET_VALUE="${secret_value}" python3 - >"${put_body}" <<'PY'
import json
import os
print(json.dumps({"kind": "secret", "value": os.environ["SECRET_VALUE"]}))
PY
remote_secrets_api PUT "/api/v1/secrets/${secret_name}" "${admin_secrets_token}" "${put_body}" "${artifact_dir}/responses/secret-put.json" "${run_id}-put"

api_credential_body="$(mktemp)"
api_credential_response="$(mktemp)"
tmp_files+=("${api_credential_body}" "${api_credential_response}")
python3 - "${run_id}" >"${api_credential_body}" <<'PY'
import json
import sys
run_id = sys.argv[1]
print(json.dumps({
    "display_name": f"secrets-proof-{run_id}",
    "auth_method": "client_secret",
    "permissions": [
        "secrets:secret:read",
        "secrets:secret:list",
        "secrets:transit:verify",
        "sandbox:execution:submit",
        "sandbox:execution:read",
        "sandbox:logs:read",
    ],
}))
PY
remote_identity_api POST "/api/v1/organization/api-credentials" "${admin_identity_token}" "${api_credential_body}" "${api_credential_response}" "${run_id}-api-credential"

read -r api_credential_id api_credential_client_id api_credential_client_secret < <(
  python3 - "${api_credential_response}" "${artifact_dir}/responses/api-credential-create-redacted.json" <<'PY'
import json
import sys
source, redacted_path = sys.argv[1:3]
payload = json.load(open(source, encoding="utf-8"))
material = payload["issued_material"]
print(payload["credential"]["credential_id"], material["client_id"], material["client_secret"])
redacted = payload.copy()
redacted["issued_material"] = dict(material)
redacted["issued_material"]["client_secret"] = "<redacted>"
json.dump(redacted, open(redacted_path, "w", encoding="utf-8"), indent=2, sort_keys=True)
PY
)
api_credential_secrets_token="$(mint_api_credential_token "${api_credential_client_id}" "${api_credential_client_secret}" "${secrets_service_project_id}")"
api_credential_sandbox_token="$(mint_api_credential_token "${api_credential_client_id}" "${api_credential_client_secret}" "${sandbox_rental_project_id}")"

read_response="$(mktemp)"
tmp_files+=("${read_response}")
remote_secrets_api GET "/api/v1/secrets/${secret_name}?kind=secret" "${member_secrets_token}" "" "${read_response}"
SECRET_VALUE="${secret_value}" python3 - "${read_response}" >"${artifact_dir}/responses/secret-read-redacted.json" <<'PY'
import json
import os
import sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("value") != os.environ["SECRET_VALUE"]:
    raise SystemExit("secret read did not return expected value")
payload["value"] = "<redacted>"
json.dump(payload, sys.stdout, indent=2, sort_keys=True)
print()
PY

api_read_response="$(mktemp)"
tmp_files+=("${api_read_response}")
remote_secrets_api GET "/api/v1/secrets/${secret_name}?kind=secret" "${api_credential_secrets_token}" "" "${api_read_response}"
SECRET_VALUE="${secret_value}" python3 - "${api_read_response}" >"${artifact_dir}/responses/api-credential-secret-read-redacted.json" <<'PY'
import json
import os
import sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("value") != os.environ["SECRET_VALUE"]:
    raise SystemExit("api credential secret read did not return expected value")
payload["value"] = "<redacted>"
json.dump(payload, sys.stdout, indent=2, sort_keys=True)
print()
PY

remote_secrets_api GET "/api/v1/secrets?kind=secret&limit=50" "${admin_secrets_token}" "" "${artifact_dir}/responses/secret-list.json"
python3 - "${secret_name}" "${artifact_dir}/responses/secret-list.json" <<'PY'
import json
import sys
name, path = sys.argv[1:3]
payload = json.load(open(path, encoding="utf-8"))
if name not in {item.get("name") for item in payload.get("secrets", [])}:
    raise SystemExit("secret list did not include proof secret")
PY

remote_secrets_api GET "/api/v1/secrets?kind=secret&limit=50" "${api_credential_secrets_token}" "" "${artifact_dir}/responses/api-credential-secret-list.json"
python3 - "${secret_name}" "${artifact_dir}/responses/api-credential-secret-list.json" <<'PY'
import json
import sys
name, path = sys.argv[1:3]
payload = json.load(open(path, encoding="utf-8"))
if name not in {item.get("name") for item in payload.get("secrets", [])}:
    raise SystemExit("api credential secret list did not include proof secret")
PY

transit_create="$(mktemp)"
tmp_files+=("${transit_create}")
python3 - "${transit_key_name}" >"${transit_create}" <<'PY'
import json
import sys
print(json.dumps({"name": sys.argv[1]}))
PY
remote_secrets_api POST "/api/v1/transit/keys" "${admin_secrets_token}" "${transit_create}" "${artifact_dir}/responses/transit-create.json" "${run_id}-transit-create"

python3 - "${artifact_dir}/responses/transit-create.json" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if not payload.get("public_key"):
    raise SystemExit("transit create did not return an ed25519 public key")
PY

transit_plaintext_b64="$(printf 'forge-metal transit proof %s' "${run_id}" | base64 -w0)"
transit_encrypt="$(mktemp)"
tmp_files+=("${transit_encrypt}")
python3 - "${transit_plaintext_b64}" >"${transit_encrypt}" <<'PY'
import json
import sys
print(json.dumps({"plaintext_base64": sys.argv[1]}))
PY
remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/encrypt" "${admin_secrets_token}" "${transit_encrypt}" "${artifact_dir}/responses/transit-encrypt.json"

ciphertext="$(
  python3 - "${artifact_dir}/responses/transit-encrypt.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["ciphertext"])
PY
)"
transit_decrypt="$(mktemp)"
tmp_files+=("${transit_decrypt}")
python3 - "${ciphertext}" >"${transit_decrypt}" <<'PY'
import json
import sys
print(json.dumps({"ciphertext": sys.argv[1]}))
PY
remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/decrypt" "${admin_secrets_token}" "${transit_decrypt}" "${artifact_dir}/responses/transit-decrypt.json"

python3 - "${transit_plaintext_b64}" "${artifact_dir}/responses/transit-decrypt.json" <<'PY'
import json
import sys
expected = sys.argv[1]
actual = json.load(open(sys.argv[2], encoding="utf-8"))["plaintext_base64"]
if actual != expected:
    raise SystemExit("transit decrypt returned wrong plaintext")
PY

transit_message_b64="$(printf 'forge-metal signature proof %s' "${run_id}" | base64 -w0)"
transit_sign="$(mktemp)"
tmp_files+=("${transit_sign}")
python3 - "${transit_message_b64}" >"${transit_sign}" <<'PY'
import json
import sys
print(json.dumps({"message_base64": sys.argv[1]}))
PY
remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/sign" "${admin_secrets_token}" "${transit_sign}" "${artifact_dir}/responses/transit-sign.json"

signature="$(
  python3 - "${artifact_dir}/responses/transit-sign.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["signature"])
PY
)"
transit_verify="$(mktemp)"
tmp_files+=("${transit_verify}")
python3 - "${transit_message_b64}" "${signature}" >"${transit_verify}" <<'PY'
import json
import sys
print(json.dumps({"message_base64": sys.argv[1], "signature": sys.argv[2]}))
PY
remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/verify" "${member_secrets_token}" "${transit_verify}" "${artifact_dir}/responses/transit-verify.json"

python3 - "${artifact_dir}/responses/transit-verify.json" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("valid") is not True:
    raise SystemExit("transit verify did not accept the signature")
PY

remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/verify" "${api_credential_secrets_token}" "${transit_verify}" "${artifact_dir}/responses/api-credential-transit-verify.json"

python3 - "${artifact_dir}/responses/api-credential-transit-verify.json" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("valid") is not True:
    raise SystemExit("api credential transit verify did not accept the signature")
PY

api_verify_credential_body="$(mktemp)"
api_verify_credential_response="$(mktemp)"
tmp_files+=("${api_verify_credential_body}" "${api_verify_credential_response}")
python3 - "${run_id}" >"${api_verify_credential_body}" <<'PY'
import json
import sys
run_id = sys.argv[1]
print(json.dumps({
    "display_name": f"secrets-proof-verify-only-{run_id}",
    "auth_method": "client_secret",
    "permissions": [
        "secrets:transit:verify",
    ],
}))
PY
remote_identity_api POST "/api/v1/organization/api-credentials" "${admin_identity_token}" "${api_verify_credential_body}" "${api_verify_credential_response}" "${run_id}-api-credential-verify-only"

read -r api_verify_credential_id api_verify_credential_client_id api_verify_credential_client_secret < <(
  python3 - "${api_verify_credential_response}" "${artifact_dir}/responses/api-credential-verify-only-create-redacted.json" <<'PY'
import json
import sys
source, redacted_path = sys.argv[1:3]
payload = json.load(open(source, encoding="utf-8"))
material = payload["issued_material"]
print(payload["credential"]["credential_id"], material["client_id"], material["client_secret"])
redacted = payload.copy()
redacted["issued_material"] = dict(material)
redacted["issued_material"]["client_secret"] = "<redacted>"
json.dump(redacted, open(redacted_path, "w", encoding="utf-8"), indent=2, sort_keys=True)
PY
)
api_verify_credential_secrets_token="$(mint_api_credential_token "${api_verify_credential_client_id}" "${api_verify_credential_client_secret}" "${secrets_service_project_id}")"

remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/verify" "${api_verify_credential_secrets_token}" "${transit_verify}" "${artifact_dir}/responses/api-credential-verify-only-transit-verify.json"
python3 - "${artifact_dir}/responses/api-credential-verify-only-transit-verify.json" <<'PY'
import json
import sys
if json.load(open(sys.argv[1], encoding="utf-8")).get("valid") is not True:
    raise SystemExit("verify-only api credential transit verify did not accept the signature")
PY

remote_openbao_direct_verify_policy_probe \
  "${api_verify_credential_secrets_token}" \
  "${org_id}" \
  "${secret_name}" \
  "${transit_key_name}" \
  "${artifact_dir}/responses/openbao-direct-verify-policy-probe.json"

remote_secrets_api POST "/api/v1/transit/keys/${transit_key_name}/rotate" "${admin_secrets_token}" "" "${artifact_dir}/responses/transit-rotate.json" "${run_id}-transit-rotate"

python3 - "${artifact_dir}/responses/transit-rotate.json" <<'PY'
import json
import sys
payload = json.load(open(sys.argv[1], encoding="utf-8"))
if payload.get("current_version") != "2":
    raise SystemExit(f"transit rotate returned current_version={payload.get('current_version')}, expected 2")
if not payload.get("public_key"):
    raise SystemExit("transit rotate did not return an ed25519 public key")
PY

api_base_url="${BASE_URL:-https://rentasandbox.${VERIFICATION_DOMAIN}}"
submit_payload="${artifact_dir}/payloads/sandbox-secret-injection.json"
python3 - "${run_id}" "${secret_name}" >"${submit_payload}" <<'PY'
import json
import shlex
import sys
run_id, secret_name = sys.argv[1:3]
command = "set -eu; hash=$(printf '%s' \"$PROOF_SECRET\" | sha256sum | awk '{print $1}'); printf 'secret-injection-ok hash=%s\\n' \"$hash\""
print(json.dumps({
    "kind": "direct",
    "idempotency_key": f"{run_id}-sandbox-injection",
    "run_command": command,
    "max_wall_seconds": 120,
    "secret_env": [
        {
            "env_name": "PROOF_SECRET",
            "kind": "secret",
            "secret_name": secret_name,
            "scope_level": "org",
        }
    ],
}))
PY

submit_response="${artifact_dir}/responses/sandbox-submit.json"
curl -fsS \
  -H "Authorization: Bearer ${api_credential_sandbox_token}" \
  -H "Content-Type: application/json" \
  --data-binary "@${submit_payload}" \
  "${api_base_url%/}/api/v1/executions" >"${submit_response}"

execution_id="$(
  python3 - "${submit_response}" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["execution_id"])
PY
)"

status="queued"
for _ in $(seq 1 180); do
  curl -fsS \
    -H "Authorization: Bearer ${api_credential_sandbox_token}" \
    "${api_base_url%/}/api/v1/executions/${execution_id}" >"${artifact_dir}/responses/sandbox-execution.json"
  status="$(
    python3 - "${artifact_dir}/responses/sandbox-execution.json" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1], encoding="utf-8"))["status"])
PY
  )"
  case "${status}" in
    succeeded|failed|canceled|lost)
      break
      ;;
  esac
  sleep 2
done

if [[ "${status}" != "succeeded" ]]; then
  echo "sandbox execution did not succeed: ${status}" >&2
  exit 1
fi

curl -fsS \
  -H "Authorization: Bearer ${api_credential_sandbox_token}" \
  "${api_base_url%/}/api/v1/executions/${execution_id}/logs" >"${artifact_dir}/responses/sandbox-logs.json"

python3 - "${secret_hash}" "${artifact_dir}/responses/sandbox-logs.json" <<'PY'
import json
import sys
expected_hash, path = sys.argv[1:3]
logs = json.load(open(path, encoding="utf-8")).get("logs", "")
needle = f"secret-injection-ok hash={expected_hash}"
if needle not in logs:
    raise SystemExit("sandbox logs did not contain expected secret hash proof")
PY

printf '%s' "${secret_value}" | verification_ssh "sudo python3 -c '
import json
import os
import sqlite3
import sys

secret = sys.stdin.read().encode()
db_path = \"/var/lib/forge-metal/vm-orchestrator/state.db\"
paths = [db_path, db_path + \"-wal\", db_path + \"-shm\"]
hits = []
checked = []
for path in paths:
    if not os.path.exists(path):
        continue
    checked.append(path)
    with open(path, \"rb\") as handle:
        if secret and secret in handle.read():
            hits.append(path)
if os.path.exists(db_path):
    conn = sqlite3.connect(db_path)
    try:
        for rowid, spec_json in conn.execute(\"SELECT rowid, spec_json FROM execs\"):
            if secret.decode() in (spec_json or \"\"):
                hits.append(f\"execs.spec_json:{rowid}\")
    finally:
        conn.close()
if hits:
    raise SystemExit(\"vm-orchestrator persisted secret material: \" + \", \".join(hits))
json.dump({\"checked_paths\": checked, \"secret_hits\": 0}, sys.stdout, indent=2, sort_keys=True)
print()
'" >"${artifact_dir}/responses/vm-orchestrator-secret-persistence.json"

remote_secrets_api DELETE "/api/v1/secrets/${secret_name}?kind=secret&scope_level=org" "${admin_secrets_token}" "" "${artifact_dir}/responses/secret-delete.json" "${run_id}-delete"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/pg.sh sandbox_rental --query "
    COPY (
      SELECT env_name, kind, secret_name, scope_level, grant_id
      FROM execution_secret_env
      WHERE execution_id = '${execution_id}'
      ORDER BY sort_order
    ) TO STDOUT WITH CSV HEADER;
  "
) >"${artifact_dir}/postgres/sandbox-secret-env.csv"

python3 - "${artifact_dir}/postgres/sandbox-secret-env.csv" <<'PY'
import csv
import sys
rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
if not rows:
    raise SystemExit("sandbox execution_secret_env did not persist secret refs")
missing = [row.get("env_name", "") for row in rows if not row.get("grant_id")]
if missing:
    raise SystemExit(f"sandbox execution_secret_env rows missing grant_id: {missing}")
for row in rows:
    for column in ("env_name", "kind", "secret_name", "scope_level"):
        if not row.get(column):
            raise SystemExit(f"sandbox execution_secret_env row missing {column}")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/pg.sh sandbox_rental --query "
    COPY (
      SELECT count(*)
      FROM information_schema.columns
      WHERE table_schema = 'public'
        AND table_name = 'execution_secret_env'
        AND column_name = 'grant_token'
    ) TO STDOUT;
  "
) >"${artifact_dir}/postgres/sandbox-secret-env-grant-token-column-count.tsv"
if [[ "$(tr -d '[:space:]' <"${artifact_dir}/postgres/sandbox-secret-env-grant-token-column-count.tsv")" != "0" ]]; then
  echo "sandbox execution_secret_env still persists grant_token" >&2
  exit 1
fi

(
  cd "${VERIFICATION_REPO_ROOT}"
  make pg-list
) >"${artifact_dir}/postgres/pg-list.txt"
if grep -Eq '(^|[[:space:]|])secrets_service([[:space:]|])' "${artifact_dir}/postgres/pg-list.txt"; then
  echo "secrets_service PostgreSQL database is still visible in make pg-list" >&2
  exit 1
fi

verification_ssh "sudo python3 -c '
import json, ssl, sys, urllib.error, urllib.request
base = \"https://127.0.0.1:8200\"
ctx = ssl._create_unverified_context()
token = open(\"/etc/credstore/openbao/root-token\", encoding=\"utf-8\").read().strip()

def get_status(path):
    request = urllib.request.Request(base + path, headers={\"X-Vault-Token\": token})
    try:
        with urllib.request.urlopen(request, context=ctx, timeout=5) as response:
            return response.status, json.loads(response.read().decode())
    except urllib.error.HTTPError as exc:
        return exc.code, {}

legacy_status, _ = get_status(\"/v1/platform-internal/data/service-credentials/secrets-service/internal-injection-token\")
legacy_metadata_status, _ = get_status(\"/v1/platform-internal/metadata/service-credentials/secrets-service/internal-injection-token\")
unsafe = {
    \"openbao-root-token\": get_status(\"/v1/platform-internal/data/openbao/root-token\")[0],
    \"openbao-unseal-key-1\": get_status(\"/v1/platform-internal/data/openbao/unseal-key-1\")[0],
    \"zitadel-masterkey\": get_status(\"/v1/platform-internal/data/zitadel/masterkey\")[0],
}
unsafe_metadata = {
    \"openbao-root-token\": get_status(\"/v1/platform-internal/metadata/openbao/root-token\")[0],
    \"openbao-unseal-key-1\": get_status(\"/v1/platform-internal/metadata/openbao/unseal-key-1\")[0],
    \"zitadel-masterkey\": get_status(\"/v1/platform-internal/metadata/zitadel/masterkey\")[0],
}
if legacy_status != 404 or legacy_metadata_status != 404:
    raise SystemExit(\"legacy OpenBao internal injection token document or metadata is still present\")
for name, status_code in unsafe.items():
    if status_code != 404:
        raise SystemExit(f\"unsafe bootstrap credential {name} unexpectedly present in OpenBao\")
for name, status_code in unsafe_metadata.items():
    if status_code != 404:
        raise SystemExit(f\"unsafe bootstrap credential metadata {name} unexpectedly present in OpenBao\")
json.dump({
    \"legacy_injection_token_status\": legacy_status,
    \"legacy_injection_token_metadata_status\": legacy_metadata_status,
    \"unsafe_bootstrap_statuses\": unsafe,
    \"unsafe_bootstrap_metadata_statuses\": unsafe_metadata,
}, sys.stdout, indent=2, sort_keys=True)
print()
'" >"${artifact_dir}/responses/openbao-service-credential-dogfood.json"

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
        --param_window_start="${window_start}" \
        --param_window_end="${window_end}" \
        --param_org_id="${org_id}" \
        --param_api_credential_id="${api_credential_id:-}" \
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

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName IN (
      'secrets.secret.put',
      'secrets.secret.read',
      'secrets.secret.list',
      'secrets.secret.delete',
      'secrets.transit.key.create',
      'secrets.transit.key.rotate',
      'secrets.transit.encrypt',
      'secrets.transit.decrypt',
      'secrets.transit.sign',
      'secrets.transit.verify'
    )
" 10 "${artifact_dir}/clickhouse/secrets-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND startsWith(SpanName, 'secrets.bao.')
    AND arrayElement(SpanAttributes, 'bao.request_id') != ''
" 10 "${artifact_dir}/clickhouse/secrets-bao-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName IN ('secrets.billing.reserve', 'secrets.billing.settle')
    AND arrayElement(SpanAttributes, 'billing.product_id') = 'secrets'
    AND arrayElement(SpanAttributes, 'billing.sku_id') IN ('secrets_kv_operation', 'secrets_transit_operation')
" 26 "${artifact_dir}/clickhouse/secrets-billing-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName = 'sandbox-rental.secrets.resolve'
" 1 "${artifact_dir}/clickhouse/sandbox-secret-resolve-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'sandbox-rental-service'
    AND SpanName IN ('secrets.injection.reference.verify', 'auth.spiffe.mtls.client')
" 2 "${artifact_dir}/clickhouse/sandbox-spiffe-injection-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    -- A warm OpenBao token cache suppresses a fresh jwt_svid.login, so the
    -- injection proof accepts either the login span or the cache span.
    AND SpanName IN ('auth.spiffe.mtls.server', 'secrets.injection.resolve', 'secrets.injection.service_token.exchange', 'auth.spiffe.jwt_svid.fetch', 'secrets.bao.jwt_svid.login', 'secrets.bao.token.cache')
" 5 "${artifact_dir}/clickhouse/secrets-spiffe-injection-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'vm-orchestrator'
    AND SpanName = 'vmorchestrator.exec.spec.redact'
    AND arrayElement(SpanAttributes, 'vmorchestrator.exec.env_values_redacted') = 'true'
" 1 "${artifact_dir}/clickhouse/vmorchestrator-redaction-spans-count.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND org_id = {org_id:String}
    AND audit_event IN (
      'secrets.secret.write',
      'secrets.secret.read',
      'secrets.secret.list',
      'secrets.secret.delete',
      'secrets.secret.inject',
      'secrets.transit_key.create',
      'secrets.transit_key.rotate',
      'secrets.transit_key.encrypt',
      'secrets.transit_key.decrypt',
      'secrets.transit_key.sign',
      'secrets.transit_key.verify'
    )
    AND (startsWith(secret_mount, 'kv-') OR startsWith(secret_mount, 'transit-'))
    AND openbao_request_id != ''
    AND openbao_accessor_hash != ''
" 11 "${artifact_dir}/clickhouse/secrets-audit-count.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND org_id = {org_id:String}
    AND credential_id = {api_credential_id:String}
    AND audit_event IN ('secrets.secret.read', 'secrets.secret.list', 'secrets.transit_key.verify')
    AND decision = 'allow'
    AND openbao_request_id != ''
" 3 "${artifact_dir}/clickhouse/secrets-api-credential-audit-count.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM metering
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND org_id = {org_id:String}
    AND product_id = 'secrets'
    AND source_type = 'secrets_api_operation'
    AND reservation_shape = 'count'
    AND reserved_quantity = 1
    AND actual_quantity = 1
" 13 "${artifact_dir}/clickhouse/secrets-metering-count.tsv"

remote_governance_api "/api/v1/governance/audit/events?credential_id=${api_credential_id}&service_name=secrets-service&limit=50" "${admin_identity_token}" "${artifact_dir}/responses/governance-api-credential-audit.json"
python3 - "${artifact_dir}/responses/governance-api-credential-audit.json" "${api_credential_id}" <<'PY'
import json
import sys
path, credential_id = sys.argv[1:3]
payload = json.load(open(path, encoding="utf-8"))
events = payload.get("events", [])
if len(events) < 3:
    raise SystemExit(f"governance credential filter returned {len(events)} events, expected at least 3")
bad = [event.get("credential_id") for event in events if event.get("credential_id") != credential_id]
if bad:
    raise SystemExit(f"governance credential filter leaked other credential IDs: {bad}")
PY

remote_governance_api "/api/v1/governance/audit/events?credential_id=${api_credential_id}-missing&service_name=secrets-service&limit=50" "${admin_identity_token}" "${artifact_dir}/responses/governance-api-credential-audit-empty.json"
python3 - "${artifact_dir}/responses/governance-api-credential-audit-empty.json" <<'PY'
import json
import sys
events = json.load(open(sys.argv[1], encoding="utf-8")).get("events", [])
if events:
    raise SystemExit("governance credential filter returned rows for a missing credential id")
PY

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database default \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --query "
      SELECT Timestamp, ServiceName, SpanName, StatusCode, intDiv(Duration, 1000000) AS duration_ms
      FROM otel_traces
      WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND ServiceName IN ('secrets-service', 'sandbox-rental-service', 'vm-orchestrator')
      ORDER BY Timestamp
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/otel-traces.tsv"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh \
    --database forge_metal \
    --param_window_start="${window_start}" \
    --param_window_end="${window_end}" \
    --param_org_id="${org_id}" \
    --query "
      SELECT recorded_at, audit_event, result, target_id, target_path_hash, secret_version, key_id
        , credential_id, secret_mount, openbao_request_id, openbao_accessor_hash
      FROM audit_events
      WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
        AND org_id = {org_id:String}
        AND startsWith(audit_event, 'secrets.')
      ORDER BY recorded_at, sequence
      FORMAT TSVWithNames
    "
) >"${artifact_dir}/clickhouse/secrets-audit-events.tsv"

python3 - "${artifact_dir}/clickhouse/secrets-audit-events.tsv" "${transit_key_name}" <<'PY'
import csv
import sys

path, transit_key_name = sys.argv[1:3]
rows = list(csv.DictReader(open(path, encoding="utf-8"), delimiter="\t"))
events = {row["audit_event"]: row for row in rows}
required = {
    "secrets.secret.write",
    "secrets.secret.read",
    "secrets.secret.list",
    "secrets.secret.delete",
    "secrets.secret.inject",
    "secrets.transit_key.create",
    "secrets.transit_key.rotate",
    "secrets.transit_key.encrypt",
    "secrets.transit_key.decrypt",
    "secrets.transit_key.sign",
    "secrets.transit_key.verify",
}
missing = sorted(required.difference(events))
if missing:
    raise SystemExit(f"missing secrets audit events: {', '.join(missing)}")

for event, row in events.items():
    if event not in required:
        continue
    if not (row["secret_mount"].startswith("kv-") or row["secret_mount"].startswith("transit-")):
        raise SystemExit(f"{event} secret_mount={row['secret_mount']!r} was not an OpenBao mount")
    if not row["openbao_request_id"]:
        raise SystemExit(f"{event} missing openbao_request_id")
    if not row["openbao_accessor_hash"]:
        raise SystemExit(f"{event} missing openbao_accessor_hash")

for event in ("secrets.secret.write", "secrets.secret.read", "secrets.secret.delete", "secrets.secret.inject"):
    row = events[event]
    if not row["target_path_hash"]:
        raise SystemExit(f"{event} missing target_path_hash")
    if row["secret_version"] != "1":
        raise SystemExit(f"{event} secret_version={row['secret_version']}, expected 1")
    if row["key_id"]:
        raise SystemExit(f"{event} unexpectedly has key_id")
list_row = events["secrets.secret.list"]
if list_row["target_path_hash"] or list_row["secret_version"] != "0" or list_row["key_id"]:
    raise SystemExit("secret list audit target should describe the collection, not one secret")
create = events["secrets.transit_key.create"]
if create["target_path_hash"] or create["secret_version"] != "1" or not create["key_id"]:
    raise SystemExit("transit key create audit target was not keyed by key_id")
rotate = events["secrets.transit_key.rotate"]
if rotate["target_path_hash"] or rotate["secret_version"] != "2" or not rotate["key_id"]:
    raise SystemExit("transit key rotate audit target was not keyed by the rotated key")
for event in ("secrets.transit_key.encrypt", "secrets.transit_key.decrypt", "secrets.transit_key.sign", "secrets.transit_key.verify"):
    row = events[event]
    if row["target_path_hash"] or row["target_id"] != transit_key_name:
        raise SystemExit(f"{event} audit target mismatch")
    if row["secret_version"] != "1":
        raise SystemExit(f"{event} secret_version={row['secret_version']}, expected 1")
PY

cat >"${artifact_dir}/run.json" <<EOF
{
  "run_id": "${run_id}",
  "org_id": "${org_id}",
  "api_credential_id": "${api_credential_id}",
  "api_verify_credential_id": "${api_verify_credential_id}",
  "secret_name": "${secret_name}",
  "secret_sha256": "${secret_hash}",
  "transit_key_name": "${transit_key_name}",
  "sandbox_execution_id": "${execution_id}",
  "window_start": "${window_start}",
  "window_end": "${window_end}",
  "artifact_dir": "${artifact_dir}"
}
EOF

echo "secrets proof ok: ${artifact_dir}"
