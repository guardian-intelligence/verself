import { defineConfig, devices } from "@playwright/test";

// The company app serves the root domain. BASE_URL is set by the canary runner
// (see `make company-smoke-test` / scripts/verify-company-live.sh) and falls back to
// the local dev bind when a developer runs `pnpm -F @verself/company test:e2e`.
const BASE_URL = process.env.BASE_URL ?? "http://127.0.0.1:4252";

export default defineConfig({
  testDir: "./e2e",
  // Operation-level timeouts only. Everything is on local bare metal; any
  // single interaction that needs more than 5 seconds is a real failure.
  timeout: 0,
  expect: { timeout: 5_000 },
  retries: 0,
  workers: 1,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    actionTimeout: 5_000,
    navigationTimeout: 5_000,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "on-first-retry",
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
