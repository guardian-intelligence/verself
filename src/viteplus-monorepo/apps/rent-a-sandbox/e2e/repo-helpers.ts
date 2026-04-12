import { expect } from "./harness";
import type { SandboxHarness, VerificationRepoMeta } from "./harness";

const repoRedirectTimeoutMS = 30_000;
const repoScanTimeoutMS = 180_000;
const executionHydrationTimeoutMS = 15_000;
const executionTimeoutMS = 180_000;

export async function importRepoFromURL(
  app: SandboxHarness,
  repoURL: string,
  defaultBranch = "main",
): Promise<{
  import_scanned_sha: string;
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

  const repoId = await app.waitForCondition(
    "repo import redirect",
    repoRedirectTimeoutMS,
    async () => {
      const match = app.page.url().match(/\/repos\/([0-9a-f-]+)$/);
      return match?.[1] ?? false;
    },
  );

  const repoHeading = app.page.getByRole("heading", { level: 1 }).first();
  await app.waitForCondition("repo heading stabilize", repoRedirectTimeoutMS, async () => {
    const heading = await app.readText(repoHeading);
    return heading !== "Import Repo" && heading.length > 0 ? heading : false;
  });
  const ready = await waitForRepoReady(app);
  const repoName = await app.readText(repoHeading);

  return {
    import_scanned_sha: ready.last_scanned_sha,
    repo_id: repoId,
    repo_name: repoName,
  };
}

export async function assertRepoDetailSSRStable(
  app: SandboxHarness,
  repoId: string,
  repoName: string,
  expectedScannedSHA: string,
): Promise<void> {
  const shaPrefix = expectedScannedSHA.slice(0, 12);
  await app.expectSSRHTML(`/repos/${repoId}`, [repoName, "Repo Contract", shaPrefix]);
  await app.assertStableRoute({
    path: `/repos/${repoId}`,
    ready: app.page.getByRole("heading", { name: repoName }),
    expectedText: [repoName, "Last scanned SHA", "Repository Scan", "Repo Contract", shaPrefix],
  });
  await expect(app.page.getByText("ready", { exact: true }).first()).toBeVisible();
}

export async function rescanRepoMetadata(
  app: SandboxHarness,
  repoId: string,
  refreshedRepoMeta: VerificationRepoMeta,
): Promise<{
  rescan_scanned_sha: string;
}> {
  await app.goto(`/repos/${repoId}`);
  const repoHeading = app.page.getByRole("heading", { level: 1 }).first();
  await expect(repoHeading).toBeVisible();

  const refreshedSha = refreshedRepoMeta.commit_sha.slice(0, 12);

  await app.page.getByRole("button", { name: "Rescan" }).click();
  await app.waitForCondition("repo rescan", repoScanTimeoutMS, async () => {
    const scannedSHA = await readInfoCardValue(app, "Last scanned SHA");
    const state = await readRepoState(app);
    return scannedSHA === refreshedSha && state === "ready";
  });

  return {
    rescan_scanned_sha: refreshedRepoMeta.commit_sha,
  };
}

export async function launchDirectExecution(
  app: SandboxHarness,
  runCommand: string,
): Promise<{
  execution_id: string;
}> {
  await app.expectSSRHTML("/jobs/new", ["Manual Execution", "Run command"]);
  await app.assertStableRoute({
    path: "/jobs/new",
    ready: app.page.getByRole("heading", { name: "Manual Execution" }),
    expectedText: ["Manual Execution", "Run command"],
  });

  await app.page.getByLabel("Run command").fill(runCommand);
  await app.page.getByRole("button", { name: "Submit Execution" }).click();
  const executionId = await app.waitForCondition(
    "execution launch redirect",
    repoRedirectTimeoutMS,
    async () => {
      const match = app.page.url().match(/\/jobs\/([0-9a-f-]+)$/);
      return match?.[1] ?? false;
    },
  );

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
  await expect(app.page.getByRole("heading", { name: "Executions", exact: true })).toBeVisible();
  await expect(app.page.getByText("Loading executions")).toBeVisible();

  await app.waitForCondition("execution list hydration", executionHydrationTimeoutMS, async () => {
    return (await executionLink.isVisible().catch(() => false)) ? true : false;
  });

  await app.assertStableRoute({
    ready: app.page.getByRole("heading", { name: "Executions", exact: true }),
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
  await app.waitForCondition("execution success", executionTimeoutMS, async () => {
    const headingVisible = await app.page
      .getByRole("heading", { name: executionPrefix })
      .isVisible()
      .catch(() => false);
    const statusVisible = await app.page
      .getByText("succeeded", { exact: true })
      .isVisible()
      .catch(() => false);
    const logText = await app.page
      .locator("pre")
      .innerText()
      .catch(() => "");
    if (headingVisible && statusVisible && logText.includes(logMarker)) {
      return true;
    }
    return false;
  });
}

async function waitForRepoReady(app: SandboxHarness): Promise<{
  last_scanned_sha: string;
}> {
  await app.waitForCondition("repo metadata scan", repoScanTimeoutMS, async () => {
    const state = await readRepoState(app);
    const scannedSHA = await readInfoCardValue(app, "Last scanned SHA");
    return state === "ready" && scannedSHA !== "--" ? true : false;
  });

  return {
    last_scanned_sha: await readInfoCardValue(app, "Last scanned SHA"),
  };
}

async function readInfoCardValue(app: SandboxHarness, label: string): Promise<string> {
  const labelNode = app.page.locator(`xpath=//div[normalize-space(text())='${label}']`).first();
  const valueNode = labelNode.locator("xpath=following-sibling::div[1]");
  await expect(valueNode).toBeVisible();
  return (await valueNode.textContent())?.trim() ?? "";
}

async function readRepoState(app: SandboxHarness): Promise<string> {
  const heading = app.page.getByRole("heading", { level: 1 }).first();
  const stateNode = heading.locator("xpath=following-sibling::*[1]");
  await expect(stateNode).toBeVisible();
  return (await stateNode.textContent())?.trim() ?? "";
}
