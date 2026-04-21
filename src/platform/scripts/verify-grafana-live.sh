#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
# shellcheck disable=SC1091
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

kind="${VERIFICATION_KIND:-grafana-live}"
run_id="${VERIFICATION_RUN_ID:-${kind}-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_REPO_ROOT}/artifacts/${kind}}"
artifact_dir="${artifact_root}/${run_id}"
dashboard_url="${GRAFANA_BASE_URL:-https://dashboard.${VERIFICATION_DOMAIN}}"
marker="fm:grafana verify=${run_id}"
dashboard_definitions_path="${VERIFICATION_PLATFORM_ROOT}/ansible/roles/grafana/vars/main.yml"

if [[ ! -f "${dashboard_definitions_path}" ]]; then
  echo "Grafana dashboard definitions not found: ${dashboard_definitions_path}" >&2
  exit 1
fi

expected_dashboard_count="$(awk '/^  - uid: / { c += 1 } END { print c + 0 }' "${dashboard_definitions_path}")"

if [[ "${expected_dashboard_count}" -le 0 ]]; then
  echo "Failed to resolve expected Grafana dashboard count from ${dashboard_definitions_path}" >&2
  exit 1
fi

mkdir -p "${artifact_dir}"
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

ch_query() {
  (cd "${VERIFICATION_PLATFORM_ROOT}" && ./scripts/clickhouse.sh --query "$1")
}

remote_psql() {
  local db="$1"
  local sql="$2"
  verification_ssh "sudo -u postgres psql -d ${db} -X -A -t -F \$'\\t' -P footer=off -c \"$sql\""
}

grafana_api_get() {
  local path="$1"
  verification_ssh "curl -fsS -u admin:'${grafana_admin_password}' 'http://127.0.0.1:4300${path}'"
}

grafana_api_post() {
  local path="$1"
  verification_ssh "curl -fsS -u admin:'${grafana_admin_password}' -H 'Content-Type: application/json' --data-binary @- 'http://127.0.0.1:4300${path}'"
}

verification_wait_for_http "Grafana public health" "${dashboard_url}/api/health" "200"

grafana_admin_password="$(verification_remote_sudo_cat /etc/credstore/grafana/admin-password)"
grafana_oidc_client_id="$(verification_remote_sudo_cat /etc/credstore/grafana/oidc-client-id | tr -d '\n')"

grafana_api_get "/api/health" >"${artifact_dir}/grafana-health.json"

curl -k -sS -o /dev/null \
  -w '%{http_code}\t%{redirect_url}\n' \
  "${dashboard_url}/login/generic_oauth" >"${artifact_dir}/grafana-oauth-redirect.tsv"

GRAFANA_OAUTH_CLIENT_ID="${grafana_oidc_client_id}" \
GRAFANA_OAUTH_DOMAIN="${VERIFICATION_DOMAIN}" \
GRAFANA_EXPECTED_REDIRECT_URI="${dashboard_url}/login/generic_oauth" \
GRAFANA_OAUTH_REDIRECT_TSV="${artifact_dir}/grafana-oauth-redirect.tsv" \
python3 - <<'PY'
import os
from pathlib import Path
from urllib.parse import parse_qs, urlparse

status, redirect_url = Path(os.environ["GRAFANA_OAUTH_REDIRECT_TSV"]).read_text().strip().split("\t", 1)
if status not in {"302", "303", "307"}:
    raise SystemExit(f"unexpected Grafana OAuth redirect status: {status}")

parsed = urlparse(redirect_url)
domain = os.environ["GRAFANA_OAUTH_DOMAIN"]
if parsed.scheme != "https" or parsed.netloc != f"auth.{domain}" or parsed.path != "/oauth/v2/authorize":
    raise SystemExit(f"unexpected OAuth authorize redirect: {redirect_url}")

query = parse_qs(parsed.query)
if query.get("client_id", [""])[0] != os.environ["GRAFANA_OAUTH_CLIENT_ID"]:
    raise SystemExit("Grafana OAuth redirect used an unexpected Zitadel client_id")

if query.get("redirect_uri", [""])[0] != os.environ["GRAFANA_EXPECTED_REDIRECT_URI"]:
    raise SystemExit("Grafana OAuth redirect used an unexpected redirect_uri")
PY

oauth_authorize_url="$(cut -f2- "${artifact_dir}/grafana-oauth-redirect.tsv")"
curl -k -sS -o /dev/null \
  -w '%{http_code}\t%{redirect_url}\n' \
  "${oauth_authorize_url}" >"${artifact_dir}/grafana-oauth-authorize.tsv"

