#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-openbao-tenancy-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/openbao-tenancy-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

acme_admin_env="$(mktemp)"
acme_member_env="$(mktemp)"
platform_admin_env="$(mktemp)"
cleanup_env() {
  rm -f "${acme_admin_env}" "${acme_member_env}" "${platform_admin_env}"
}
trap cleanup_env EXIT

"${script_dir}/assume-persona.sh" acme-admin --print >"${acme_admin_env}"
"${script_dir}/assume-persona.sh" acme-member --print >"${acme_member_env}"
"${script_dir}/assume-persona.sh" platform-admin --print >"${platform_admin_env}"

set +u
# shellcheck source=/dev/null
source "${acme_admin_env}"
acme_admin_token="${SECRETS_SERVICE_ACCESS_TOKEN}"
secrets_project_id="${SECRETS_SERVICE_AUTH_AUDIENCE}"
# shellcheck source=/dev/null
source "${acme_member_env}"
acme_member_token="${SECRETS_SERVICE_ACCESS_TOKEN}"
# shellcheck source=/dev/null
source "${platform_admin_env}"
platform_admin_token="${SECRETS_SERVICE_ACCESS_TOKEN}"
set -u

claim_json="$(
  python3 - "${acme_admin_token}" "${acme_member_token}" "${platform_admin_token}" <<'PY'
import base64
import json
import sys

def decode(token):
    part = token.split(".")[1]
    part += "=" * (-len(part) % 4)
    return json.loads(base64.urlsafe_b64decode(part.encode()))

acme_admin, acme_member, platform_admin = [decode(token) for token in sys.argv[1:4]]
payload = {
    "acme_org_id": acme_admin["urn:zitadel:iam:user:resourceowner:id"],
    "acme_member_org_id": acme_member["urn:zitadel:iam:user:resourceowner:id"],
    "platform_org_id": platform_admin["urn:zitadel:iam:user:resourceowner:id"],
    "acme_admin_jti_present": bool(acme_admin.get("jti")),
    "acme_member_jti_present": bool(acme_member.get("jti")),
    "platform_admin_jti_present": bool(platform_admin.get("jti")),
}
if payload["acme_org_id"] != payload["acme_member_org_id"]:
    raise SystemExit("Acme admin/member tokens do not share an organization")
print(json.dumps(payload, sort_keys=True))
PY
)"
acme_org_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["acme_org_id"])' <<<"${claim_json}")"
platform_org_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["platform_org_id"])' <<<"${claim_json}")"
proof_value="$(python3 -c 'import secrets; print(secrets.token_urlsafe(32))')"

remote_payload_b64="$(
  python3 - \
    "${VERIFICATION_DOMAIN}" \
    "${run_id}" \
    "${secrets_project_id}" \
    "spiffe://spiffe.${VERIFICATION_DOMAIN}/svc/secrets-service" \
    "${acme_org_id}" \
    "${platform_org_id}" \
    "${acme_admin_token}" \
    "${acme_member_token}" \
    "${platform_admin_token}" \
    "${proof_value}" <<'PY'
import base64
import json
import sys

keys = [
    "domain",
    "run_id",
    "secrets_project_id",
    "spire_secrets_service_id",
    "acme_org_id",
    "platform_org_id",
    "acme_admin_token",
    "acme_member_token",
    "platform_admin_token",
    "proof_value",
]
payload = dict(zip(keys, sys.argv[1:]))
print(base64.b64encode(json.dumps(payload).encode()).decode())
PY
)"

remote_python="$(
  cat <<'PY'
import base64
import hashlib
import json
import ssl
import sys
import urllib.error
import urllib.request

payload = json.loads(base64.b64decode(sys.stdin.read()).decode())
base = "https://127.0.0.1:8200"
ctx = ssl._create_unverified_context()

def request(method, path, token=None, body=None):
    data = None
    headers = {}
    if token:
        headers["X-Vault-Token"] = token
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=5) as resp:
            text = resp.read().decode()
            parsed = json.loads(text) if text else {}
            return resp.status, parsed, dict(resp.headers)
    except urllib.error.HTTPError as exc:
        text = exc.read().decode()
        parsed = json.loads(text) if text else {}
        return exc.code, parsed, dict(exc.headers)

def require_status(label, status, expected):
    if status not in expected:
        raise SystemExit(f"{label} returned HTTP {status}, expected {sorted(expected)}")

