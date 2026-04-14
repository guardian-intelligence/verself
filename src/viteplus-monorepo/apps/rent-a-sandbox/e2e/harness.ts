import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import {
  expect,
  test as base,
  type BrowserContext,
  type ConsoleMessage,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";
import { env } from "./env";
import {
  createVerificationRun,
  persistVerificationRun,
  type VerificationRun,
} from "./verification-run";

const execFile = promisify(execFileCallback);

export const verificationRunHeader = "X-Forge-Metal-Verification-Run";
export const shortTimeoutMS = 5_000;
export const pollIntervalMS = 250;

const routeBaseURL = normalizeBaseURL(env.baseURL);
const allowedWarningPatterns = [
  // The edge stack currently emits both legacy and modern policy headers.
  /Feature-Policy header: Some features are specified in both Feature-Policy and Permissions-Policy header: payment/i,
];

type ConsoleEntry = {
  readonly locationUrl: string;
  readonly text: string;
  readonly type: string;
};

type FailedRequest = {
  readonly failure: string;
  readonly resourceType: string;
  readonly url: string;
};

class BrowserMonitor {
  readonly consoleMessages: ConsoleEntry[] = [];
  readonly failedRequests: FailedRequest[] = [];
  readonly pageErrors: string[] = [];

  constructor(page: Page) {
    page.on("console", (message) => {
      const location = message.location();
      this.consoleMessages.push({
        locationUrl: location.url ?? "",
        text: message.text(),
        type: message.type(),
      });
    });
    page.on("pageerror", (error) => {
      this.pageErrors.push(`[${page.url()}] ${error.stack || String(error)}`);
    });
    page.on("requestfailed", (request) => {
      this.failedRequests.push({
        failure: request.failure()?.errorText || "unknown",
        resourceType: request.resourceType(),
        url: request.url(),
      });
    });
  }

  reset(): void {
    this.consoleMessages.length = 0;
    this.failedRequests.length = 0;
    this.pageErrors.length = 0;
  }

  async assertHealthy(): Promise<void> {
    const unexpectedFailures = this.failedRequests.filter((request) => {
      if (!request.url.startsWith(routeBaseURL)) {
        return false;
      }

      if (request.failure !== "net::ERR_ABORTED") {
        return true;
      }

      if (request.url.includes("/v1/shape") || request.url.includes("/_serverFn/")) {
        return false;
      }

      return !["font", "image", "media", "script", "stylesheet"].includes(request.resourceType);
    });

    const unexpectedConsoleMessages = this.consoleMessages.filter((message) => {
      if (message.locationUrl && !message.locationUrl.startsWith(routeBaseURL)) {
        return false;
      }

      if (allowedWarningPatterns.some((pattern) => pattern.test(message.text))) {
        return false;
      }

      return message.type === "error" || message.type === "warning";
    });

    if (
      this.pageErrors.length === 0 &&
      unexpectedFailures.length === 0 &&
      unexpectedConsoleMessages.length === 0
    ) {
      return;
    }

    throw new Error(
      [
        this.pageErrors[0],
        unexpectedFailures[0]
          ? `${unexpectedFailures[0].failure} ${unexpectedFailures[0].url}`
          : "",
        unexpectedConsoleMessages[0]
          ? `${unexpectedConsoleMessages[0].type}: ${unexpectedConsoleMessages[0].text}`
          : "",
      ]
        .filter(Boolean)
        .join(" | "),
    );
  }
}

export interface VerificationRepoMeta {
  owner: string;
  repo_name: string;
  public_base_url: string;
  loopback_repo_url: string;
  browse_url: string;
  ref: string;
  commit_sha: string;
}

export interface BillingFixtureState {
  org_id: string;
  org_name: string;
  email: string;
  product_id: string;
  state: string;
  plan_id: string;
  business_now: string;
  balance_units?: number;
  totals_by_source: Record<string, number>;
  contracts: number;
}

export interface BillingClockState {
  org_id: string;
  product_id: string;
  scope_kind: string;
  scope_id: string;
  business_now: string;
  has_override: boolean;
  generation: number;
  cycles_rolled_over: number;
  contract_changes_applied: number;
  entitlements_ensured: number;
}

export interface BillingDocumentEvidence {
  document_id: string;
  document_number: string;
  document_kind: string;
  finalization_id: string;
  cycle_id: string;
  status: string;
  payment_status: string;
  period_start: string;
  period_end: string;
  total_due_units: number;
}

export class SandboxHarness {
  readonly monitor: BrowserMonitor;
  readonly runID: string;
  readonly runJSONPath: string;

  constructor(
    readonly page: Page,
    readonly context: BrowserContext,
    readonly testInfo: TestInfo,
  ) {
    this.monitor = new BrowserMonitor(page);
    this.runID =
      env.verificationRunID.trim() ||
      `${slugify(testInfo.titlePath.join("-"))}-${Date.now().toString(36)}`;
    this.runJSONPath = env.verificationRunJSONPath || testInfo.outputPath("verification-run.json");
  }

  createRun(): VerificationRun {
    return createVerificationRun(this.runID);
  }

  resetBrowserSignals(): void {
    this.monitor.reset();
  }

  async assertHealthy(): Promise<void> {
    await this.monitor.assertHealthy();
  }

  async installVerificationHeader(): Promise<void> {
    await this.context.route(`${routeBaseURL}/**`, async (route) => {
      await route.continue({
        headers: {
          ...route.request().headers(),
          [verificationRunHeader]: this.runID,
        },
      });
    });
  }

  async persistRun(run: VerificationRun): Promise<void> {
    await persistVerificationRun(this.runJSONPath, run);
  }

  async goto(pathname: string): Promise<void> {
    await this.page.goto(pathname, { waitUntil: "domcontentloaded" });
  }

  async expectSSRHTML(pathname: string, expectedFragments: string[]): Promise<string> {
    const html = await this.fetchHTML(pathname);
    for (const fragment of expectedFragments) {
      expect(html).toContain(fragment);
    }
    return html;
  }

  async fetchHTML(pathname: string): Promise<string> {
    const url = new URL(pathname, env.baseURL).toString();
    const cookies = await this.context.cookies(url);
    const cookieHeader = cookies.map((cookie) => `${cookie.name}=${cookie.value}`).join("; ");
    const response = await fetch(url, {
      headers: {
        ...(cookieHeader ? { Cookie: cookieHeader } : {}),
        [verificationRunHeader]: this.runID,
      },
    });

    if (!response.ok) {
      throw new Error(`SSR fetch failed for ${pathname}: ${response.status}`);
    }

    return await response.text();
  }

  async assertStableRoute({
    path,
    ready,
    stableContent,
    expectedText,
    exactText = false,
  }: {
    path?: string;
    ready: Locator;
    stableContent?: Locator;
    expectedText: string[];
    exactText?: boolean;
  }): Promise<{ after: string; before: string }> {
    if (path) {
      await this.goto(path);
    }

    const content = stableContent ?? this.page.locator("main");
    await expect(ready).toBeVisible({ timeout: shortTimeoutMS });
    const before = await this.readText(content);

    for (const fragment of expectedText) {
      expect(before).toContain(fragment);
    }

    for (let attempt = 0; attempt < 4; attempt += 1) {
      await this.page.waitForTimeout(pollIntervalMS);
      await expect(ready).toBeVisible({ timeout: shortTimeoutMS });
    }

    const after = await this.readText(content);
    for (const fragment of expectedText) {
      expect(after).toContain(fragment);
    }

    if (exactText) {
      expect(after).toBe(before);
    }

    return { after, before };
  }

  async waitForCondition<T>(
    label: string,
    timeoutMs: number,
    predicate: () => Promise<T | false | null | undefined>,
  ): Promise<T> {
    const deadline = Date.now() + timeoutMs;
    let lastError: unknown;

    while (Date.now() < deadline) {
      try {
        const result = await predicate();
        if (result !== false && result !== null && result !== undefined) {
          return result;
        }
      } catch (error) {
        lastError = error;
      }

      await this.page.waitForTimeout(pollIntervalMS);
    }

    throw new Error(
      `${label} did not complete within ${timeoutMs}ms${lastError ? `: ${formatError(lastError)}` : ""}`,
    );
  }

  async ensureLoggedIn(): Promise<void> {
    const loginNameInput = this.page.locator("#loginName");
    const passwordInput = this.page.locator("#password");
    const loginRedirectButton = this.page.getByRole("button", { name: /click here/i });
    const otherUserButton = this.page.getByRole("button", { name: /other user/i });
    const skipButton = this.page.getByRole("button", { name: /^Skip$/ });
    const dashboardHeading = this.page.getByRole("heading", { name: "Dashboard" });
    const signOutLink = this.page.getByRole("link", { name: "Sign out" });

    await this.goto("/");
    if (await isDashboardReady({ dashboardHeading, signOutLink })) {
      return;
    }

    await this.goto("/login");
    await this.waitForCondition("login flow", 30_000, async () => {
      if (await isDashboardReady({ dashboardHeading, signOutLink })) {
        return true;
      }

      if (await loginRedirectButton.isVisible().catch(() => false)) {
        await loginRedirectButton.click();
        await waitForAuthBoundary(this.page, {
          dashboardHeading,
          loginNameInput,
          loginRedirectButton,
          otherUserButton,
          passwordInput,
          skipButton,
        });
        return false;
      }

      if (await otherUserButton.isVisible().catch(() => false)) {
        await otherUserButton.click();
        await waitForAuthBoundary(this.page, {
          dashboardHeading,
          loginNameInput,
          loginRedirectButton,
          otherUserButton,
          passwordInput,
          skipButton,
        });
        return false;
      }

      if (await loginNameInput.isVisible().catch(() => false)) {
        await loginNameInput.fill(env.testEmail);
        await this.page.locator("button[type='submit']").click();
        await waitForAuthBoundary(this.page, {
          dashboardHeading,
          loginNameInput,
          loginRedirectButton,
          otherUserButton,
          passwordInput,
          skipButton,
        });
        return false;
      }

      if (await passwordInput.isVisible().catch(() => false)) {
        await passwordInput.fill(env.testPassword);
        await this.page.locator("button[type='submit']").click();
        await waitForAuthBoundary(this.page, {
          dashboardHeading,
          loginNameInput,
          loginRedirectButton,
          otherUserButton,
          passwordInput,
          skipButton,
        });
        return false;
      }

      if (await skipButton.isVisible().catch(() => false)) {
        await skipButton.click();
        await waitForAuthBoundary(this.page, {
          dashboardHeading,
          loginNameInput,
          loginRedirectButton,
          otherUserButton,
          passwordInput,
          skipButton,
        });
      }

      return false;
    });

    await expect(signOutLink).toBeVisible({ timeout: shortTimeoutMS });
  }

  async readBalance(): Promise<number> {
    // Sums the entitlements view's hidden test attribute. The visible UI never
    // shows a top-line balance — this is a relative-comparison anchor for e2e
    // (started vs finished) only.
    if (!this.page.url().includes("/billing")) {
      await this.goto("/billing");
    }
    const view = this.page.getByTestId("entitlements-view");
    await view.first().waitFor({ state: "visible", timeout: shortTimeoutMS });
    const raw = await view.first().getAttribute("data-test-available-units");
    if (raw === null) {
      throw new Error("entitlements view missing data-test-available-units");
    }
    const units = Number.parseInt(raw, 10);
    if (!Number.isFinite(units)) {
      throw new Error(`entitlements view test-available-units is not numeric: ${raw}`);
    }
    return units;
  }

  async readText(locator: Locator): Promise<string> {
    return normalizeText(await locator.innerText().catch(() => ""));
  }

  async pushVerificationRepoRevision(revision: string): Promise<VerificationRepoMeta> {
    const scriptPath = fileURLToPath(
      new URL("../../../../platform/scripts/ensure-verification-repo.sh", import.meta.url),
    );
    const { stdout } = await execFile(scriptPath, [], {
      env: {
        ...process.env,
        VERIFICATION_REPO_REVISION: revision,
      },
    });
    return JSON.parse(stdout) as VerificationRepoMeta;
  }

  async setBillingUserState({
    balanceCents,
    businessNow,
    org = "platform",
    orgID,
    planID,
    productID = "sandbox",
    state = "free",
  }: {
    balanceCents?: number;
    businessNow?: string;
    org?: string;
    orgID?: number | string;
    planID?: string;
    productID?: string;
    state?: string;
  }): Promise<BillingFixtureState> {
    const scriptPath = fileURLToPath(
      new URL("../../../../platform/scripts/set-user-state.sh", import.meta.url),
    );
    const args = ["--email", env.testEmail, "--product-id", productID, "--state", state];
    if (orgID !== undefined) {
      args.push("--org-id", String(orgID));
    } else {
      args.push("--org", org);
    }
    if (planID) {
      args.push("--plan-id", planID);
    }
    if (balanceCents !== undefined) {
      args.push("--balance-cents", String(balanceCents));
    }
    if (businessNow) {
      args.push("--business-now", businessNow);
    }
    const { stdout } = await execFile(scriptPath, args, { env: process.env });
    return JSON.parse(stdout) as BillingFixtureState;
  }

  async setBillingClock({
    advanceSeconds,
    clear = false,
    orgID,
    productID = "sandbox",
    reason = this.runID,
    set,
  }: {
    advanceSeconds?: number;
    clear?: boolean;
    orgID: number | string;
    productID?: string;
    reason?: string;
    set?: string;
  }): Promise<BillingClockState> {
    const scriptPath = fileURLToPath(
      new URL("../../../../platform/scripts/billing-clock.sh", import.meta.url),
    );
    const args = ["--org-id", String(orgID), "--product-id", productID, "--reason", reason];
    if (set) {
      args.push("--set", set);
    } else if (advanceSeconds !== undefined) {
      args.push("--advance-seconds", String(advanceSeconds));
    } else if (clear) {
      args.push("--clear");
    }
    const { stdout } = await execFile(scriptPath, args, { env: process.env });
    return JSON.parse(stdout) as BillingClockState;
  }

  async readBillingDocuments({
    orgID,
    productID = "sandbox",
  }: {
    orgID: number | string;
    productID?: string;
  }): Promise<BillingDocumentEvidence[]> {
    const platformDir = fileURLToPath(new URL("../../../../platform/", import.meta.url));
    const scriptPath = fileURLToPath(
      new URL("scripts/pg.sh", new URL("../../../../platform/", import.meta.url)),
    );
    const orgIDText = String(orgID).replaceAll("'", "''");
    const productIDText = productID.replaceAll("'", "''");
    const sql = `
      SELECT COALESCE(json_agg(json_build_object(
        'document_id', document_id,
        'document_number', COALESCE(document_number, ''),
        'document_kind', document_kind,
        'finalization_id', COALESCE(finalization_id, ''),
        'cycle_id', COALESCE(cycle_id, ''),
        'status', status,
        'payment_status', payment_status,
        'period_start', to_char(period_start AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
        'period_end', to_char(period_end AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
        'total_due_units', total_due_units
      ) ORDER BY period_start DESC, document_id DESC), '[]'::json)::text
      FROM billing_documents
      WHERE org_id = '${orgIDText}'
        AND product_id = '${productIDText}'
        AND status <> 'voided';
    `;
    const { stdout } = await execFile(
      scriptPath,
      ["billing", "--no-align", "--tuples-only", "--quiet", "--query", sql],
      { cwd: platformDir, env: process.env },
    );
    return JSON.parse(stdout.trim() || "[]") as BillingDocumentEvidence[];
  }
}

export const test = base.extend<{ app: SandboxHarness }>({
  app: async ({ context, page }, use, testInfo) => {
    const app = new SandboxHarness(page, context, testInfo);
    await app.installVerificationHeader();

    await use(app);
    await app.assertHealthy();
  },
});

export { expect };

export async function ensureTestUserExists(): Promise<void> {
  if (!env.zitadelAdminPAT) {
    return;
  }

  const headers = {
    Authorization: `Bearer ${env.zitadelAdminPAT}`,
    "Content-Type": "application/json",
  };

  const searchResponse = await fetch(`${env.zitadelBaseURL}/v2/users`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      queries: [{ emailQuery: { emailAddress: env.testEmail } }],
    }),
  });
  const searchBody = await searchResponse.json();
  const existingUser = searchBody?.result?.[0];
  if (existingUser?.userId) {
    return;
  }

  const createResponse = await fetch(`${env.zitadelBaseURL}/v2/users/human`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      username: env.testUsername,
      profile: {
        givenName: env.testFirstName,
        familyName: env.testLastName,
      },
      email: { email: env.testEmail, isVerified: true },
      password: { password: env.testPassword, changeRequired: false },
    }),
  });
  if (!createResponse.ok) {
    throw new Error(
      `Failed to create test user: ${createResponse.status} ${await createResponse.text()}`,
    );
  }

  const createdUser = await createResponse.json();
  const userId = createdUser.userId;

  const projectResponse = await fetch(`${env.zitadelBaseURL}/management/v1/projects/_search`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      queries: [
        {
          nameQuery: {
            name: env.zitadelProjectName,
            method: "TEXT_QUERY_METHOD_EQUALS",
          },
        },
      ],
    }),
  });
  const projectBody = await projectResponse.json();
  const projectId = projectBody?.result?.[0]?.id;
  if (!projectId) {
    throw new Error(`Could not find Zitadel project "${env.zitadelProjectName}"`);
  }

  await fetch(`${env.zitadelBaseURL}/management/v1/users/${userId}/grants`, {
    method: "POST",
    headers,
    body: JSON.stringify({ projectId, roleKeys: [] }),
  });
}

