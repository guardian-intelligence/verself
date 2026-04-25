import { defineConfig, devices } from "@playwright/test";
import { deriveAppBaseURL } from "@forge-metal/web-env";

const BASE_URL = deriveAppBaseURL("console");
const RECORD_VERIFICATION_ARTIFACTS = process.env.FORGE_METAL_RECORD_ARTIFACTS === "1";

export default defineConfig({
  testDir: "./e2e",
  // Keep Playwright operation timeouts short; long business waits use explicit polling helpers.
  timeout: 0,
  expect: { timeout: 5_000 },
  retries: 0,
  workers: 1,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    actionTimeout: 5_000,
    navigationTimeout: 5_000,
    trace: RECORD_VERIFICATION_ARTIFACTS ? "on" : "on-first-retry",
    screenshot: RECORD_VERIFICATION_ARTIFACTS ? "on" : "only-on-failure",
    video: RECORD_VERIFICATION_ARTIFACTS ? "on" : "on-first-retry",
    ignoreHTTPSErrors: true,
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
