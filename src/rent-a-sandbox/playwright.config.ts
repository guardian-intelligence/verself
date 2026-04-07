import { defineConfig, devices } from "@playwright/test";

// E2E tests run against the live deployment.
// Override via environment variables for different environments.
const BASE_URL = process.env.BASE_URL || "https://rentasandbox.anveio.com";

export default defineConfig({
  testDir: "./e2e",
  timeout: 120_000, // 2 min — Stripe checkout redirect flow is slow
  expect: { timeout: 15_000 },
  retries: 1,
  workers: 1, // Serial — tests share billing state
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
    video: "on-first-retry",
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
