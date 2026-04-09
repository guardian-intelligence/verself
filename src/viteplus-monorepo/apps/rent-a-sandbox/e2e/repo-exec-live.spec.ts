import fs from "node:fs/promises";
import path from "node:path";
import { test, expect } from "@playwright/test";
import { env } from "./env";
import { ensureLoggedIn, ensureTestUserExists, installVerificationOverlay, readBalance } from "./helpers";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";

test.use({
  trace: "on",
  video: "on",
  screenshot: "on",
});

test.describe("Sandbox Repo Execution Live Verification", () => {
  test.describe.configure({ timeout: 180_000 });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("submit repo-backed sandbox, verify completion, logs, and billed balance delta", async ({
    page,
    context,
  }, testInfo) => {
    const verificationRunID =
      env.verificationRunID || `sandbox-live-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    const runJSONPath =
      env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");

    await installVerificationOverlay(page, verificationRunID);

    const run = {
      verification_run_id: verificationRunID,
      repo_url: env.verificationRepoURL,
      ref: env.verificationRepoRef,
      submit_requested_at: new Date().toISOString(),
      execution_id: "",
      attempt_id: "",
      started_balance: 0,
      finished_balance: 0,
      status: "unknown",
      detail_url: "",
      log_marker: env.verificationLogMarker,
      terminal_observed_at: "",
      error: "",
    };
    try {
      await ensureLoggedIn(page);
      await context.setExtraHTTPHeaders({
        [verificationRunHeader]: verificationRunID,
      });
      run.started_balance = await readBalance(page);

      await page.goto("/jobs/new");
      await page.waitForLoadState("networkidle");
      await expect(page.getByRole("heading", { name: "Create Sandbox" })).toBeVisible();

      const repoInput = page.getByLabel("Repository URL");
      const refInput = page.getByLabel("Ref");
      const submitButton = page.getByRole("button", { name: "Create Sandbox" });

      await repoInput.fill(env.verificationRepoURL);
      await refInput.fill(env.verificationRepoRef);
      await expect(repoInput).toHaveValue(env.verificationRepoURL);
      await expect(refInput).toHaveValue(env.verificationRepoRef);
      await expect(submitButton).toBeEnabled();

      const submitResponsePromise = page.waitForResponse(
        (resp) => resp.url().includes("/api/v1/executions") && resp.request().method() === "POST",
      );

      await submitButton.click();
      const submitResponse = await submitResponsePromise;
      expect(submitResponse.ok()).toBeTruthy();

      const submitJSON = (await submitResponse.json()) as {
        execution_id: string;
        attempt_id: string;
        status: string;
      };

      run.execution_id = submitJSON.execution_id;
      run.attempt_id = submitJSON.attempt_id;
      run.detail_url = `/jobs/${submitJSON.execution_id}`;

      await expect(page).toHaveURL(new RegExp(`/jobs/${submitJSON.execution_id}`));
      await page.screenshot({ path: testInfo.outputPath("submitted.png"), fullPage: true });

      await expect(page.getByText("succeeded", { exact: true })).toBeVisible({
        timeout: 180_000,
      });

      await expect(page.locator("pre")).toContainText(env.verificationLogMarker, {
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
      await expect(page.getByRole("link", { name: submitJSON.execution_id.slice(0, 8) })).toBeVisible({
        timeout: 30_000,
      });

      await page.screenshot({ path: testInfo.outputPath("completed.png"), fullPage: true });
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      if (!run.terminal_observed_at) {
        run.terminal_observed_at = new Date().toISOString();
      }
      await fs.mkdir(path.dirname(runJSONPath), { recursive: true });
      await fs.writeFile(runJSONPath, JSON.stringify(run, null, 2));
    }
  });
});
