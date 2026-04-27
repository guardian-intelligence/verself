#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-workload-identity-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/workload-identity-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

if [[ "${WORKLOAD_IDENTITY_PROOF_EXERCISE_SECRETS:-1}" != "0" ]]; then
  VERIFICATION_RUN_ID="${run_id}-secrets" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_dir}/dependencies/secrets-proof" \
    "${script_dir}/verify-secrets-live.sh"
fi

if [[ "${WORKLOAD_IDENTITY_PROOF_EXERCISE_GRAFANA:-1}" != "0" ]]; then
  VERIFICATION_RUN_ID="${run_id}-grafana" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_dir}/dependencies/grafana-proof" \
    "${script_dir}/verify-grafana-live.sh"
fi

if [[ "${WORKLOAD_IDENTITY_PROOF_EXERCISE_TEMPORAL:-1}" != "0" ]]; then
  VERIFICATION_RUN_ID="${run_id}-temporal" \
  VERIFICATION_ARTIFACT_ROOT="${artifact_dir}/dependencies/temporal-verify" \
    "${script_dir}/verify-temporal-live.sh"
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
    f"spiffe://{trust_domain}/svc/nats",
    f"spiffe://{trust_domain}/svc/grafana",
    f"spiffe://{trust_domain}/svc/otelcol",
    f"spiffe://{trust_domain}/svc/clickhouse-server",
    f"spiffe://{trust_domain}/svc/clickhouse-operator",
    f"spiffe://{trust_domain}/svc/temporal-server",
]
systemd_units = [
    "spire-server",
    "spire-agent",
    "clickhouse-server-spiffe-helper",
    "clickhouse-operator-spiffe-helper",
    "otelcol-clickhouse-spiffe-helper",
    "otelcol",
    "nats-spiffe-helper",
    "nats",
    "identity-service",
    "governance-service",
    "billing-service",
    "sandbox-rental-service",
    "secrets-service",
    "mailbox-service",
    "grafana-clickhouse-spiffe-helper",
    "grafana",
    "temporal-server",
    "stalwart",
]
for unit in systemd_units:
    state = subprocess.check_output(["systemctl", "is-active", unit], text=True).strip()
    if state != "active":
        raise SystemExit(f"{unit} is {state}, expected active")

stale_credentials = [
    "/etc/credstore/spire/join-token",
    "/run/spire-agent/private/join-token",
    "/etc/credstore/billing/pg-dsn",
    "/etc/credstore/billing/stripe-secret-key",
    "/etc/credstore/billing/stripe-webhook-secret",
    "/etc/credstore/billing/ch-password",
    "/etc/credstore/identity-service/pg-dsn",
    "/etc/credstore/governance-service/pg-dsn",
    "/etc/credstore/governance-service/identity-pg-dsn",
    "/etc/credstore/governance-service/billing-pg-dsn",
    "/etc/credstore/governance-service/sandbox-pg-dsn",
    "/etc/credstore/governance-service/ch-password",
    "/etc/credstore/sandbox-rental/pg-dsn",
    "/etc/credstore/sandbox-rental/ch-password",
    "/etc/credstore/sandbox-rental/github-app-private-key",
    "/etc/credstore/sandbox-rental/github-app-webhook-secret",
    "/etc/credstore/sandbox-rental/github-app-client-secret",
    "/etc/credstore/mailbox-service/pg-dsn",
    "/etc/credstore/mailbox-service/resend-api-key",
    "/etc/credstore/grafana/clickhouse-password",
    "/etc/credstore/mailbox-service/stalwart-admin-password",
    "/etc/credstore/stalwart/pg-password",
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
    "pg-password",
    "stripe-secret-key",
    "stripe-webhook-secret",
    "ch-password",
    "github-app-private-key",
    "github-app-webhook-secret",
    "github-app-client-secret",
    "resend-api-key",
    "stalwart-admin-password",
]
load_credentials = {}
unit_texts = {}
# systemd 256+ renders LoadCredential= via `systemctl show --value` as the
# opaque sentinel "[unprintable]", so the raw `systemctl cat` output is the
# only reliable surface for the stale-term scan. We concatenate every
# LoadCredential= line (there are typically several per unit).
for unit in ["identity-service", "governance-service", "billing-service", "sandbox-rental-service", "secrets-service", "mailbox-service", "stalwart"]:
    unit_text = subprocess.check_output(["systemctl", "cat", unit], text=True)
    unit_texts[unit] = unit_text
    load_credential_lines = [l.strip() for l in unit_text.splitlines() if l.strip().startswith("LoadCredential=")]
    value = "\n".join(load_credential_lines)
    load_credentials[unit] = value
    stale_terms = [term for term in stale_loadcredential_terms if term in value]
    if stale_terms:
        raise SystemExit(f"{unit} still loads stale credentials: {', '.join(stale_terms)}")

