import { test } from "@playwright/test";
import { env } from "./env";
import {
  ensureLoggedIn,
  ensureTestUserExists,
  installVerificationHeader,
  installVerificationOverlay,
} from "./helpers";
import { importRepoFromURL } from "./repo-flow";
import { createVerificationRun, persistVerificationRun } from "./verification-run";

test.use({
  trace: "on",
  video: "on",
  screenshot: "on",
});

test.describe("Sandbox Repo Import Verification", () => {
  test.describe.configure({ timeout: 600_000 });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("imports repo and waits for the bootstrap golden", async ({ page, context }, testInfo) => {
    const verificationRunID =
      env.verificationRunID ||
      `sandbox-import-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const runJSONPath = env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");

    await installVerificationOverlay(page, verificationRunID);
    await installVerificationHeader(context, verificationRunID);

    const run = createVerificationRun(verificationRunID);
    try {
      await ensureLoggedIn(page);
      Object.assign(run, await importRepoFromURL(page, env.verificationRepoURL));
      run.detail_url = `/repos/${run.repo_id}`;
      run.status = "succeeded";

      await page.screenshot({
        path: testInfo.outputPath("repo-import-ready.png"),
        fullPage: true,
      });
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await persistVerificationRun(runJSONPath, run);
    }
  });
});
