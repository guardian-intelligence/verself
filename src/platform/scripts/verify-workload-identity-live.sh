#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-workload-identity-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/workload-identity-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ "${WORKLOAD_IDENTITY_PROOF_EXERCISE_SECRETS:-1}" != "0" ]]; then
  VERIFICATION_RUN_ID="${run_id}-secrets" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_dir}/dependencies/secrets-proof" \
    "${script_dir}/verify-secrets-live.sh"
fi

verification_ssh "sudo python3 - $(printf '%q' "${VERIFICATION_DOMAIN}")" >"${artifact_dir}/workload-identity-state.json" <<'PY'
import json
import os
import grp
import socket
import ssl
import stat
import subprocess
import sys
import urllib.request

domain = sys.argv[1]
trust_domain = "spiffe." + domain
expected_ids = [
    f"spiffe://{trust_domain}/svc/identity-service",
    f"spiffe://{trust_domain}/svc/governance-service",
    f"spiffe://{trust_domain}/svc/billing-service",
    f"spiffe://{trust_domain}/svc/sandbox-rental-service",
    f"spiffe://{trust_domain}/svc/secrets-service",
    f"spiffe://{trust_domain}/svc/mailbox-service",
]
systemd_units = [
    "spire-server",
    "spire-agent",
    "spire-oidc-discovery-provider",
    "identity-service",
    "governance-service",
    "billing-service",
    "sandbox-rental-service",
    "secrets-service",
    "mailbox-service",
]
for unit in systemd_units:
    state = subprocess.check_output(["systemctl", "is-active", unit], text=True).strip()
    if state != "active":
        raise SystemExit(f"{unit} is {state}, expected active")

stale_credentials = [
    "/etc/credstore/spire/join-token",
    "/run/spire-agent/private/join-token",
    "/etc/credstore/billing/pg-dsn",
    "/etc/credstore/identity-service/pg-dsn",
    "/etc/credstore/governance-service/pg-dsn",
    "/etc/credstore/governance-service/identity-pg-dsn",
    "/etc/credstore/governance-service/billing-pg-dsn",
    "/etc/credstore/governance-service/sandbox-pg-dsn",
    "/etc/credstore/sandbox-rental/pg-dsn",
    "/etc/credstore/mailbox-service/pg-dsn",
    "/etc/credstore/governance-service/internal-audit-token",
    "/etc/credstore/sandbox-rental/billing-client-secret",
    "/etc/credstore/sandbox-rental/secret-injection-grant-ed25519.pem",
    "/etc/credstore/secrets-service/internal-injection-token",
    "/etc/credstore/secrets-service/service-account-client-secret",
    "/etc/credstore/secrets-service/service-account-user-id",
    "/etc/credstore/secrets-service/billing-client-secret",
    "/etc/credstore/secrets-service/sandbox-injection-grant-ed25519.pub.pem",
]
present_stale = [path for path in stale_credentials if os.path.exists(path)]
if present_stale:
    raise SystemExit("stale shared service credentials still exist: " + ", ".join(present_stale))

stale_loadcredential_terms = [
    "internal-audit-token",
    "billing-client-secret",
    "service-account-client-secret",
    "service-account-user-id",
    "internal-injection-token",
    "secret-injection-grant",
    "sandbox-injection-grant",
    "pg-dsn",
]
load_credentials = {}
for unit in ["identity-service", "governance-service", "billing-service", "sandbox-rental-service", "secrets-service"]:
    value = subprocess.check_output(["systemctl", "show", unit, "--property=LoadCredential", "--value"], text=True)
    load_credentials[unit] = value.strip()
    stale_terms = [term for term in stale_loadcredential_terms if term in value]
    if stale_terms:
        raise SystemExit(f"{unit} still loads stale credentials: {', '.join(stale_terms)}")

socket_path = "/run/spire-agent/sockets/agent.sock"
socket_stat = os.stat(socket_path)
socket_group = grp.getgrgid(socket_stat.st_gid).gr_name
if socket_group != "spire_workload":
    raise SystemExit(f"{socket_path} group is {socket_group}, expected spire_workload")
if not stat.S_ISSOCK(socket_stat.st_mode):
    raise SystemExit(f"{socket_path} is not a unix socket")