stale_environment_terms = [
    "_OPENBAO_ADDR=",
    "_OPENBAO_PLATFORM_MOUNT=",
    "_OPENBAO_SPIFFE_JWT_MOUNT=",
    "_OPENBAO_WORKLOAD_AUDIENCE=",
    "_OPENBAO_ROLE=",
]
for unit in ["billing-service", "sandbox-rental-service", "mailbox-service"]:
    env_lines = [l.strip() for l in unit_texts[unit].splitlines() if l.strip().startswith("Environment=")]
    stale_terms = [term for term in stale_environment_terms if any(term in line for line in env_lines)]
    if stale_terms:
        raise SystemExit(f"{unit} still carries legacy OpenBao env wiring: {', '.join(stale_terms)}")

for unit in [
    "clickhouse-server-spiffe-helper",
    "clickhouse-operator-spiffe-helper",
    "otelcol-clickhouse-spiffe-helper",
    "otelcol",
    "nats-spiffe-helper",
    "nats",
    "governance-service",
    "billing-service",
    "sandbox-rental-service",
    "mailbox-service",
    "grafana-clickhouse-spiffe-helper",
    "grafana",
    "temporal-server",
    "stalwart",
]:
    subprocess.run(["systemctl", "restart", unit], check=True)
    for _ in range(30):
        state = subprocess.run(["systemctl", "is-active", "--quiet", unit], check=False)
        if state.returncode == 0:
            break
        subprocess.run(["sleep", "1"], check=True)
    else:
        raise SystemExit(f"{unit} did not become active after restart")

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
    ("temporal_server", "temporal", "temporal"),
    ("temporal_server", "temporal", "temporal_visibility"),
    ("stalwart", "stalwart", "stalwart"),
]:
    dsn = f"postgres://{db_user}@/{database}?host=/var/run/postgresql&sslmode=disable"
    current_user = subprocess.check_output([
        "sudo",
        "-u",
        system_user,
        "/opt/verself/profile/bin/psql",
        dsn,
        "-Atc",
        "select current_user",
    ], text=True).strip()
    if current_user != db_user:
        raise SystemExit(f"peer auth as {system_user}->{db_user} returned {current_user}")
    peer_auth_checks.append({"system_user": system_user, "db_user": db_user, "database": database})

entries = subprocess.check_output([
    "/opt/verself/profile/bin/spire-server",
    "entry",
    "show",
    "-socketPath",
    "/run/spire-server/private/api.sock",
], text=True)
missing_ids = [spiffe_id for spiffe_id in expected_ids if spiffe_id not in entries]
if missing_ids:
    raise SystemExit("missing SPIRE registration entries: " + ", ".join(missing_ids))

# Rendered Stalwart config must not contain a PostgreSQL password surface.
# With peer auth the toml has no password key at all; a residual `password =`
# would mean a stale template or manual edit re-introduced the credential.
stalwart_config_path = "/etc/stalwart/config.toml"
with open(stalwart_config_path, "r", encoding="utf-8") as f:
    stalwart_config_text = f.read()
stalwart_pg_block = []
in_pg_block = False
for raw_line in stalwart_config_text.splitlines():
    line = raw_line.strip()
    if line.startswith("[store."):
        in_pg_block = line == '[store."postgresql"]'
        continue
    if line.startswith("["):
        in_pg_block = False
        continue
    if in_pg_block and line:
        stalwart_pg_block.append(line)
pg_block_password_keys = [l for l in stalwart_pg_block if l.startswith("password") and "=" in l]
if pg_block_password_keys:
    raise SystemExit(f"stalwart {stalwart_config_path} [store.\"postgresql\"] still defines: {pg_block_password_keys}")
pg_block_host = [l for l in stalwart_pg_block if l.startswith("host") and "=" in l]
if not any('"/var/run/postgresql"' in l for l in pg_block_host):
    raise SystemExit(f"stalwart {stalwart_config_path} [store.\"postgresql\"] host is not a Unix socket: {pg_block_host}")

# The `stalwart` PG role must not carry a password — `pg_hba.conf` still has a
# SCRAM rule for 127.0.0.1, so a leftover password would let an attacker on the
# loopback bypass peer auth.
stalwart_rolpassword = subprocess.check_output([
    "sudo",
    "-u",
    "postgres",
    "/opt/verself/profile/bin/psql",
    "-Atc",
    "select coalesce(rolpassword, '') from pg_authid where rolname='stalwart'",
], text=True).strip()
if stalwart_rolpassword:
    raise SystemExit("stalwart PG role has a non-null password; peer-auth-only invariant broken")

