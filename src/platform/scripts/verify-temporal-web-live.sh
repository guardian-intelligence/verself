#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=src/platform/scripts/lib/verification-context.sh
source "${script_dir}/lib/verification-context.sh"
verification_context_init "${BASH_SOURCE[0]}"

run_id="${VERIFICATION_RUN_ID:-temporal-web-smoke-test-$(date -u +%Y%m%dT%H%M%SZ)}"
artifact_root="${VERIFICATION_ARTIFACT_ROOT:-${VERIFICATION_SMOKE_ARTIFACT_ROOT}/temporal-web-smoke-test}"
artifact_dir="${artifact_root}/${run_id}"
browser_log_path="${artifact_dir}/browser.log"
mkdir -p "${artifact_dir}/clickhouse"
verself_web_app_dir="${VERIFICATION_REPO_ROOT}/src/viteplus-monorepo/apps/verself-web"

window_start="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
temporal_host="temporal.${VERIFICATION_DOMAIN}"
auth_host="auth.${VERIFICATION_DOMAIN}"
temporal_base_url="https://${temporal_host}"
trust_domain="spiffe.${VERIFICATION_DOMAIN}"
temporal_web_spiffe_id="spiffe://${trust_domain}/svc/temporal-web"
temporal_sandbox_namespace="sandbox-rental-service"
temporal_billing_namespace="billing-service"
persona_env="${artifact_dir}/platform-admin.env"
persona_metadata_path="${artifact_dir}/platform-admin.json"
browser_script_path="$(mktemp "${verself_web_app_dir}/.temporal-web-smoke-test-XXXXXX.mjs")"

cleanup() {
  rm -f "${browser_script_path}"
}
trap cleanup EXIT

wait_for_public_health() {
  local name="$1"
  local url="$2"
  local host="$3"
  local expected_status="$4"

  for _ in $(seq 1 60); do
    local code
    code="$(
      curl -k -s -o /dev/null -w '%{http_code}' \
        --resolve "${host}:443:${VERIFICATION_REMOTE_HOST}" \
        "${url}" || true
    )"
    if [[ "${code}" == "${expected_status}" ]]; then
      return 0
    fi
    sleep 1
  done

  echo "${name} did not return ${expected_status} in time: ${url}" >&2
  return 1
}

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

verification_ssh "sudo systemctl is-active temporal-server temporal-web zitadel caddy" \
  >"${artifact_dir}/systemd-active.txt"
verification_wait_for_loopback_api "Temporal Web loopback health" "http://127.0.0.1:4301/healthz" "200"
verification_ssh "sudo ss -ltnH '( sport = :7233 or sport = :4301 or sport = :8085 )'" \
  >"${artifact_dir}/listeners.tsv"

wait_for_public_health "Temporal Web public health" "${temporal_base_url}/healthz" "${temporal_host}" "200"

curl -k -sS \
  --resolve "${temporal_host}:443:${VERIFICATION_REMOTE_HOST}" \
  -o "${artifact_dir}/public-health.json" \
  "${temporal_base_url}/healthz"

curl -k -sS -o /dev/null \
  --resolve "${temporal_host}:443:${VERIFICATION_REMOTE_HOST}" \
  -w '%{http_code}\t%{redirect_url}\n' \
  "${temporal_base_url}/auth/sso?returnUrl=%2F" >"${artifact_dir}/oauth-redirect.tsv"

TEMPORAL_OAUTH_REDIRECT_TSV="${artifact_dir}/oauth-redirect.tsv" \
TEMPORAL_OAUTH_DOMAIN="${VERIFICATION_DOMAIN}" \
python3 - <<'PY'
import os
from pathlib import Path
from urllib.parse import urlparse

status, redirect_url = Path(os.environ["TEMPORAL_OAUTH_REDIRECT_TSV"]).read_text().strip().split("\t", 1)
if status not in {"302", "303", "307"}:
    raise SystemExit(f"unexpected Temporal Web OAuth redirect status: {status}")

parsed = urlparse(redirect_url)
domain = os.environ["TEMPORAL_OAUTH_DOMAIN"]
if parsed.scheme != "https" or parsed.netloc != f"auth.{domain}" or parsed.path != "/oauth/v2/authorize":
    raise SystemExit(f"unexpected Temporal Web OAuth authorize redirect: {redirect_url}")
PY

"${script_dir}/assume-persona.sh" platform-admin --output "${persona_env}" >"${persona_metadata_path}"
# shellcheck disable=SC1090
source "${persona_env}"

set +e
cat >"${browser_script_path}" <<'JS'
import fs from "node:fs/promises";
import { chromium } from "@playwright/test";