function normalizeBaseURL(baseURL: string): string {
  return new URL(baseURL).href.replace(/\/$/, "");
}

function normalizeText(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/(^-|-$)/g, "");
}

function formatError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

async function isDashboardReady({
  dashboardHeading,
  signOutLink,
}: {
  dashboardHeading: Locator;
  signOutLink: Locator;
}): Promise<boolean> {
  const [dashboardVisible, signOutVisible] = await Promise.all([
    dashboardHeading.isVisible().catch(() => false),
    signOutLink.isVisible().catch(() => false),
  ]);

  return dashboardVisible && signOutVisible;
}

async function waitForAuthBoundary(
  page: Page,
  {
    dashboardHeading,
    loginNameInput,
    loginRedirectButton,
    otherUserButton,
    passwordInput,
    skipButton,
  }: {
    dashboardHeading: Locator;
    loginNameInput: Locator;
    loginRedirectButton: Locator;
    otherUserButton: Locator;
    passwordInput: Locator;
    skipButton: Locator;
  },
): Promise<void> {
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (
      (await dashboardHeading.isVisible().catch(() => false)) ||
      (await loginNameInput.isVisible().catch(() => false)) ||
      (await passwordInput.isVisible().catch(() => false)) ||
      (await loginRedirectButton.isVisible().catch(() => false)) ||
      (await otherUserButton.isVisible().catch(() => false)) ||
      (await skipButton.isVisible().catch(() => false))
    ) {
      return;
    }

    await page.waitForLoadState("domcontentloaded").catch(() => {});
    await page.waitForTimeout(pollIntervalMS);
  }
}