def response_request_id(body, headers):
    return headers.get("X-Vault-Request-Id") or body.get("request_id") or ""

root_token = open("/etc/credstore/openbao/root-token", encoding="utf-8").read().strip()
admin_pat = open("/etc/zitadel/admin.pat", encoding="utf-8").read().strip()

org_req = urllib.request.Request(
    "http://127.0.0.1:8085/v2/organizations/_search",
    data=b"{}",
    headers={
        "Authorization": "Bearer " + admin_pat,
        "Host": "auth." + payload["domain"],
        "Content-Type": "application/json",
    },
)
org_payload = json.loads(urllib.request.urlopen(org_req, timeout=5).read().decode())
orgs = [
    {
        "id": org["id"],
        "name": org["name"],
        "primary_domain": org.get("primaryDomain", ""),
    }
    for org in org_payload.get("result", [])
    if org.get("state") == "ORGANIZATION_STATE_ACTIVE"
]
if not orgs:
    raise SystemExit("Zitadel returned no active organizations")

status, mounts_body, mounts_headers = request("GET", "/v1/sys/mounts", root_token)
require_status("sys/mounts", status, {200})
status, auth_body, auth_headers = request("GET", "/v1/sys/auth", root_token)
require_status("sys/auth", status, {200})
mounts = set((mounts_body.get("data") or {}).keys())
auth_mounts = set((auth_body.get("data") or {}).keys())

policy_checks = []
role_checks = []
for org in orgs:
    org_id = org["id"]
    expected_mounts = {f"kv-{org_id}/", f"transit-{org_id}/"}
    expected_auth = f"jwt-{org_id}/"
    missing_mounts = sorted(expected_mounts - mounts)
    if missing_mounts:
        raise SystemExit(f"missing OpenBao mounts for org {org_id}: {missing_mounts}")
    if expected_auth not in auth_mounts:
        raise SystemExit(f"missing OpenBao JWT auth mount for org {org_id}: {expected_auth}")
    for policy in [f"secrets-{org_id}-read-only", f"secrets-{org_id}-read-write", f"secrets-{org_id}-injection-read"]:
        p_status, p_body, p_headers = request("GET", f"/v1/sys/policies/acl/{policy}", root_token)
        require_status(f"policy {policy}", p_status, {200})
        policy_checks.append({"policy": policy, "request_id": response_request_id(p_body, p_headers)})
    for role, expected_policy, project_role in [
        ("secrets-owner", f"secrets-{org_id}-read-write", "owner"),
        ("secrets-admin", f"secrets-{org_id}-read-write", "admin"),
        ("secrets-member", f"secrets-{org_id}-read-only", "member"),
    ]:
        r_status, r_body, r_headers = request("GET", f"/v1/auth/jwt-{org_id}/role/{role}", root_token)
        require_status(f"role {org_id} {role}", r_status, {200})
        data = r_body.get("data") or {}
        token_policies = set(data.get("token_policies") or [])
        if expected_policy not in token_policies:
            raise SystemExit(f"role {org_id} {role} missing policy {expected_policy}")
        bound_claims = data.get("bound_claims") or {}
        role_claim = f"/urn:zitadel:iam:org:project:{payload['secrets_project_id']}:roles/{project_role}/{org_id}"
        if bound_claims.get("urn:zitadel:iam:user:resourceowner:id") != org_id or role_claim not in bound_claims:
            raise SystemExit(f"role {org_id} {role} has unexpected bound_claims")
        role_checks.append({"role": role, "org_id": org_id, "request_id": response_request_id(r_body, r_headers)})
    spiffe_role = f"secrets-injection-{org_id}"
    s_status, s_body, s_headers = request("GET", f"/v1/auth/spiffe-jwt/role/{spiffe_role}", root_token)
    require_status(f"SPIFFE workload role {spiffe_role}", s_status, {200})
    s_data = s_body.get("data") or {}
    if f"secrets-{org_id}-injection-read" not in set(s_data.get("token_policies") or []):
        raise SystemExit(f"SPIFFE workload role {spiffe_role} missing injection-read policy")
    if (s_data.get("bound_claims") or {}).get("sub") != payload["spire_secrets_service_id"]:
        raise SystemExit(f"SPIFFE workload role {spiffe_role} is not bound to secrets-service SPIFFE ID")
    if "openbao" not in set(s_data.get("bound_audiences") or []):
        raise SystemExit(f"SPIFFE workload role {spiffe_role} is not bound to openbao audience")
    role_checks.append({"role": spiffe_role, "org_id": org_id, "request_id": response_request_id(s_body, s_headers)})