const artifactDir = process.env.ARTIFACT_DIR;
const baseURL = process.env.TEMPORAL_BASE_URL;
const temporalHost = process.env.TEMPORAL_HOST;
const authHost = process.env.AUTH_HOST;
const remoteHost = process.env.REMOTE_HOST;
const email = process.env.BROWSER_EMAIL;
const password = process.env.BROWSER_PASSWORD;
const shortTimeoutMS = 5_000;
const loginTimeoutMS = 30_000;
const pollIntervalMS = 100;

if (!artifactDir || !baseURL || !temporalHost || !authHost || !remoteHost || !email || !password) {
  throw new Error("missing Temporal Web smoke-test browser environment");
}

const browser = await chromium.launch({
  headless: true,
  args: [
    `--host-resolver-rules=MAP ${temporalHost} ${remoteHost},MAP ${authHost} ${remoteHost},EXCLUDE localhost`,
  ],
});
const context = await browser.newContext();
const page = await context.newPage();
const apiResponses = [];

page.on("response", async (response) => {
  const url = response.url();
  if (!url.startsWith(`${baseURL}/api/v1/`)) {
    return;
  }

  let body = "";
  let bodyExcerpt = "";
  try {
    body = await response.text();
    bodyExcerpt = body.slice(0, 4096);
  } catch (error) {
    body = "";
    bodyExcerpt = `<response body unavailable: ${error instanceof Error ? error.message : String(error)}>`;
  }

  apiResponses.push({
    body,
    bodyExcerpt,
    status: response.status(),
    url,
  });
});

async function isVisible(locator) {
  return locator.isVisible().catch(() => false);
}

async function isHomeReady({ namespacesNavLink, workflowsNavLink }) {
  const [namespacesVisible, workflowsVisible] = await Promise.all([
    isVisible(namespacesNavLink),
    isVisible(workflowsNavLink),
  ]);
  return namespacesVisible && workflowsVisible;
}

async function waitForAuthBoundary({ namespacesNavLink, workflowsNavLink, loginNameInput, passwordInput, loginRedirectButton, otherUserButton, skipButton }) {
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (
      (await isHomeReady({ namespacesNavLink, workflowsNavLink })) ||
      (await isVisible(loginNameInput)) ||
      (await isVisible(passwordInput)) ||
      (await isVisible(loginRedirectButton)) ||
      (await isVisible(otherUserButton)) ||
      (await isVisible(skipButton))
    ) {
      return;
    }

    await page.waitForLoadState("domcontentloaded").catch(() => {});
    await page.waitForTimeout(pollIntervalMS);
  }
}

async function waitForAPIResponse(label, predicate, timeoutMS) {
  const deadline = Date.now() + timeoutMS;

  while (Date.now() < deadline) {
    const match = apiResponses.find(predicate);
    if (match) {
      return match;
    }

    await page.waitForTimeout(pollIntervalMS);
  }

  throw new Error(
    `${label} did not complete within ${timeoutMS}ms: ${JSON.stringify(apiResponses.slice(-20), null, 2)}`,
  );
}

