import { test, expect } from "@playwright/test";
import { env } from "./env";
import {
  ensureLoggedIn,
  ensureTestUserExists,
  installVerificationHeader,
  installVerificationOverlay,
  pushVerificationRepoRevision,
  readBalance,
  type VerificationRepoMeta,
} from "./helpers";
import {
  importRepoFromURL,
  launchExecutionFromRepo,
  refreshRepoGolden,
  waitForExecutionSuccess,
} from "./repo-flow";
import { createVerificationRun, persistVerificationRun } from "./verification-run";

test.use({
  trace: "on",
  video: "on",
  screenshot: "on",
});

test.describe("Sandbox Repo Import Live Verification", () => {
  test.describe.configure({ timeout: 600_000 });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("import repo, bootstrap golden, execute, refresh, and execute again", async ({
    page,
    context,
  }, testInfo) => {
    const verificationRunID =
      env.verificationRunID ||
      `sandbox-live-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const runJSONPath = env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");

    await installVerificationOverlay(page, verificationRunID);
    await installVerificationHeader(context, verificationRunID);

    const run = createVerificationRun(verificationRunID);
    try {
      await ensureLoggedIn(page);
      run.started_balance = await readBalance(page);
      Object.assign(run, await importRepoFromURL(page, env.verificationRepoURL));

      await page.screenshot({
        path: testInfo.outputPath("repo-ready.png"),
        fullPage: true,
      });

      const firstExecution = await launchExecutionFromRepo(page);
      run.execution_id = firstExecution.execution_id;
      run.attempt_id = firstExecution.attempt_id;
      run.detail_url = `/jobs/${firstExecution.execution_id}`;

      await waitForExecutionSuccess(page, firstExecution.execution_id, env.verificationLogMarker);

      const refreshedRepoMeta: VerificationRepoMeta = await pushVerificationRepoRevision(
        `${verificationRunID}-refresh`,
      );
      Object.assign(run, await refreshRepoGolden(page, run.repo_id, refreshedRepoMeta));

      const secondExecution = await launchExecutionFromRepo(page);
      run.execution_id = secondExecution.execution_id;
      run.attempt_id = secondExecution.attempt_id;
      run.detail_url = `/jobs/${secondExecution.execution_id}`;

      await waitForExecutionSuccess(page, secondExecution.execution_id, env.verificationLogMarker);
      await expect(page.getByText(refreshedRepoMeta.commit_sha)).toBeVisible({
        timeout: 30_000,
      });

      run.terminal_observed_at = new Date().toISOString();
      run.status = "succeeded";

      await expect(async () => {
        await page.goto("/");
        await page.waitForLoadState("networkidle");
        const refreshedBalance = await readBalance(page);
        expect(refreshedBalance).toBeLessThan(run.started_balance);
        run.finished_balance = refreshedBalance;
      }).toPass({ timeout: 60_000, intervals: [2_000, 5_000] });

      await page.goto("/jobs", { waitUntil: "domcontentloaded" });
      await expect(
        page.getByRole("link", {
          name: secondExecution.execution_id.slice(0, 8),
        }),
      ).toBeVisible({
        timeout: 30_000,
      });

      await page.screenshot({
        path: testInfo.outputPath("completed.png"),
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