GRAFANA_OAUTH_DOMAIN="${VERIFICATION_DOMAIN}" \
GRAFANA_OAUTH_AUTHORIZE_TSV="${artifact_dir}/grafana-oauth-authorize.tsv" \
python3 - <<'PY'
import os
from pathlib import Path
from urllib.parse import urlparse

status, redirect_url = Path(os.environ["GRAFANA_OAUTH_AUTHORIZE_TSV"]).read_text().strip().split("\t", 1)
if status not in {"302", "303", "307"}:
    raise SystemExit(f"unexpected Zitadel authorize status: {status}")

parsed = urlparse(redirect_url)
domain = os.environ["GRAFANA_OAUTH_DOMAIN"]
if parsed.scheme != "https" or parsed.netloc != f"auth.{domain}" or not parsed.path.startswith("/ui/login/"):
    raise SystemExit(f"unexpected Zitadel login redirect: {redirect_url}")
PY

remote_psql postgres "
SELECT datname
FROM pg_database
WHERE datname = 'grafana';
" >"${artifact_dir}/postgres-grafana-database.tsv"

remote_psql postgres "
SELECT rolname
FROM pg_roles
WHERE rolname = 'grafana';
" >"${artifact_dir}/postgres-grafana-role.tsv"

grafana_dashboard_storage="$(remote_psql grafana "
SELECT CASE
  WHEN to_regclass('public.resource') IS NOT NULL THEN 'resource'
  WHEN to_regclass('public.dashboard') IS NOT NULL THEN 'dashboard'
  ELSE 'missing'
END;
")"
grafana_dashboard_storage="$(echo "${grafana_dashboard_storage}" | tr -d '\r\n')"

case "${grafana_dashboard_storage}" in
  resource)
    remote_psql grafana "
    SELECT name, value::jsonb #>> '{spec,title}' AS title
    FROM resource
    WHERE resource = 'dashboards'
      AND name LIKE 'forge-metal-%'
    ORDER BY name;
    " >"${artifact_dir}/postgres-grafana-dashboards.tsv"
    ;;
  dashboard)
    remote_psql grafana "
    SELECT uid AS name, title
    FROM dashboard
    WHERE uid LIKE 'forge-metal-%'
    ORDER BY uid;
    " >"${artifact_dir}/postgres-grafana-dashboards.tsv"
    ;;
  *)
    echo "Could not find Grafana dashboard storage tables (resource/dashboard)" >&2
    exit 1
    ;;
esac

grafana_api_post "/api/ds/query" >"${artifact_dir}/grafana-datasource-query.json" <<JSON
{
  "queries": [
    {
      "refId": "A",
      "datasource": {
        "uid": "forge-metal-clickhouse",
        "type": "grafana-clickhouse-datasource"
      },
      "format": 1,
      "queryType": "sql",
      "rawSql": "SELECT 1 AS ok /* ${marker} */"
    }
  ],
  "from": "now-5m",
  "to": "now"
}
JSON

