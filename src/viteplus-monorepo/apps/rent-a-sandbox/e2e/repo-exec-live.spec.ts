import fs from "node:fs/promises";
import path from "node:path";
import { test, expect, type Page } from "@playwright/test";
import { env } from "./env";
import {
  ensureLoggedIn,
  ensureTestUserExists,
  installVerificationOverlay,
  pushVerificationRepoRevision,
  readBalance,
  type VerificationRepoMeta,
} from "./helpers";

const verificationRunHeader = "X-Forge-Metal-Verification-Run";

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

    const run = {
      verification_run_id: verificationRunID,
      repo_url: env.verificationRepoURL,
      ref: env.verificationRepoRef,
      repo_id: "",
      bootstrap_generation_id: "",
      bootstrap_execution_id: "",
      bootstrap_attempt_id: "",
      bootstrap_source_sha: "",
      refresh_generation_id: "",
      refresh_execution_id: "",
      refresh_attempt_id: "",
      refreshed_commit_sha: "",
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

      await page.goto("/repos/new");
      await page.waitForLoadState("networkidle");
      await expect(page.getByRole("heading", { name: "Import Repo" })).toBeVisible();

      const repoInput = page.getByLabel("Clone URL");
      const defaultBranchInput = page.getByLabel("Default branch");
      const submitButton = page.getByRole("button", { name: "Import Repo" });

      await repoInput.fill(env.verificationRepoURL);
      await defaultBranchInput.fill("main");
      await expect(repoInput).toHaveValue(env.verificationRepoURL);
      await expect(defaultBranchInput).toHaveValue("main");
      await expect(submitButton).toBeEnabled();

      await Promise.all([
        page.waitForURL(/\/repos\/[0-9a-f-]+$/, { timeout: 60_000 }),
        submitButton.click(),
      ]);
      run.repo_id = requireRouteID(page.url(), "/repos/");

      await page.waitForLoadState("networkidle");
      await waitForRepoState(page, "ready");
      const bootstrap = await readActiveGolden(page);
      run.bootstrap_generation_id = bootstrap.generation_id;
      run.bootstrap_execution_id = bootstrap.execution_id;
      run.bootstrap_source_sha = bootstrap.source_sha;

      await page.screenshot({ path: testInfo.outputPath("repo-ready.png"), fullPage: true });

      const firstExecution = await launchExecutionFromRepo(page);
      run.execution_id = firstExecution.execution_id;
      run.attempt_id = firstExecution.attempt_id;
      run.detail_url = `/jobs/${firstExecution.execution_id}`;

      await waitForExecutionSuccess(page, firstExecution.execution_id, env.verificationLogMarker);

      const refreshedRepoMeta: VerificationRepoMeta = await pushVerificationRepoRevision(
        `${verificationRunID}-refresh`,
      );
      run.refreshed_commit_sha = refreshedRepoMeta.commit_sha;

      await page.goto(`/repos/${run.repo_id}`);
      await page.waitForLoadState("networkidle");

      await page.getByRole("button", { name: "Rescan" }).click();
      await expect
        .poll(() => readInfoCardValue(page, "Last scanned SHA"), { timeout: 60_000 })
        .toBe(refreshedRepoMeta.commit_sha.slice(0, 12));
      await expect.poll(() => readRepoState(page), { timeout: 30_000 }).toBe("ready");
      await expect(page.getByText(refreshedRepoMeta.commit_sha.slice(0, 12))).toBeVisible({
        timeout: 30_000,
      });

      await page.getByRole("button", { name: /^(Prepare|Refresh) Golden$/ }).click();

      await expect
        .poll(() => readInfoRowValue(page, "Source SHA"), { timeout: 300_000 })
        .toBe(refreshedRepoMeta.commit_sha.slice(0, 12));
      await waitForRepoState(page, "ready");
      const refreshedGolden = await readActiveGolden(page);
      run.refresh_generation_id = refreshedGolden.generation_id;
      run.refresh_execution_id = refreshedGolden.execution_id;

      const secondExecution = await launchExecutionFromRepo(page);
      run.execution_id = secondExecution.execution_id;
      run.attempt_id = secondExecution.attempt_id;
      run.detail_url = `/jobs/${secondExecution.execution_id}`;

      await waitForExecutionSuccess(page, secondExecution.execution_id, env.verificationLogMarker);
      await expect(page.getByText(refreshedRepoMeta.commit_sha)).toBeVisible({ timeout: 30_000 });

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
        page.getByRole("link", { name: secondExecution.execution_id.slice(0, 8) }),
      ).toBeVisible({
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

async function waitForRepoState(page: Page, state: string): Promise<void> {
  await expect(page.getByText(state.replaceAll("_", " "), { exact: true }).first()).toBeVisible({
    timeout: 300_000,
  });
}

async function launchExecutionFromRepo(page: Page) {
  await Promise.all([
    page.waitForURL(/\/jobs\/[0-9a-f-]+$/, { timeout: 60_000 }),
    page.getByRole("button", { name: "Run Execution" }).click(),
  ]);
  await page.waitForLoadState("networkidle");
  return {
    execution_id: requireRouteID(page.url(), "/jobs/"),
    attempt_id: "",
    status: "queued",
  };
}

async function waitForExecutionSuccess(
  page: Page,
  executionID: string,
  logMarker: string,
): Promise<void> {
  await expect(page).toHaveURL(new RegExp(`/jobs/${executionID}`));
  await expect(page.getByText("succeeded", { exact: true })).toBeVisible({
    timeout: 300_000,
  });
  await expect(page.locator("pre")).toContainText(logMarker, {
    timeout: 60_000,
  });
}

async function readActiveGolden(page: Page): Promise<{
  generation_id: string;
  execution_id: string;
  source_sha: string;
}> {
  const executionHref = await page
    .getByRole("link", { name: "View bootstrap execution" })
    .getAttribute("href");
  return {
    generation_id: await readInfoRowValue(page, "Generation"),
    execution_id: executionHref ? requireRouteID(executionHref, "/jobs/") : "",
    source_sha: await readInfoRowValue(page, "Source SHA"),
  };
}

async function readInfoRowValue(page: Page, label: string): Promise<string> {
  const labelNode = page.locator("span", { hasText: label }).first();
  const valueNode = labelNode.locator("xpath=following-sibling::span[1]");
  await expect(valueNode).toBeVisible({ timeout: 30_000 });
  return (await valueNode.textContent())?.trim() ?? "";
}

async function readInfoCardValue(page: Page, label: string): Promise<string> {
  const labelNode = page.locator(`xpath=//div[normalize-space(text())='${label}']`).first();
  const valueNode = labelNode.locator("xpath=following-sibling::div[1]");
  await expect(valueNode).toBeVisible({ timeout: 30_000 });
  return (await valueNode.textContent())?.trim() ?? "";
}

async function readRepoState(page: Page): Promise<string> {
  const heading = page.getByRole("heading", { level: 1 }).first();
  const stateNode = heading.locator("xpath=following-sibling::*[1]");
  await expect(stateNode).toBeVisible({ timeout: 30_000 });
  return (await stateNode.textContent())?.trim() ?? "";
}

function requireRouteID(urlOrPath: string, prefix: string): string {
  const match = urlOrPath.match(new RegExp(`${prefix}([0-9a-f-]+)`));
  if (!match?.[1]) {
    throw new Error(`Could not extract route id from ${urlOrPath}`);
  }
  return match[1];
}
