import { defineConfig, devices } from "@playwright/test";
import { deriveProductBaseURL } from "@verself/web-env";

const BASE_URL = deriveProductBaseURL();

export default defineConfig({
  testDir: "./e2e",
  // Operation-level timeouts only. The bare-metal box responds in tens of
  // milliseconds; anything slower than 5s is a real failure, not a slow box.
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
