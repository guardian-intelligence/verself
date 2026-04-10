import { expect } from "./harness";
import type { SandboxHarness, VerificationRepoMeta } from "./harness";

export async function importRepoFromURL(
  app: SandboxHarness,
  repoURL: string,
  defaultBranch = "main",
): Promise<{
  bootstrap_execution_id: string;
  bootstrap_generation_id: string;
  bootstrap_source_sha: string;
  repo_id: string;
  repo_name: string;
}> {
  await app.expectSSRHTML("/repos/new", ["Import Repo", "Clone URL", "Default branch"]);
  await app.assertStableRoute({
    path: "/repos/new",
    ready: app.page.getByRole("heading", { name: "Import Repo" }),
    expectedText: ["Import Repo", "Clone URL", "Default branch"],
  });

  const repoInput = app.page.getByLabel("Clone URL");
  const defaultBranchInput = app.page.getByLabel("Default branch");
  const submitButton = app.page.getByRole("button", { name: "Import Repo" });

  await repoInput.fill(repoURL);
  await defaultBranchInput.fill(defaultBranch);
  await submitButton.click();

  const repoId = await app.waitForCondition("repo import redirect", 30_000, async () => {
    const match = app.page.url().match(/\/repos\/([0-9a-f-]+)$/);
    return match?.[1] ?? false;
  });

  const repoName = await app.readText(app.page.getByRole("heading", { level: 1 }).first());
  const bootstrap = await waitForRepoReady(app);

  return {
    bootstrap_execution_id: bootstrap.bootstrap_execution_id,
    bootstrap_generation_id: bootstrap.bootstrap_generation_id,
    bootstrap_source_sha: bootstrap.bootstrap_source_sha,
    repo_id: repoId,
    repo_name: repoName,
  };
}

export async function assertRepoDetailSSRStable(
  app: SandboxHarness,
  repoId: string,
  repoName: string,
  expectedSourceSHA: string,
): Promise<void> {
  const shaPrefix = expectedSourceSHA.slice(0, 12);
  await app.expectSSRHTML(`/repos/${repoId}`, [repoName, "Active Golden", shaPrefix]);
  await app.assertStableRoute({
    path: `/repos/${repoId}`,
    ready: app.page.getByRole("heading", { name: repoName }),
    expectedText: [repoName, "Active Golden", "Repo Contract", shaPrefix],
  });
  await expect(app.page.getByText("ready", { exact: true }).first()).toBeVisible();
}

export async function refreshRepoGolden(
  app: SandboxHarness,
  repoId: string,
  refreshedRepoMeta: VerificationRepoMeta,
): Promise<{
  refresh_execution_id: string;
  refresh_generation_id: string;
  refreshed_commit_sha: string;
}> {
  await app.goto(`/repos/${repoId}`);
  const repoHeading = app.page.getByRole("heading", { level: 1 }).first();
  await expect(repoHeading).toBeVisible();

  const refreshedSha = refreshedRepoMeta.commit_sha.slice(0, 12);

  await app.page.getByRole("button", { name: "Rescan" }).click();
  await app.waitForCondition("repo rescan", 60_000, async () => {
    const scannedSHA = await readInfoCardValue(app, "Last scanned SHA");
    const state = await readRepoState(app);
    return scannedSHA === refreshedSha && state === "ready";
  });

  await app.page.getByRole("button", { name: /^(Prepare|Refresh) Golden$/ }).click();
  await app.waitForCondition("repo refresh", 180_000, async () => {
    const sourceSHA = await readInfoRowValue(app, "Source SHA");
    const state = await readRepoState(app);
    return sourceSHA === refreshedSha && state === "ready";
  });

  const executionHref = await app
    .page
    .getByRole("link", { name: "View bootstrap execution" })
    .getAttribute("href");

  return {
    refresh_execution_id: executionHref ? requireRouteID(executionHref, "/jobs/") : "",
    refresh_generation_id: await readInfoRowValue(app, "Generation"),
    refreshed_commit_sha: refreshedRepoMeta.commit_sha,
  };
}

export async function launchExecutionFromRepo(app: SandboxHarness): Promise<{
  execution_id: string;
}> {
  await app.page.getByRole("button", { name: "Run Execution" }).click();
  const executionId = await app.waitForCondition("execution launch redirect", 30_000, async () => {
    const match = app.page.url().match(/\/jobs\/([0-9a-f-]+)$/);
    return match?.[1] ?? false;
  });

  return { execution_id: executionId };
}