def login(org_id, role, jwt):
    status, body, headers = request("POST", f"/v1/auth/jwt-{org_id}/login", body={"role": role, "jwt": jwt})
    require_status(f"jwt login {org_id} {role}", status, {200})
    auth = body.get("auth") or {}
    token = auth.get("client_token") or ""
    accessor = auth.get("accessor") or ""
    lease_duration = int(auth.get("lease_duration") or 0)
    if not token:
        raise SystemExit(f"jwt login {org_id} {role} returned no OpenBao token")
    if lease_duration <= 0 or lease_duration > 900:
        raise SystemExit(f"jwt login {org_id} {role} returned bad lease duration {lease_duration}")
    return {
        "token": token,
        "lease_duration": lease_duration,
        "accessor_hash": hashlib.sha256(accessor.encode()).hexdigest() if accessor else "",
        "request_id": response_request_id(body, headers),
    }

acme_admin = login(payload["acme_org_id"], "secrets-admin", payload["acme_admin_token"])
acme_member = login(payload["acme_org_id"], "secrets-member", payload["acme_member_token"])

bad_status, bad_body, bad_headers = request(
    "POST",
    f"/v1/auth/jwt-{payload['platform_org_id']}/login",
    body={"role": "secrets-admin", "jwt": payload["acme_admin_token"]},
)
require_status("cross-org jwt login", bad_status, {400, 403})

secret_path = f"/v1/kv-{payload['acme_org_id']}/data/_proof/openbao-tenancy-proof"
status, put_body, put_headers = request("POST", secret_path, acme_admin["token"], {"data": {"value": payload["proof_value"]}})
require_status("admin kv put", status, {200, 204})
status, get_body, get_headers = request("GET", secret_path, acme_member["token"])
require_status("member kv get", status, {200})
read_value = ((get_body.get("data") or {}).get("data") or {}).get("value")
if read_value != payload["proof_value"]:
    raise SystemExit("member kv get returned the wrong value")
member_write_status, member_write_body, member_write_headers = request(
    "POST",
    secret_path,
    acme_member["token"],
    {"data": {"value": "forbidden"}},
)
require_status("member kv put", member_write_status, {403})
cross_read_status, cross_read_body, cross_read_headers = request(
    "GET",
    f"/v1/kv-{payload['platform_org_id']}/data/_proof/openbao-tenancy-proof",
    acme_admin["token"],
)
require_status("cross-org kv get", cross_read_status, {403})
for org_id in [payload["acme_org_id"], payload["platform_org_id"]]:
    request("DELETE", f"/v1/kv-{org_id}/metadata/secret/openbao-tenancy-proof", root_token)

result = {
    "layout": "root_mounts",
    "run_id": payload["run_id"],
    "secrets_project_id": payload["secrets_project_id"],
    "orgs": orgs,
    "org_count": len(orgs),
    "acme_org_id": payload["acme_org_id"],
    "platform_org_id": payload["platform_org_id"],
    "claims": {
        "jti_required": True,
    },
    "openbao": {
        "mounts": sorted(mounts),
        "auth_mounts": sorted(auth_mounts),
        "policy_checks": policy_checks,
        "role_checks": role_checks,
        "login": {
            "admin_lease_duration": acme_admin["lease_duration"],
            "member_lease_duration": acme_member["lease_duration"],
            "admin_accessor_hash": acme_admin["accessor_hash"],
            "member_accessor_hash": acme_member["accessor_hash"],
            "admin_request_id": acme_admin["request_id"],
            "member_request_id": acme_member["request_id"],
            "cross_org_login_status": bad_status,
        },
        "kv": {
            "put_request_id": response_request_id(put_body, put_headers),
            "get_request_id": response_request_id(get_body, get_headers),
            "member_write_status": member_write_status,
            "cross_read_status": cross_read_status,
            "value_sha256": hashlib.sha256(payload["proof_value"].encode()).hexdigest(),
        },
    },
}
print(json.dumps(result, indent=2, sort_keys=True))
PY
)"
remote_python_q="$(printf '%q' "${remote_python}")"
printf '%s' "${remote_payload_b64}" | verification_ssh "sudo python3 -c ${remote_python_q}" >"${artifact_dir}/openbao-tenancy-state.json"