async function login() {
  const loginNameInput = page.locator("#loginName");
  const passwordInput = page.locator("#password");
  const loginRedirectButton = page.getByRole("button", { name: /click here/i });
  const otherUserButton = page.getByRole("button", { name: /other user/i });
  const skipButton = page.getByRole("button", { name: /^Skip$/ });
  const namespacesNavLink = page.getByRole("link", { name: /^Namespaces$/ });
  const workflowsNavLink = page.getByRole("link", { name: /^Workflows$/ });

  await page.goto(`${baseURL}/auth/sso?returnUrl=%2F`, {
    waitUntil: "domcontentloaded",
    timeout: shortTimeoutMS,
  });

  for (let attempt = 0; attempt < loginTimeoutMS / pollIntervalMS; attempt += 1) {
    if (await isHomeReady({ namespacesNavLink, workflowsNavLink })) {
      return;
    }

    if (await isVisible(loginRedirectButton)) {
      await loginRedirectButton.click();
      await waitForAuthBoundary({
        namespacesNavLink,
        workflowsNavLink,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await isVisible(otherUserButton)) {
      await otherUserButton.click();
      await waitForAuthBoundary({
        namespacesNavLink,
        workflowsNavLink,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await isVisible(loginNameInput)) {
      await loginNameInput.fill(email);
      await page.locator("button[type='submit']").click();
      await waitForAuthBoundary({
        namespacesNavLink,
        workflowsNavLink,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await isVisible(passwordInput)) {
      await passwordInput.fill(password);
      await page.locator("button[type='submit']").click();
      await waitForAuthBoundary({
        namespacesNavLink,
        workflowsNavLink,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await isVisible(skipButton)) {
      await skipButton.click();
      await waitForAuthBoundary({
        namespacesNavLink,
        workflowsNavLink,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    await page.waitForTimeout(pollIntervalMS);
  }

  throw new Error(`unable to complete Temporal Web login from ${page.url()}`);
}

try {
  await login();
  await page.waitForLoadState("domcontentloaded", { timeout: shortTimeoutMS }).catch(() => {});

  const namespacesResponse = await waitForAPIResponse(
    "Temporal Web namespaces response",
    (response) =>
      response.url.startsWith(`${baseURL}/api/v1/namespaces`) && response.status === 200,
    shortTimeoutMS,
  );
  if (
    !namespacesResponse.body.includes(process.env.TEMPORAL_SANDBOX_NAMESPACE ?? "") ||
    !namespacesResponse.body.includes(process.env.TEMPORAL_BILLING_NAMESPACE ?? "")
  ) {
    throw new Error(
      `Temporal Web namespaces payload did not include the service-owned namespaces: ${namespacesResponse.bodyExcerpt}`,
    );
  }

  await waitForAPIResponse(
    "Temporal Web cluster info response",
    (response) =>
      response.url.startsWith(`${baseURL}/api/v1/cluster-info`) && response.status === 200,
    shortTimeoutMS,
  );

  await page.screenshot({ path: `${artifactDir}/temporal-web-home.png`, fullPage: true });
  await fs.writeFile(
    `${artifactDir}/browser-result.json`,
    JSON.stringify(
      {
        api_responses: apiResponses,
        current_url: page.url(),
        title: await page.title(),
        visible_text_excerpt: (await page.locator("body").innerText().catch(() => "")).slice(0, 4096),
      },
      null,
      2,
    ),
    "utf8",
  );
} finally {
  await context.close();
  await browser.close();
}
JS
# shellcheck disable=SC2016 # Positional args are expanded inside the child shell.
env \
  TEMPORAL_BASE_URL="${temporal_base_url}" \
  TEMPORAL_HOST="${temporal_host}" \
  AUTH_HOST="${auth_host}" \
  REMOTE_HOST="${VERIFICATION_REMOTE_HOST}" \
  ARTIFACT_DIR="${artifact_dir}" \
  BROWSER_EMAIL="${BROWSER_EMAIL}" \
  BROWSER_PASSWORD="${BROWSER_PASSWORD}" \
  TEMPORAL_SANDBOX_NAMESPACE="${temporal_sandbox_namespace}" \
  TEMPORAL_BILLING_NAMESPACE="${temporal_billing_namespace}" \
  TEMPORAL_BROWSER_SCRIPT="${browser_script_path}" \
  bash -lc '
    cd "$1"
    vp exec node "$TEMPORAL_BROWSER_SCRIPT"
  ' bash "${verself_web_app_dir}" >"${browser_log_path}" 2>&1
browser_status=$?
set -e

verification_tail_log_on_failure "${browser_status}" "${browser_log_path}" "200"
if [[ "${browser_status}" -ne 0 ]]; then
  exit "${browser_status}"
fi

window_end="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

(
  cd "${VERIFICATION_PLATFORM_ROOT}"
  ./scripts/clickhouse.sh --query "SYSTEM FLUSH LOGS"
) >"${artifact_dir}/clickhouse/flush-logs.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-web'
" 1 "${artifact_dir}/clickhouse/temporal-web-traces-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_logs
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-web'
" 1 "${artifact_dir}/clickhouse/temporal-web-logs-count.tsv"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'auth.spiffe.mtls.server'
    AND SpanAttributes['spiffe.peer_id'] = {temporal_web_spiffe_id:String}
" 1 "${artifact_dir}/clickhouse/temporal-server-mtls-from-web-count.tsv" --param_temporal_web_spiffe_id="${temporal_web_spiffe_id}"

wait_for_clickhouse_count default "
  SELECT count()
  FROM otel_traces
  WHERE Timestamp BETWEEN parseDateTime64BestEffort({window_start:String}) AND parseDateTime64BestEffort({window_end:String}) + INTERVAL 30 SECOND
    AND ServiceName = 'temporal-server'
    AND SpanName = 'temporal.auth.authorize'
    AND SpanAttributes['spiffe.peer_id'] = {temporal_web_spiffe_id:String}
    AND SpanAttributes['temporal.authz.decision'] = 'allow'
" 1 "${artifact_dir}/clickhouse/temporal-authz-web-allow-count.tsv" --param_temporal_web_spiffe_id="${temporal_web_spiffe_id}"

echo "temporal web smoke test ok: ${artifact_dir}"