export async function assertJobsIndexHydratesExecutionList(
  app: SandboxHarness,
  executionId: string,
): Promise<void> {
  const executionLink = app.page.getByRole("link", {
    name: executionId.slice(0, 8),
  });

  await app.expectSSRHTML("/jobs", ["Executions", "Loading executions"]);
  await app.goto("/jobs");
  await expect(app.page.getByRole("heading", { name: "Executions" })).toBeVisible();
  await expect(app.page.getByText("Loading executions")).toBeVisible();

  await app.waitForCondition("execution list hydration", 15_000, async () => {
    return (await executionLink.isVisible().catch(() => false)) ? true : false;
  });

  await app.assertStableRoute({
    ready: app.page.getByRole("heading", { name: "Executions" }),
    expectedText: ["Executions", executionId.slice(0, 8)],
  });
}

export async function assertExecutionDetailHydratesLogs(
  app: SandboxHarness,
  executionId: string,
  logMarker: string,
): Promise<void> {
  const executionPrefix = executionId.slice(0, 8);
  await app.expectSSRHTML(`/jobs/${executionId}`, [executionPrefix, "Loading logs"]);
  await app.goto(`/jobs/${executionId}`);
  await expect(app.page.getByRole("heading", { name: executionPrefix })).toBeVisible();
  await expect(app.page.getByText("Loading logs")).toBeVisible();

  await waitForExecutionSuccess(app, executionId, logMarker);

  const mainText = await app.readText(app.page.locator("main"));
  expect(mainText).toContain(executionPrefix);
  expect(mainText).toContain("succeeded");
  expect(mainText).toContain(logMarker);
}

export async function waitForExecutionSuccess(
  app: SandboxHarness,
  executionId: string,
  logMarker: string,
): Promise<void> {
  const executionPrefix = executionId.slice(0, 8);
  await app.waitForCondition("execution success", 180_000, async () => {
    const headingVisible = await app.page
      .getByRole("heading", { name: executionPrefix })
      .isVisible()
      .catch(() => false);
    const statusVisible = await app.page.getByText("succeeded", { exact: true }).isVisible().catch(() => false);
    const logText = await app.page.locator("pre").innerText().catch(() => "");
    if (headingVisible && statusVisible && logText.includes(logMarker)) {
      return true;
    }
    return false;
  });
}

export async function waitForRepoReady(app: SandboxHarness): Promise<{
  bootstrap_execution_id: string;
  bootstrap_generation_id: string;
  bootstrap_source_sha: string;
}> {
  await app.waitForCondition("repo bootstrap", 180_000, async () => {
    const state = await readRepoState(app);
    const executionLinkVisible = await app.page
      .getByRole("link", { name: "View bootstrap execution" })
      .isVisible()
      .catch(() => false);

    return state === "ready" && executionLinkVisible;
  });

  const executionHref = await app
    .page
    .getByRole("link", { name: "View bootstrap execution" })
    .getAttribute("href");

  return {
    bootstrap_execution_id: executionHref ? requireRouteID(executionHref, "/jobs/") : "",
    bootstrap_generation_id: await readInfoRowValue(app, "Generation"),
    bootstrap_source_sha: await readInfoRowValue(app, "Source SHA"),
  };
}

export function requireRouteID(urlOrPath: string, prefix: string): string {
  const match = urlOrPath.match(new RegExp(`${prefix}([0-9a-f-]+)`));
  if (!match?.[1]) {
    throw new Error(`Could not extract route id from ${urlOrPath}`);
  }

  return match[1];
}

async function readInfoCardValue(app: SandboxHarness, label: string): Promise<string> {
  const labelNode = app.page.locator(`xpath=//div[normalize-space(text())='${label}']`).first();
  const valueNode = labelNode.locator("xpath=following-sibling::div[1]");
  await expect(valueNode).toBeVisible();
  return (await valueNode.textContent())?.trim() ?? "";
}

async function readInfoRowValue(app: SandboxHarness, label: string): Promise<string> {
  const labelNode = app.page.locator("span", { hasText: label }).first();
  const valueNode = labelNode.locator("xpath=following-sibling::span[1]");
  await expect(valueNode).toBeVisible();
  return (await valueNode.textContent())?.trim() ?? "";
}

async function readRepoState(app: SandboxHarness): Promise<string> {
  const heading = app.page.getByRole("heading", { level: 1 }).first();
  const stateNode = heading.locator("xpath=following-sibling::*[1]");
  await expect(stateNode).toBeVisible();
  return (await stateNode.textContent())?.trim() ?? "";
}
