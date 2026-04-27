#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-secrets-leak-proof-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_PROOF_ARTIFACT_ROOT}/secrets-leak-proof}"
artifact_dir="${artifact_root}/${run_id}"
mkdir -p "${artifact_dir}/clickhouse" "${artifact_dir}/responses"

findings_path="${artifact_dir}/leak-findings.jsonl"
: >"${findings_path}"

# shellcheck disable=SC1090
source <("${script_dir}/assume-persona.sh" acme-admin --print)

sentinel="$(
  RUN_ID="${run_id}" python3 - <<'PY'
import base64
import json
import os
import secrets

def segment(payload):
    raw = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode()
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")

print(".".join([
    segment({"alg": "none", "typ": "JWT"}),
    segment({"sub": "verself-leak-proof", "run": os.environ["RUN_ID"], "nonce": secrets.token_hex(12)}),
    base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("="),
]))
PY
)"

patterns_json="$(
  printf '%s\n' "${SECRETS_SERVICE_ACCESS_TOKEN}" | python3 -c 'import json, sys; print(json.dumps({"real_token": sys.stdin.readline().rstrip("\n"), "sentinel": sys.argv[1]}))' "${sentinel}"
)"

scan_text() {
  local surface="$1"
  python3 - "${surface}" "${findings_path}" 3<<<"${patterns_json}" <<'PY'
import json
import os
import re
import sys

surface, findings_path = sys.argv[1:3]
patterns = json.load(os.fdopen(3))
text = sys.stdin.read()
jwt_pattern = re.compile(r"\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b")
checks = [
    ("real-token", patterns["real_token"]),
    ("sentinel-token", patterns["sentinel"]),
]
findings = []
for kind, needle in checks:
    count = text.count(needle) if needle else 0
    if count:
        findings.append({"surface": surface, "kind": kind, "count": count})
auth_count = len(re.findall(r"(?i)authorization:\s*bearer", text))
if auth_count:
    findings.append({"surface": surface, "kind": "authorization-bearer", "count": auth_count})
jwt_count = len(jwt_pattern.findall(text))
if jwt_count:
    findings.append({"surface": surface, "kind": "jwt-shaped-value", "count": jwt_count})
if findings:
    with open(findings_path, "a", encoding="utf-8") as handle:
        for finding in findings:
            handle.write(json.dumps(finding, sort_keys=True) + "\n")
    sys.exit(1)
PY
}

remote_secrets_api() {
  local method="$1"
  local path="$2"
  local token="$3"
  local body_path="$4"
  local output_path="$5"
  local expected_statuses="$6"
  local idempotency_key="${7:-}"
  local request_b64
  request_b64="$(
    printf '%s\n' "${token}" | python3 -c '
import base64
import json
import pathlib
import sys

method, path, body_path, expected_statuses, idempotency_key = sys.argv[1:6]
token = sys.stdin.readline().rstrip("\n")
body_b64 = ""
if body_path:
    body_b64 = base64.b64encode(pathlib.Path(body_path).read_bytes()).decode()
print(base64.b64encode(json.dumps({
    "method": method,
    "path": path,
    "token": token,
    "body_b64": body_b64,
    "expected_statuses": expected_statuses,
    "idempotency_key": idempotency_key,
}).encode()).decode())
' "${method}" "${path}" "${body_path}" "${expected_statuses}" "${idempotency_key}"
  )"

  printf '%s\n' "${request_b64}" | verification_ssh "python3 -c '
import base64
import http.client
import json
import sys