# Live kernel check: the running stalwart process must not hold any TCP
# connection to PostgreSQL. This catches the case where the config file is
# correct but a build regression opens a TCP socket anyway.
stalwart_pids = subprocess.run(
    ["pgrep", "-x", "stalwart"],
    check=False,
    capture_output=True,
    text=True,
).stdout.split()
if not stalwart_pids:
    raise SystemExit("no running stalwart process found for TCP audit")
ss_out = subprocess.check_output(["ss", "-tnHp"], text=True)
stalwart_tcp_pg = []
for line in ss_out.splitlines():
    # `ss -tnp` prints the peer address as the 4th space-separated field, e.g.
    # "ESTAB 0 0 127.0.0.1:55124 127.0.0.1:5432 users:((\"stalwart\",pid=12345,fd=9))"
    # Filter on any line that mentions a stalwart pid AND a :5432 endpoint.
    if ":5432" not in line:
        continue
    if any(f"pid={pid}" in line for pid in stalwart_pids):
        stalwart_tcp_pg.append(line.strip())
if stalwart_tcp_pg:
    raise SystemExit("stalwart holds TCP connections to PostgreSQL: " + "; ".join(stalwart_tcp_pg))

bundle_endpoint_url = "https://127.0.0.1:8082"
bundle_context = ssl.create_default_context(cafile="/etc/spire/bundle-endpoint/ca.pem")
# SPIRE's built-in bundle endpoint serves the JWKS-compatible SPIFFE bundle at
# `/`; there is no discovery document or secondary `/keys` path in this topology.
bundle_request = urllib.request.Request(bundle_endpoint_url)
bundle = json.loads(
    urllib.request.urlopen(bundle_request, context=bundle_context, timeout=5).read().decode()
)
if not bundle.get("keys"):
    raise SystemExit("SPIRE bundle endpoint returned no keys")

openbao_root_token = open("/etc/credstore/openbao/root-token", "r", encoding="utf-8").read().strip()
openbao_context = ssl._create_unverified_context()
openbao_config_request = urllib.request.Request(
    "https://127.0.0.1:8200/v1/auth/spiffe-jwt/config",
    headers={"X-Vault-Token": openbao_root_token},
)
openbao_config = json.loads(
    urllib.request.urlopen(openbao_config_request, context=openbao_context, timeout=5).read().decode()
)
openbao_auth_config = openbao_config.get("data") or {}
if openbao_auth_config.get("jwks_url") != bundle_endpoint_url:
    raise SystemExit(
        f"unexpected OpenBao JWKS URL: {openbao_auth_config.get('jwks_url')}"
    )
if openbao_auth_config.get("bound_issuer") != bundle_endpoint_url:
    raise SystemExit(
        f"unexpected OpenBao bound_issuer: {openbao_auth_config.get('bound_issuer')}"
    )
if openbao_auth_config.get("oidc_discovery_url"):
    raise SystemExit(
        "OpenBao SPIFFE auth still advertises oidc_discovery_url="
        + openbao_auth_config.get("oidc_discovery_url", "")
    )
if not openbao_auth_config.get("jwks_ca_pem"):
    raise SystemExit("OpenBao SPIFFE auth jwks_ca_pem is empty")

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
    "stalwart_postgres_store": stalwart_pg_block,
    "stalwart_rolpassword_empty": stalwart_rolpassword == "",
    "stalwart_tcp_pg_connections": stalwart_tcp_pg,
    "bundle_endpoint": {
        "url": bundle_endpoint_url,
        "issuer": bundle_endpoint_url,
        "key_count": len(bundle.get("keys") or []),
        "refresh_hint": bundle.get("spiffe_refresh_hint", 0),
        "sequence": bundle.get("spiffe_sequence", 0),
    },
    "openbao_spiffe_auth": {
        "jwks_url": openbao_auth_config.get("jwks_url", ""),
        "bound_issuer": openbao_auth_config.get("bound_issuer", ""),
        "has_jwks_ca_pem": bool(openbao_auth_config.get("jwks_ca_pem")),
    },
}
print(json.dumps(payload, indent=2, sort_keys=True))
PY

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

# ClickHouse does not publish its listener ports through the system tables we
# query elsewhere, so we assert the live socket set directly from the host.
verification_ssh "sudo ss -ltnH '( sport = :8123 or sport = :8443 or sport = :9000 or sport = :9440 )'" \
  >"${artifact_dir}/clickhouse/clickhouse-secure-listeners.tsv"

