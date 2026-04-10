import { test, expect } from "@playwright/test";
import { env } from "./env";
import {
  ensureLoggedIn,
  ensureTestUserExists,
  installVerificationHeader,
  installVerificationOverlay,
  readBalance,
} from "./helpers";
import { importRepoFromURL, launchExecutionFromRepo, waitForExecutionSuccess } from "./repo-flow";
import { createVerificationRun, persistVerificationRun } from "./verification-run";

test.use({
  trace: "on",
  video: "on",
  screenshot: "on",
});

test.describe("Sandbox Repo Execute Verification", () => {
  test.describe.configure({ timeout: 600_000 });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("runs an execution from an imported repo", async ({ page, context }, testInfo) => {
    const verificationRunID =
      env.verificationRunID ||
      `sandbox-execute-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const runJSONPath = env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");

    await installVerificationOverlay(page, verificationRunID);
    await installVerificationHeader(context, verificationRunID);

    const run = createVerificationRun(verificationRunID);
    try {
      await ensureLoggedIn(page);
      run.started_balance = await readBalance(page);
      Object.assign(run, await importRepoFromURL(page, env.verificationRepoURL));

      const execution = await launchExecutionFromRepo(page);
      run.execution_id = execution.execution_id;
      run.attempt_id = execution.attempt_id;
      run.detail_url = `/jobs/${execution.execution_id}`;

      await waitForExecutionSuccess(page, execution.execution_id, env.verificationLogMarker);
      run.status = "succeeded";

      await expect(async () => {
        await page.goto("/");
        await page.waitForLoadState("networkidle");
        const refreshedBalance = await readBalance(page);
        expect(refreshedBalance).toBeLessThan(run.started_balance);
        run.finished_balance = refreshedBalance;
      }).toPass({ timeout: 60_000, intervals: [2_000, 5_000] });

      await page.screenshot({
        path: testInfo.outputPath("repo-execution-complete.png"),
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