if ! jq -e '
  (.results | length) > 0
  and ([.results[] | select((.status // 0) != 200 or (.error? != null))] | length) == 0
' "${artifact_dir}/grafana-datasource-query.json" >/dev/null; then
  echo "Grafana datasource baseline query assertion failed" >&2
  exit 1
fi

ch_query "
SELECT
  name,
  auth_type
FROM system.users
WHERE name = 'grafana_observer'
FORMAT TSVWithNames
" >"${artifact_dir}/clickhouse-user-auth.tsv"

# ClickHouse does not surface listener ports in system.settings/server_settings,
# so the live socket table is the only reliable proof that 8443/9440 are bound.
verification_ssh "sudo ss -ltnH '( sport = :8443 or sport = :9440 )'" \
  >"${artifact_dir}/clickhouse-secure-listeners.tsv"

grafana_api_get "/api/search?type=dash-db" >"${artifact_dir}/grafana-dashboard-search.json"

jq -r '.[] | select(.uid | startswith("forge-metal-")) | .uid' \
  "${artifact_dir}/grafana-dashboard-search.json" \
  | sort >"${artifact_dir}/grafana-dashboard-uids.tsv"

dashboard_count="$(wc -l <"${artifact_dir}/grafana-dashboard-uids.tsv")"
if [[ "${dashboard_count}" -ne "${expected_dashboard_count}" ]]; then
  echo "Expected ${expected_dashboard_count} Forge Metal dashboards, got ${dashboard_count}" >&2
  exit 1
fi

mapfile -t dashboard_uids <"${artifact_dir}/grafana-dashboard-uids.tsv"
dashboard_query_count=0
for dashboard_uid in "${dashboard_uids[@]}"; do
  dashboard_json="${artifact_dir}/grafana-dashboard-${dashboard_uid}.json"
  request_json="${artifact_dir}/grafana-dashboard-${dashboard_uid}-query-request.json"
  response_json="${artifact_dir}/grafana-dashboard-${dashboard_uid}-query-response.json"

  grafana_api_get "/api/dashboards/uid/${dashboard_uid}" >"${dashboard_json}"

  jq \
    --arg run_id "${run_id}" \
    --arg dashboard_uid "${dashboard_uid}" \
    --arg datasource_uid "forge-metal-clickhouse" \
    '
    [
      .dashboard.panels[]
      | select(.targets != null)
      | . as $panel
      | $panel.targets[]
      | select(.rawSql? != null)
      | . + {
          datasource: (.datasource // {
            "type": "grafana-clickhouse-datasource",
            "uid": $datasource_uid
          }),
          refId: ("P" + ($panel.id | tostring)),
          rawSql: (
            .rawSql
            + "\n/* fm:grafana verify="
            + $run_id
            + " dashboard="
            + $dashboard_uid
            + " panel="
            + ($panel.id | tostring)
            + " */"
          )
        }
    ] as $queries
    | {
        queries: $queries,
        from: "now-5m",
        to: "now"
      }
    ' "${dashboard_json}" >"${request_json}"

  query_count="$(jq '.queries | length' "${request_json}")"
  if [[ "${query_count}" -eq 0 ]]; then
    echo "Dashboard ${dashboard_uid} did not contain ClickHouse panel queries" >&2
    exit 1
  fi
  dashboard_query_count=$((dashboard_query_count + query_count))

  grafana_api_post "/api/ds/query" <"${request_json}" >"${response_json}"

  if ! jq -e '
    (.results | length) > 0
    and ([.results[] | select((.status // 0) != 200 or (.error? != null))] | length) == 0
  ' "${response_json}" >/dev/null; then
    echo "Dashboard ${dashboard_uid} ClickHouse query assertion failed" >&2
    exit 1
  fi
done

ch_query "SYSTEM FLUSH LOGS" >/dev/null

ch_query "
SELECT
  event_time,
  type,
  initial_user,
  exception_code,
  query
FROM system.query_log
WHERE event_time >= parseDateTimeBestEffort('${started_at}')
  AND query LIKE '%${marker}%'
ORDER BY event_time, type
FORMAT TSVWithNames
" >"${artifact_dir}/clickhouse-query-log.tsv"

successful_dashboard_queries="$(ch_query "
SELECT count()
FROM system.query_log
WHERE event_time >= parseDateTimeBestEffort('${started_at}')
  AND type = 'QueryFinish'
  AND initial_user = 'grafana_observer'
  AND exception_code = 0
  AND query LIKE '%fm:grafana verify=${run_id} dashboard=%'
FORMAT TabSeparated
")"

sequenced_dashboard_queries="$(ch_query "
SELECT count()
FROM (
  SELECT
    query,
    minIf(event_time_microseconds, type = 'QueryStart') AS started_at_us,
    minIf(event_time_microseconds, type = 'QueryFinish') AS finished_at_us,
    countIf(type = 'QueryStart') AS starts,
    countIf(type = 'QueryFinish') AS finishes
  FROM system.query_log
  WHERE event_time >= parseDateTimeBestEffort('${started_at}')
    AND initial_user = 'grafana_observer'
    AND query LIKE '%fm:grafana verify=${run_id} dashboard=%'
  GROUP BY query
  HAVING starts >= 1
    AND finishes >= 1
    AND finished_at_us >= started_at_us
)
FORMAT TabSeparated
")"

cat >"${artifact_dir}/dashboard-query-summary.tsv" <<TSV
expected_dashboard_count	discovered_dashboard_count	expected_panel_query_count	successful_panel_query_count	sequenced_panel_query_count
${expected_dashboard_count}	${dashboard_count}	${dashboard_query_count}	${successful_dashboard_queries}	${sequenced_dashboard_queries}
TSV

for _ in $(seq 1 10); do
  ch_query "
  SELECT
    Timestamp,
    ServiceName,
    SeverityText,
    Body,
    toString(LogAttributes) AS attrs
  FROM default.otel_logs
  WHERE Timestamp >= parseDateTimeBestEffort('${started_at}')
    AND (
      ServiceName IN ('grafana', 'caddy')
      OR toString(LogAttributes) LIKE '%dashboard.${VERIFICATION_DOMAIN}%'
    )
  ORDER BY Timestamp
  FORMAT TSVWithNames
  " >"${artifact_dir}/otel-logs.tsv"

  if [[ "$(wc -l <"${artifact_dir}/otel-logs.tsv")" -gt 1 ]]; then
    break
  fi
  sleep 1
done

for _ in $(seq 1 10); do
  ch_query "
  SELECT
    Timestamp,
    TraceId,
    SpanId,
    ParentSpanId,
    ServiceName,
    SpanName,
    StatusCode
  FROM default.otel_traces
  WHERE Timestamp >= parseDateTimeBestEffort('${started_at}')
    AND ServiceName = 'zitadel'
    AND (
      SpanName LIKE '%/oauth/%'
      OR SpanName LIKE '%/oidc/%'
      OR SpanName LIKE '%/ui/login/%'
    )
  ORDER BY Timestamp
  FORMAT TSVWithNames
  " >"${artifact_dir}/otel-traces.tsv"

  if [[ "$(wc -l <"${artifact_dir}/otel-traces.tsv")" -gt 1 ]]; then
    break
  fi
  sleep 1
done

if ! grep -q $'grafana' "${artifact_dir}/postgres-grafana-role.tsv"; then
  echo "Grafana PostgreSQL role assertion failed" >&2
  exit 1
fi

if ! grep -q $'grafana' "${artifact_dir}/postgres-grafana-database.tsv"; then
  echo "Grafana PostgreSQL database assertion failed" >&2
  exit 1
fi

if [[ "$(wc -l <"${artifact_dir}/postgres-grafana-dashboards.tsv")" -ne "${expected_dashboard_count}" ]]; then
  echo "Grafana PostgreSQL dashboard provisioning assertion failed" >&2
  exit 1
fi

if ! grep -q $'QueryFinish\tgrafana_observer\t0' "${artifact_dir}/clickhouse-query-log.tsv"; then
  echo "Grafana datasource did not produce a successful grafana_observer QueryFinish" >&2
  exit 1
fi

if ! grep -q 'ssl_certificate' "${artifact_dir}/clickhouse-user-auth.tsv"; then
  echo "Grafana ClickHouse user is not certificate-authenticated" >&2
  exit 1
fi

if ! grep -q ':8443' "${artifact_dir}/clickhouse-secure-listeners.tsv"; then
  echo "Grafana verification expected ClickHouse listener on 8443" >&2
  exit 1
fi

if ! grep -q ':9440' "${artifact_dir}/clickhouse-secure-listeners.tsv"; then
  echo "Grafana verification expected ClickHouse listener on 9440" >&2
  exit 1
fi

if [[ "${successful_dashboard_queries}" -lt "${dashboard_query_count}" ]]; then
  echo "Grafana dashboard queries produced ${successful_dashboard_queries}/${dashboard_query_count} successful ClickHouse QueryFinish rows" >&2
  exit 1
fi

if [[ "${sequenced_dashboard_queries}" -lt "${dashboard_query_count}" ]]; then
  echo "Grafana dashboard queries produced ${sequenced_dashboard_queries}/${dashboard_query_count} ordered QueryStart->QueryFinish sequences" >&2
  exit 1
fi

if ! grep -Eq $'\t(grafana|caddy)\t' "${artifact_dir}/otel-logs.tsv"; then
  echo "Grafana verification did not produce Grafana/Caddy OTel log evidence" >&2
  exit 1
fi

if [[ "$(wc -l <"${artifact_dir}/otel-traces.tsv")" -le 1 ]]; then
  echo "Grafana verification did not produce Zitadel OAuth trace evidence in ClickHouse" >&2
  exit 1
fi

cat >"${artifact_dir}/run.json" <<JSON
{
  "verification_run_id": "${run_id}",
  "dashboard_url": "${dashboard_url}",
  "started_at": "${started_at}",
  "marker": "${marker}",
  "status": "succeeded"
}
JSON

echo "Grafana verification evidence: ${artifact_dir}"
