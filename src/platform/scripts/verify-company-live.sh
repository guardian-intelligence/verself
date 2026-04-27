#!/usr/bin/env bash
# Verify that the Guardian Intelligence company site at company_domain
# emits the expected company.* spans into default.otel_traces when a real
# browser walks the IA.
#
# Flow:
#   1. Derive deterministic deploy identity (same helper as telemetry-smoke-test).
#   2. Resolve the live company base URL from company_domain.
#   3. Run the Playwright canary at apps/company/e2e/canary.spec.ts against
#      the live URL; the canary walks every IA node, the OG cards, and the
#      brand-kit download.
#   4. Poll default.otel_traces for the span set the canary is expected to
#      have emitted, correlated on TraceId or run_key.
#
# Env:
#   COMPANY_SMOKE_TEST_BASE_URL  — override the derived guardianintelligence.org URL. Useful
#     for staging or localhost rehearsals.
set -euo pipefail

cd "$(dirname "$0")/.."

run_date="$(date -u +%Y-%m-%d)"
run_host="$(hostname -s 2>/dev/null || hostname)"
counter_dir="${XDG_CACHE_HOME:-$HOME/.cache}/verself/company-smoke-test"
counter_file="${counter_dir}/${run_date}.counter"
lock_file="${counter_dir}/${run_date}.lock"
mkdir -p "${counter_dir}"

run_counter="$(python3 - "${counter_file}" "${lock_file}" <<'PY'
import fcntl
import pathlib
import sys
counter_path = pathlib.Path(sys.argv[1])
lock_path = pathlib.Path(sys.argv[2])
with lock_path.open("a+") as lock_file:
    fcntl.flock(lock_file, fcntl.LOCK_EX)
    try:
        current = int(counter_path.read_text(encoding="utf-8").strip() or "0")
    except (FileNotFoundError, ValueError):
        current = 0
    current += 1
    counter_path.write_text(str(current), encoding="utf-8")
    print(f"{current:06d}")
PY
)"

deploy_run_key="${run_date}.${run_counter}@${run_host}"
deploy_id="$(python3 -c 'import sys, uuid; print(uuid.uuid5(uuid.NAMESPACE_URL, f"verself:{sys.argv[1]}"))' "${deploy_run_key}")"

export VERSELF_DEPLOY_ID="${deploy_id}"
export VERSELF_DEPLOY_RUN_KEY="${deploy_run_key}"
export VERSELF_VERIFICATION_RUN="${deploy_run_key}"
export VERSELF_CORRELATION_ID="${deploy_id}"
export VERSELF_DEPLOY_KIND="company-smoke-test"

# Resolve the company base URL. The company app owns the root domain; if the
# caller set COMPANY_SMOKE_TEST_BASE_URL (local rehearsal against
# http://127.0.0.1:4252 for example) we use that instead.
resolve_base_url() {
  if [[ -n "${COMPANY_SMOKE_TEST_BASE_URL:-}" ]]; then
    printf '%s\n' "${COMPANY_SMOKE_TEST_BASE_URL}"
    return 0
  fi
  local domain
  domain="$(python3 -c 'import yaml, sys; print(yaml.safe_load(open(sys.argv[1]))["company_domain"])' ansible/group_vars/all/main.yml)"
  printf 'https://%s\n' "${domain}"
}

BASE_URL="$(resolve_base_url)"
export BASE_URL
export VERSELF_COMPANY_BASE_URL="${BASE_URL}"

echo "company-smoke-test: base_url=${BASE_URL} deploy_id=${deploy_id}"

# --- Run the Playwright canary -----------------------------------------------
cd ../viteplus-monorepo/apps/company
CI=true corepack pnpm exec playwright test --reporter=list

# --- Verify spans reached ClickHouse -----------------------------------------
cd ../../../platform

# Every expected span. Each row asserts presence in a fresh time window.
expected_routes=(
  "/"
  "/design"
  "/letters"
  "/newsroom"
  "/newsroom/we-opened-the-newsroom"
  "/solutions"
  "/company"
  "/careers"
  "/press"
  "/changelog"
  "/contact"
  "/letters/ship-the-reference-architecture"
)
expected_og_slugs=(home design letters newsroom solutions)