if ! grep -q ':9440' "${artifact_dir}/clickhouse/clickhouse-secure-listeners.tsv"; then
  echo "ClickHouse is not listening on 9440" >&2
  exit 1
fi

if grep -Eq ':(8123|8443|9000)' "${artifact_dir}/clickhouse/clickhouse-secure-listeners.tsv"; then
  echo "ClickHouse still exposes stale listeners on 8123/8443/9000" >&2
  exit 1
fi

wait_for_clickhouse_count() {
  local database="$1"
  local query="$2"
  local min_count="$3"
  local output_path="$4"
  shift 4
  local extra_args=("$@")
  local count="0"
  for _ in $(seq 1 60); do
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

assert_clickhouse_zero() {
  local database="$1"
  local query="$2"
  local output_path="$3"
  shift 3
  local extra_args=("$@")
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database "${database}" \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      "${extra_args[@]}" \
      --query "${query}"
  ) >"${output_path}"
  local count
  count="$(tail -n 1 "${output_path}" | tr -d '[:space:]')"
  if [[ ! "${count}" =~ ^[0-9]+$ ]] || (( count != 0 )); then
    echo "ClickHouse zero assertion failed for ${output_path}: got ${count}, expected 0" >&2
    return 1
  fi
}

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND SpanName = 'auth.spiffe.mtls.client'
    AND ServiceName IN ('identity-service', 'sandbox-rental-service', 'secrets-service', 'temporal-bootstrap')
    AND SpanAttributes['spiffe.expected_server_id'] != ''
" 3 "${artifact_dir}/clickhouse/spiffe-mtls-client-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND SpanName = 'auth.spiffe.mtls.server'
    AND ServiceName IN ('billing-service', 'governance-service', 'secrets-service', 'temporal-server')
    AND SpanAttributes['spiffe.peer_id'] != ''
" 3 "${artifact_dir}/clickhouse/spiffe-mtls-server-spans-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName IN ('auth.spiffe.jwt_svid.fetch', 'secrets.bao.jwt_svid.login')
" 2 "${artifact_dir}/clickhouse/spiffe-jwt-svid-spans-count.tsv"

for service in billing-service sandbox-rental-service mailbox-service; do
  service_slug="${service//-/_}"
  wait_for_clickhouse_count default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
      AND ServiceName = {service:String}
      AND SpanName = 'secrets.runtime.resolve'
  " 1 "${artifact_dir}/clickhouse/${service_slug}-runtime-secret-resolve-count.tsv" --param_service="${service}"

  wait_for_clickhouse_count default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
      AND ServiceName = 'secrets-service'
      AND SpanName = 'secrets.platform.resolve'
      AND SpanAttributes['verself.runtime_secret_consumer'] = {service:String}
  " 1 "${artifact_dir}/clickhouse/${service_slug}-secrets-service-runtime-secret-resolve-count.tsv" --param_service="${service}"

  assert_clickhouse_zero default "
    SELECT count()
    FROM otel_traces
    WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
      AND ServiceName = {service:String}
      AND SpanName IN ('workload.openbao.jwt_svid.login', 'workload.openbao.kv.get')
  " "${artifact_dir}/clickhouse/${service_slug}-direct-openbao-span-count.tsv" --param_service="${service}"
done

wait_for_clickhouse_count default "
  SELECT count()
  FROM system.users
  WHERE name IN ('clickhouse_operator', 'otelcol', 'billing_service', 'governance_service', 'sandbox_rental', 'grafana_observer')
    AND has(auth_type, 'ssl_certificate')
" 6 "${artifact_dir}/clickhouse/clickhouse-cert-auth-users-count.tsv"

default_user_count="$(
  (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh --query "SELECT count() FROM system.users WHERE name = 'default' FORMAT TabSeparated"
  ) | tr -d '[:space:]'
)"
if [[ "${default_user_count}" != "0" ]]; then
  echo "ClickHouse default user still exists" >&2
  exit 1
fi

for clickhouse_user in clickhouse_operator otelcol billing_service governance_service sandbox_rental grafana_observer; do
  wait_for_clickhouse_count default "
    SELECT count()
    FROM system.query_log
    WHERE event_time BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
      AND type = 'QueryFinish'
      AND exception_code = 0
      AND initial_user = {clickhouse_user:String}
  " 1 "${artifact_dir}/clickhouse/${clickhouse_user}-query-log-count.tsv" --param_clickhouse_user="${clickhouse_user}"
done

wait_for_clickhouse_count verself "
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
