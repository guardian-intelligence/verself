import { defineConfig, devices } from "@playwright/test";
import { deriveAppBaseURL } from "@forge-metal/web-env";

const BASE_URL = deriveAppBaseURL("mail");
const verificationRunID = process.env.VERIFICATION_RUN_ID?.trim();

export default defineConfig({
  testDir: "./e2e",
  expect: { timeout: 5_000 },
  retries: 0,
  workers: 1,
  reporter: [["html", { open: "never" }], ["list"]],
  use: {
    baseURL: BASE_URL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    ignoreHTTPSErrors: true,
    ...(verificationRunID
      ? {
          extraHTTPHeaders: {
            "X-Forge-Metal-Verification-Run": verificationRunID,
          },
        }
      : {}),
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