peer_auth_checks = []
for system_user, db_user, database in [
    ("billing", "billing", "billing"),
    ("identity_service", "identity_service", "identity_service"),
    ("governance_service", "governance_service", "governance_service"),
    ("governance_service", "identity_service", "identity_service"),
    ("governance_service", "billing", "billing"),
    ("governance_service", "sandbox_rental", "sandbox_rental"),
    ("sandbox_rental", "sandbox_rental", "sandbox_rental"),
    ("mailbox_service", "mailbox_service", "mailbox_service"),
]:
    dsn = f"postgres://{db_user}@/{database}?host=/var/run/postgresql&sslmode=disable"
    current_user = subprocess.check_output([
        "sudo",
        "-u",
        system_user,
        "/opt/forge-metal/profile/bin/psql",
        dsn,
        "-Atc",
        "select current_user",
    ], text=True).strip()
    if current_user != db_user:
        raise SystemExit(f"peer auth as {system_user}->{db_user} returned {current_user}")
    peer_auth_checks.append({"system_user": system_user, "db_user": db_user, "database": database})

entries = subprocess.check_output([
    "/opt/forge-metal/profile/bin/spire-server",
    "entry",
    "show",
    "-socketPath",
    "/run/spire-server/private/api.sock",
], text=True)
missing_ids = [spiffe_id for spiffe_id in expected_ids if spiffe_id not in entries]
if missing_ids:
    raise SystemExit("missing SPIRE registration entries: " + ", ".join(missing_ids))

oidc_issuer = "https://127.0.0.1:8082"
oidc_context = ssl.create_default_context(cafile="/etc/spire/oidc/ca.pem")
oidc_request = urllib.request.Request(oidc_issuer + "/.well-known/openid-configuration")
oidc = json.loads(urllib.request.urlopen(oidc_request, context=oidc_context, timeout=5).read().decode())
if oidc.get("issuer") != oidc_issuer:
    raise SystemExit(f"unexpected SPIRE OIDC issuer: {oidc.get('issuer')}")
jwks_request = urllib.request.Request(oidc_issuer + "/keys")
jwks = json.loads(urllib.request.urlopen(jwks_request, context=oidc_context, timeout=5).read().decode())
if not jwks.get("keys"):
    raise SystemExit("SPIRE OIDC JWKS endpoint returned no keys")

payload = {
    "domain": domain,
    "trust_domain": trust_domain,
    "expected_ids": expected_ids,
    "active_units": systemd_units,
    "workload_socket": {
        "path": socket_path,
        "mode": oct(socket_stat.st_mode & 0o777),
        "group": socket_group,
    },
    "postgres_peer_auth": peer_auth_checks,
    "load_credentials": load_credentials,
    "oidc": {
        "issuer": oidc.get("issuer", ""),
        "jwks_uri": oidc.get("jwks_uri", ""),
        "key_count": len(jwks.get("keys") or []),
    },
}
print(json.dumps(payload, indent=2, sort_keys=True))
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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
    AND SpanName = 'auth.spiffe.mtls.client'
    AND ServiceName IN ('identity-service', 'sandbox-rental-service', 'secrets-service')
    AND SpanAttributes['spiffe.expected_server_id'] != ''
" 3 "${artifact_dir}/clickhouse/spiffe-mtls-client-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND SpanName = 'auth.spiffe.mtls.server'
    AND ServiceName IN ('billing-service', 'governance-service', 'secrets-service')
    AND SpanAttributes['spiffe.peer_id'] != ''
" 3 "${artifact_dir}/clickhouse/spiffe-mtls-server-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName IN ('auth.spiffe.jwt_svid.fetch', 'secrets.bao.jwt_svid.login')
" 2 "${artifact_dir}/clickhouse/spiffe-jwt-svid-spans-count.tsv"

wait_for_clickhouse_count forge_metal "
  SELECT count()
  FROM audit_events
  WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND actor_spiffe_id != ''
    AND service_name IN ('identity-service', 'sandbox-rental-service', 'secrets-service')
" 1 "${artifact_dir}/clickhouse/spiffe-audit-actor-count.tsv"

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

echo "workload identity proof ok: ${artifact_dir}"