payload = json.loads(base64.b64decode(sys.stdin.readline()).decode())
body = base64.b64decode(payload.get(\"body_b64\") or \"\")
expected = {int(item) for item in payload[\"expected_statuses\"].split(\",\") if item}
headers = {\"Authorization\": \"Bearer \" + payload[\"token\"], \"Content-Type\": \"application/json\"}
if payload.get(\"idempotency_key\"):
    headers[\"Idempotency-Key\"] = payload[\"idempotency_key\"]
conn = http.client.HTTPConnection(\"127.0.0.1\", 4251, timeout=5)
conn.request(payload[\"method\"], payload[\"path\"], body=body if body else None, headers=headers)
response = conn.getresponse()
data = response.read()
sys.stdout.buffer.write(data)
if response.status not in expected:
    sys.stderr.write(f\"unexpected secrets-service status {response.status}; expected {sorted(expected)}\\n\")
    sys.exit(1)
'" >"${output_path}"
}

public_caddy_rejected_request() {
  local token="$1"
  local output_path="$2"
  local url="${BASE_URL:-https://sandbox.api.${VERIFICATION_DOMAIN}}"
  url="${url%/}/api/v1/executions/00000000-0000-0000-0000-000000000000"
  printf '%s\n' "${token}" | python3 -c '
import json
import sys
import urllib.error
import urllib.request

url = sys.argv[1]
token = sys.stdin.readline().rstrip("\n")
request = urllib.request.Request(url, headers={"Authorization": "Bearer " + token, "User-Agent": "verself-secrets-leak-proof"})
try:
    with urllib.request.urlopen(request, timeout=5) as response:
        status = response.status
        body = response.read(4096).decode("utf-8", errors="replace")
except urllib.error.HTTPError as error:
    status = error.code
    body = error.read(4096).decode("utf-8", errors="replace")
if status < 400 or status >= 500:
    raise SystemExit(f"unexpected public rejection status {status}")
print(json.dumps({"status": status, "body_bytes": len(body)}, sort_keys=True))
' "${url}" >"${output_path}"
}

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

emit_scan_span() {
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
  export VERSELF_DEPLOY_KIND="secrets-leak-proof"
  # shellcheck source=src/platform/scripts/deploy_identity.sh
  source "${script_dir}/deploy_identity.sh"
  (
    cd "${VERIFICATION_REPO_ROOT}"
    PROOF_SPAN_SERVICE="proof-runner" \
    PROOF_SPAN_NAME="secrets.leak_proof.scan" \
    PROOF_SPAN_ATTRS_JSON="$(python3 -c 'import json, sys; print(json.dumps({"verself.proof_run_id": sys.argv[1], "leak.findings": 0}))' "${run_id}")" \
      go run ./src/otel/cmd/proof-span
  )
}

scan_clickhouse() {
  local failed=0
  if ! (
    cd "${VERIFICATION_PLATFORM_ROOT}"
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
          toString(LogAttributes) AS log_attributes,
          toString(ResourceAttributes) AS resource_attributes
        FROM otel_logs
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
          AND ServiceName IN ('caddy', 'secrets-service', 'sandbox-rental-service', 'governance-service', 'otelcol')
        ORDER BY Timestamp
        FORMAT JSONEachRow
      "
  ) | scan_text "default.otel_logs"; then
    failed=1
  fi
  if ! (
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
          toString(SpanAttributes) AS span_attributes,
          toString(ResourceAttributes) AS resource_attributes
        FROM otel_traces
        WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
          AND ServiceName IN ('secrets-service', 'sandbox-rental-service', 'governance-service', 'proof-runner')
        ORDER BY Timestamp
        FORMAT JSONEachRow
      "
  ) | scan_text "default.otel_traces"; then
    failed=1
  fi
  if ! (
    cd "${VERIFICATION_PLATFORM_ROOT}"
    ./scripts/clickhouse.sh \
      --database verself \
      --param_window_start="${window_start}" \
      --param_window_end="${window_end}" \
      --query "
        SELECT *
        FROM audit_events
        WHERE recorded_at BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
          AND (service_name IN ('secrets-service', 'sandbox-rental-service', 'governance-service') OR startsWith(audit_event, 'secrets.'))
        ORDER BY recorded_at, sequence
        FORMAT JSONEachRow
      "
  ) | scan_text "verself.audit_events"; then
    failed=1
  fi
  return "${failed}"
}

scan_remote_logs() {
  local failed=0
  if ! printf '%s\n' "${patterns_json}" | verification_ssh "sudo python3 -c '
import datetime as dt
import json
import pathlib
import re
import subprocess
import sys

patterns = json.loads(sys.stdin.readline())
start = dt.datetime.fromisoformat(\"${window_start}\".replace(\"Z\", \"+00:00\"))
end = dt.datetime.fromisoformat(\"${window_end}\".replace(\"Z\", \"+00:00\")) + dt.timedelta(seconds=60)
jwt_pattern = re.compile(r\"\\b[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]{8,}\\b\")

def scan(surface, text):
    findings = []
    for kind, needle in ((\"real-token\", patterns[\"real_token\"]), (\"sentinel-token\", patterns[\"sentinel\"])):
        count = text.count(needle) if needle else 0
        if count:
            findings.append({\"surface\": surface, \"kind\": kind, \"count\": count})
    auth_count = len(re.findall(r\"(?i)authorization:\\s*bearer\", text))
    if auth_count:
        findings.append({\"surface\": surface, \"kind\": \"authorization-bearer\", \"count\": auth_count})
    jwt_count = len(jwt_pattern.findall(text))
    if jwt_count:
        findings.append({\"surface\": surface, \"kind\": \"jwt-shaped-value\", \"count\": jwt_count})
    return findings

all_findings = []
caddy_path = pathlib.Path(\"/var/log/caddy/access.log\")
if caddy_path.exists():
    selected = []
    for line in caddy_path.read_text(encoding=\"utf-8\", errors=\"replace\").splitlines():
        try:
            timestamp = float(json.loads(line).get(\"ts\", 0))
        except Exception:
            continue
        if start.timestamp() - 5 <= timestamp <= end.timestamp():
            selected.append(line)
    all_findings.extend(scan(\"/var/log/caddy/access.log\", \"\\n\".join(selected)))

journal = subprocess.run([
    \"journalctl\", \"--no-pager\", \"-o\", \"cat\",
    \"--since\", start.isoformat(), \"--until\", end.isoformat(),
    \"-u\", \"caddy\", \"-u\", \"secrets-service\", \"-u\", \"sandbox-rental-service\",
    \"-u\", \"governance-service\", \"-u\", \"otelcol\",
], check=False, text=True, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
all_findings.extend(scan(\"journalctl\", journal.stdout))

for finding in all_findings:
    print(json.dumps(finding, sort_keys=True))
sys.exit(1 if all_findings else 0)
'" >"${artifact_dir}/remote-log-scan.jsonl"; then
    failed=1
    cat "${artifact_dir}/remote-log-scan.jsonl" >>"${findings_path}"
  fi
  return "${failed}"
}

scan_artifacts() {
  python3 - "${artifact_dir}" "${findings_path}" 3<<<"${patterns_json}" <<'PY'
import json
import os
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1]).resolve()
findings_path = pathlib.Path(sys.argv[2]).resolve()
patterns = json.load(os.fdopen(3))
jwt_pattern = re.compile(r"\b[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b")
findings = []
for path in root.rglob("*"):
    if not path.is_file() or path.resolve() == findings_path:
        continue
    text = path.read_text(encoding="utf-8", errors="replace")
    surface = str(path.relative_to(root))
    for kind, needle in (("real-token", patterns["real_token"]), ("sentinel-token", patterns["sentinel"])):
        count = text.count(needle) if needle else 0
        if count:
            findings.append({"surface": "artifact:" + surface, "kind": kind, "count": count})
    auth_count = len(re.findall(r"(?i)authorization:\s*bearer", text))
    if auth_count:
        findings.append({"surface": "artifact:" + surface, "kind": "authorization-bearer", "count": auth_count})
    jwt_count = len(jwt_pattern.findall(text))
    if jwt_count:
        findings.append({"surface": "artifact:" + surface, "kind": "jwt-shaped-value", "count": jwt_count})
if findings:
    with findings_path.open("a", encoding="utf-8") as handle:
        for finding in findings:
            handle.write(json.dumps(finding, sort_keys=True) + "\n")
    sys.exit(1)
PY
}

secret_name="leak-proof-${run_id}"
window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

put_body="${artifact_dir}/payload-secret-put.json"
python3 - "${run_id}" >"${put_body}" <<'PY'
import json
import sys

print(json.dumps({"value": "leak-proof-value-" + sys.argv[1]}))
PY

remote_secrets_api PUT "/api/v1/secrets/${secret_name}" "${SECRETS_SERVICE_ACCESS_TOKEN}" "${put_body}" "${artifact_dir}/responses/accepted-secret-put.json" "200,201" "${run_id}-put"
remote_secrets_api GET "/api/v1/secrets/${secret_name}" "${sentinel}" "" "${artifact_dir}/responses/rejected-secrets-sentinel.json" "401,403"
public_caddy_rejected_request "${sentinel}" "${artifact_dir}/responses/rejected-public-caddy.json"

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 60 SECOND
    AND ServiceName = 'secrets-service'
    AND SpanName = 'secrets.secret.put'
" 1 "${artifact_dir}/clickhouse/secrets-put-span-count.tsv"

failed=0
scan_clickhouse || failed=1
scan_remote_logs || failed=1
scan_artifacts || failed=1

if (( failed != 0 )); then
  echo "secrets leak proof failed; sanitized findings are in ${findings_path}" >&2
  exit 1
fi

emit_scan_span
span_window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort('${span_window_end}') + INTERVAL 60 SECOND
    AND ServiceName = 'proof-runner'
    AND SpanName = 'secrets.leak_proof.scan'
    AND SpanAttributes['verself.proof_run_id'] = {run_id:String}
" 1 "${artifact_dir}/clickhouse/leak-proof-span-count.tsv"

python3 - "${run_id}" "${window_start}" "${span_window_end}" "${artifact_dir}" >"${artifact_dir}/run.json" <<'PY'
import json
import sys

run_id, window_start, window_end, artifact_dir = sys.argv[1:5]
print(json.dumps({
    "run_id": run_id,
    "window_start": window_start,
    "window_end": window_end,
    "artifact_dir": artifact_dir,
    "checked_surfaces": [
        "default.otel_logs",
        "default.otel_traces",
        "verself.audit_events",
        "/var/log/caddy/access.log",
        "journalctl",
        "proof artifacts",
    ],
}, indent=2, sort_keys=True))
PY

echo "secrets leak proof ok: ${artifact_dir}"
