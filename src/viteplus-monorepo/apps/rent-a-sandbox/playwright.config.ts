import { defineConfig, devices } from "@playwright/test";
import { deriveAppBaseURL } from "@forge-metal/web-env";

// E2E tests run against the live deployment.
// Override via environment variables for different environments.
const BASE_URL = deriveAppBaseURL("rentasandbox");
const RECORD_VERIFICATION_ARTIFACTS = process.env.FORGE_METAL_RECORD_ARTIFACTS === "1";

export default defineConfig({
  testDir: "./e2e",
  timeout: 120_000, // 2 min — Stripe checkout redirect flow is slow
  expect: { timeout: 15_000 },
  retries: 1,
  workers: 1, // Serial — tests share billing state
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    trace: RECORD_VERIFICATION_ARTIFACTS ? "on" : "on-first-retry",
    screenshot: RECORD_VERIFICATION_ARTIFACTS ? "on" : "only-on-failure",
    video: RECORD_VERIFICATION_ARTIFACTS ? "on" : "on-first-retry",
    // Accept self-signed certs for dev environments
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