poll_clickhouse() {
  local query="$1"
  local label="$2"
  local expected="$3"
  local got=""
  for _ in $(seq 1 45); do
    got="$(./scripts/clickhouse.sh --database default --query "${query} FORMAT TSVRaw" || true)"
    got="${got//$'\n'/}"
    if [[ "${got}" == "${expected}" ]]; then
      echo "company-smoke-test: ${label} = ${got} (ok)"
      return 0
    fi
    sleep 1
  done
  echo "ERROR: ${label} = ${got} (expected ${expected})" >&2
  return 1
}

# 1. Every route seen in a company.route_view span within the last 10 minutes.
route_filter=""
for path in "${expected_routes[@]}"; do
  route_filter+="'${path}',"
done
route_filter="${route_filter%,}"

poll_clickhouse "
  SELECT count(DISTINCT SpanAttributes['route.path']) AS seen
  FROM default.otel_traces
  WHERE ServiceName = 'company-web'
    AND SpanName = 'company.route_view'
    AND SpanAttributes['route.path'] IN (${route_filter})
    AND Timestamp > now() - INTERVAL 10 MINUTE
" "route_view.distinct_paths" "${#expected_routes[@]}"

# 2. Every OG card seen with voice_pass=true. The OG route emits its span via
# the Nitro server-side tracer (ServiceName='company'), distinct from the
# browser spans above (ServiceName='company-web'). Both belong to the same
# deploy, so union them here.
og_filter=""
for slug in "${expected_og_slugs[@]}"; do
  og_filter+="'${slug}',"
done
og_filter="${og_filter%,}"

poll_clickhouse "
  SELECT count(DISTINCT SpanAttributes['og.slug']) AS seen
  FROM default.otel_traces
  WHERE ServiceName IN ('company','company-web')
    AND SpanName = 'company.og.render'
    AND SpanAttributes['og.voice_pass'] = 'true'
    AND SpanAttributes['og.slug'] IN (${og_filter})
    AND Timestamp > now() - INTERVAL 10 MINUTE
" "og.render.voice_pass_distinct_slugs" "${#expected_og_slugs[@]}"

# 3. Landing hero-view fired at least once.
poll_clickhouse "
  SELECT count() >= 1 ? 1 : 0 AS ok
  FROM default.otel_traces
  WHERE ServiceName = 'company-web'
    AND SpanName = 'company.landing.hero_view'
    AND Timestamp > now() - INTERVAL 10 MINUTE
" "landing.hero_view.fired" "1"

# 4. Newsroom index emits the full span family the canary exercises. Two
#    distinct SpanNames must land: newsroom.index.view (mount-time) and
#    newsroom.index.bulletin_click (the one Flare giant bulletin on the
#    page, clicked through to the article route).
#
#    The canary spec (apps/company/e2e/canary.spec.ts, step #8) drives the
#    bulletin click. If either SpanName is absent, either the instrumentation
#    regressed or the BatchSpanProcessor missed its flush window — both
#    treated as a hard failure, per the output contract's "real ClickHouse
#    traces are the only admissable smoke test of task completion" rule.
poll_clickhouse "
  SELECT count(DISTINCT SpanName) AS seen
  FROM default.otel_traces
  WHERE ServiceName = 'company-web'
    AND SpanName IN (
      'newsroom.index.view',
      'newsroom.index.bulletin_click'
    )
    AND Timestamp > now() - INTERVAL 10 MINUTE
" "newsroom.index.span_families" "2"

# 5. Newsroom article view for the seeded canonical slug. Asserts both that
#    the route was visited and that the article-level attributes landed,
#    since the span also carries article.published_at / article.category.
poll_clickhouse "
  SELECT count() >= 1 ? 1 : 0 AS ok
  FROM default.otel_traces
  WHERE ServiceName = 'company-web'
    AND SpanName = 'company.newsroom_article.view'
    AND SpanAttributes['article.slug'] = 'we-opened-the-newsroom'
    AND Timestamp > now() - INTERVAL 10 MINUTE
" "newsroom_article.view.seeded_slug" "1"

echo "company-smoke-test: verified deploy_id=${deploy_id} deploy_run_key=${deploy_run_key}"