if ! python3 - "${claim_json}" <<'PY'
import json
import sys

claims = json.loads(sys.argv[1])
missing = [key for key, value in claims.items() if key.endswith("_jti_present") and not value]
if missing:
    raise SystemExit("missing jti claims: " + ", ".join(missing))
PY
then
  echo "ERROR: Zitadel persona token missing jti claim" >&2
  exit 1
fi

emit_span() {
  local span_name="$1"
  local attrs_json="$2"
  (
    cd "${VERIFICATION_REPO_ROOT}"
    PROOF_SPAN_SERVICE="platform-ansible" \
    PROOF_SPAN_NAME="${span_name}" \
    PROOF_SPAN_ATTRS_JSON="${attrs_json}" \
      go run ./src/otel/cmd/proof-span
  )
}

with_otlp_tunnel() {
  local port
  port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
  ssh -N \
    -o ExitOnForwardFailure=yes \
    -o ServerAliveInterval=15 \
    -o ServerAliveCountMax=3 \
    -o StrictHostKeyChecking=no \
    -L "${port}:127.0.0.1:4317" \
    "${VERIFICATION_REMOTE_USER}@${VERIFICATION_REMOTE_HOST}" </dev/null >/dev/null 2>&1 &
  local tunnel_pid=$!
  trap 'kill "${tunnel_pid}" 2>/dev/null || true; wait "${tunnel_pid}" 2>/dev/null || true' RETURN

  for _ in $(seq 1 20); do
    if python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
      break
    fi
    sleep 0.25
  done
  if ! python3 -c "import socket; socket.create_connection(('127.0.0.1', ${port}), 1).close()" 2>/dev/null; then
    echo "ERROR: OTLP tunnel to ${VERIFICATION_REMOTE_HOST} did not come up on 127.0.0.1:${port}" >&2
    return 1
  fi

  export VERSELF_OTLP_ENDPOINT="127.0.0.1:${port}"
  export VERSELF_DEPLOY_RUN_KEY="${run_id}"
  export VERSELF_DEPLOY_KIND="openbao-tenancy-proof"
  # shellcheck source=src/platform/scripts/deploy_identity.sh
  source "${script_dir}/deploy_identity.sh"

  python3 - "${run_id}" "${artifact_dir}/openbao-tenancy-state.json" <<'PY' | while IFS= read -r line; do
import json
import sys

run_id, state_path = sys.argv[1:3]
state = json.load(open(state_path, encoding="utf-8"))
for org in state["orgs"]:
    attrs = {
        "verself.proof_run_id": run_id,
        "verself.org_id": org["id"],
        "bao.layout": state["layout"],
        "bao.mount": "kv-" + org["id"],
        "bao.auth_mount": "jwt-" + org["id"],
    }
    print(json.dumps({"name": "openbao.tenancy.reconcile." + org["id"], "attrs": attrs}, sort_keys=True))
PY
    span_name="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["name"])' <<<"${line}")"
    attrs_json="$(python3 -c 'import json,sys; print(json.dumps(json.loads(sys.stdin.read())["attrs"], sort_keys=True))' <<<"${line}")"
    emit_span "${span_name}" "${attrs_json}"
  done
}

with_otlp_tunnel
window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

org_count="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8"))["org_count"])' "${artifact_dir}/openbao-tenancy-state.json")"

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  local count="0"
  for _ in $(seq 1 45); do
    (
      cd "${VERIFICATION_PLATFORM_ROOT}"
      ./scripts/clickhouse.sh \
        --database "${database}" \
        --param_window_start="${window_start}" \
        --param_window_end="${window_end}" \
        --param_run_id="${run_id}" \
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
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'platform-ansible'
    AND startsWith(SpanName, 'openbao.tenancy.reconcile.')
    AND SpanAttributes['verself.proof_run_id'] = {run_id:String}
" "${org_count}" "${artifact_dir}/clickhouse/openbao-tenancy-spans-count.tsv"

python3 - "${run_id}" "${window_start}" "${window_end}" "${artifact_dir}" >"${artifact_dir}/run.json" <<'PY'
import json
import sys

run_id, window_start, window_end, artifact_dir = sys.argv[1:5]
print(json.dumps({
    "run_id": run_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
}, indent=2, sort_keys=True))
PY

echo "openbao tenancy proof ok: ${artifact_dir}"
