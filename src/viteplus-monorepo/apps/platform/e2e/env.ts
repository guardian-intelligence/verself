import { deriveProductBaseURL } from "@verself/web-env";

// Test environment for the platform app. The site is fully public — no Zitadel
// dance needed — so the env shape stays minimal compared to console/e2e.
export const env = {
  baseURL: deriveProductBaseURL(),
  // Allows e2e tests to scope ClickHouse assertions to the in-flight deploy
  // when set; defaults to a per-run sentinel so re-running the suite locally
  // doesn't pick up stale spans from prior runs.
  deployRunKey:
    process.env.PLATFORM_E2E_DEPLOY_RUN_KEY?.trim() ||
    process.env.VERSELF_DEPLOY_RUN_KEY?.trim() ||
    "",
  // Optional override pointing at the worker host where ClickHouse runs.
  clickhouseRemote: process.env.PLATFORM_E2E_CLICKHOUSE_REMOTE?.trim() || "",
} as const;
