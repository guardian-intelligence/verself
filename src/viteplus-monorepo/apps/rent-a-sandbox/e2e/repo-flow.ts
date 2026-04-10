import { expect, type Page } from "@playwright/test";
import type { VerificationRepoMeta } from "./helpers";

export async function importRepoFromURL(
  page: Page,
  repoURL: string,
  defaultBranch = "main",
): Promise<{
  repo_id: string;
  bootstrap_generation_id: string;
  bootstrap_execution_id: string;
  bootstrap_source_sha: string;
}> {
  await page.goto("/repos/new");
  await page.waitForLoadState("networkidle");
  await expect(page.getByRole("heading", { name: "Import Repo" })).toBeVisible();

  const repoInput = page.getByLabel("Clone URL");
  const defaultBranchInput = page.getByLabel("Default branch");
  const submitButton = page.getByRole("button", { name: "Import Repo" });

  await repoInput.fill(repoURL);
  await defaultBranchInput.fill(defaultBranch);
  await expect(repoInput).toHaveValue(repoURL);
  await expect(defaultBranchInput).toHaveValue(defaultBranch);
  await expect(submitButton).toBeEnabled();

  await Promise.all([
    page.waitForURL(/\/repos\/[0-9a-f-]+$/, { timeout: 60_000 }),
    submitButton.click(),
  ]);

  await page.waitForLoadState("networkidle");
  await waitForRepoState(page, "ready");
  const bootstrap = await readActiveGolden(page);

  return {
    repo_id: requireRouteID(page.url(), "/repos/"),
    bootstrap_generation_id: bootstrap.generation_id,
    bootstrap_execution_id: bootstrap.execution_id,
    bootstrap_source_sha: bootstrap.source_sha,
  };
}

export async function refreshRepoGolden(
  page: Page,
  repoID: string,
  refreshedRepoMeta: VerificationRepoMeta,
): Promise<{
  refresh_generation_id: string;
  refresh_execution_id: string;
  refreshed_commit_sha: string;
}> {
  await page.goto(`/repos/${repoID}`);
  await page.waitForLoadState("networkidle");

  await page.getByRole("button", { name: "Rescan" }).click();
  await expect
    .poll(() => readInfoCardValue(page, "Last scanned SHA"), {
      timeout: 60_000,
    })
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

  return {
    refresh_generation_id: refreshedGolden.generation_id,
    refresh_execution_id: refreshedGolden.execution_id,
    refreshed_commit_sha: refreshedRepoMeta.commit_sha,
  };
}

export async function waitForRepoState(page: Page, state: string): Promise<void> {
  await expect(page.getByText(state.replaceAll("_", " "), { exact: true }).first()).toBeVisible({
    timeout: 300_000,
  });
}

export async function launchExecutionFromRepo(page: Page) {
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

export async function waitForExecutionSuccess(
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

export function requireRouteID(urlOrPath: string, prefix: string): string {
  const match = urlOrPath.match(new RegExp(`${prefix}([0-9a-f-]+)`));
  if (!match?.[1]) {
    throw new Error(`Could not extract route id from ${urlOrPath}`);
  }
  return match[1];
}
