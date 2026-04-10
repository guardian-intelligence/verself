import { test } from "@playwright/test";
import { env } from "./env";
import {
  ensureLoggedIn,
  ensureTestUserExists,
  installVerificationHeader,
  installVerificationOverlay,
  pushVerificationRepoRevision,
  type VerificationRepoMeta,
} from "./helpers";
import { importRepoFromURL, refreshRepoGolden } from "./repo-flow";
import { createVerificationRun, persistVerificationRun } from "./verification-run";

test.use({
  trace: "on",
  video: "on",
  screenshot: "on",
});

test.describe("Sandbox Repo Refresh Verification", () => {
  test.describe.configure({ timeout: 600_000 });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("rescans the repo and refreshes the golden after a new commit", async ({
    page,
    context,
  }, testInfo) => {
    const verificationRunID =
      env.verificationRunID ||
      `sandbox-refresh-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const runJSONPath = env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");

    await installVerificationOverlay(page, verificationRunID);
    await installVerificationHeader(context, verificationRunID);

    const run = createVerificationRun(verificationRunID);
    try {
      await ensureLoggedIn(page);
      Object.assign(run, await importRepoFromURL(page, env.verificationRepoURL));

      const refreshedRepoMeta: VerificationRepoMeta = await pushVerificationRepoRevision(
        `${verificationRunID}-refresh`,
      );
      Object.assign(run, await refreshRepoGolden(page, run.repo_id, refreshedRepoMeta));
      run.detail_url = `/repos/${run.repo_id}`;
      run.status = "succeeded";

      await page.screenshot({
        path: testInfo.outputPath("repo-refresh-ready.png"),
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
