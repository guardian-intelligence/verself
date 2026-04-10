import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { expect, type BrowserContext, type Page } from "@playwright/test";
import { env } from "./env";

const execFile = promisify(execFileCallback);
export const verificationRunHeader = "X-Forge-Metal-Verification-Run";
const routeBaseURL = normalizeBaseURL(env.baseURL);
const shortTimeoutMS = 5_000;
const pollIntervalMS = 250;

export async function ensureTestUserExists(): Promise<void> {
  if (!env.zitadelAdminPAT) return;

  const headers = {
    Authorization: `Bearer ${env.zitadelAdminPAT}`,
    "Content-Type": "application/json",
  };

  const searchResp = await fetch(`${env.zitadelBaseURL}/v2/users`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      queries: [{ emailQuery: { emailAddress: env.testEmail } }],
    }),
  });
  const searchBody = await searchResp.json();
  const existing = searchBody?.result?.[0];
  if (existing?.userId) return;

  const createResp = await fetch(`${env.zitadelBaseURL}/v2/users/human`, {
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
  if (!createResp.ok) {
    const err = await createResp.text();
    throw new Error(`Failed to create test user: ${createResp.status} ${err}`);
  }
  const created = await createResp.json();
  const userId = created.userId;

  const projResp = await fetch(`${env.zitadelBaseURL}/management/v1/projects/_search`, {
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
  const projBody = await projResp.json();
  const projectId = projBody?.result?.[0]?.id;
  if (!projectId) {
    console.warn(
      `Could not find Zitadel project "${env.zitadelProjectName}" — skipping project grant`,
    );
    return;
  }

  await fetch(`${env.zitadelBaseURL}/management/v1/users/${userId}/grants`, {
    method: "POST",
    headers,
    body: JSON.stringify({ projectId, roleKeys: [] }),
  });
}

export async function ensureLoggedIn(page: Page): Promise<void> {
  const loginNameInput = page.locator("#loginName");
  const passwordInput = page.locator("#password");
  const loginRedirectButton = page.getByRole("button", { name: /click here/i });
  const otherUserButton = page.getByRole("button", { name: /other user/i });
  const skipButton = page.getByRole("button", { name: /^Skip$/ });
  const dashboardHeading = page.getByRole("heading", { name: "Dashboard" });
  const signOutLink = page.getByRole("link", { name: "Sign out" });
  const balanceCard = page.getByTestId("balance-card");

  await page.goto("/");
  await page.waitForLoadState("domcontentloaded");
  if (await isDashboardReady({ dashboardHeading, balanceCard, signOutLink })) {
    return;
  }

  await page.goto("/login");
  await page.waitForLoadState("domcontentloaded");
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (await isDashboardReady({ dashboardHeading, balanceCard, signOutLink })) {
      return;
    }

    if (await loginRedirectButton.isVisible().catch(() => false)) {
      await loginRedirectButton.click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await otherUserButton.isVisible().catch(() => false)) {
      await otherUserButton.click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await loginNameInput.isVisible().catch(() => false)) {
      await loginNameInput.fill(env.testEmail);
      await page.locator("button[type='submit']").click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await passwordInput.isVisible().catch(() => false)) {
      await passwordInput.fill(env.testPassword);
      await page.locator("button[type='submit']").click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await skipButton.isVisible().catch(() => false)) {
      await skipButton.click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    await page.waitForTimeout(pollIntervalMS);
  }

  await expect(balanceCard).toBeVisible({ timeout: shortTimeoutMS });
}

export async function readBalance(page: Page): Promise<number> {
  const balanceText = page.getByTestId("balance-total");

  await balanceText.waitFor({ state: "visible", timeout: 10_000 });
  const raw = await balanceText.textContent();
  if (!raw) throw new Error("Could not read balance text");
  return parseInt(raw.replace(/[^0-9-]/g, ""), 10);
}

export async function installVerificationOverlay(
  page: Page,
  verificationRunID: string,
): Promise<void> {
  await page.addInitScript(
    ({ runID }) => {
      const overlayId = "__forge_metal_verification_overlay";

      function renderOverlay() {
        let el = document.getElementById(overlayId);
        if (!el) {
          el = document.createElement("div");
          el.id = overlayId;
          Object.assign(el.style, {
            position: "fixed",
            right: "12px",
            bottom: "12px",
            zIndex: "2147483647",
            padding: "8px 10px",
            borderRadius: "8px",
            background: "rgba(0, 0, 0, 0.82)",
            color: "#fff",
            font: "12px/1.4 Menlo, Monaco, monospace",
            pointerEvents: "none",
            whiteSpace: "pre",
            boxShadow: "0 8px 24px rgba(0,0,0,0.2)",
          });
          document.documentElement.appendChild(el);
        }
        el.textContent = `forge-metal verification\n${runID}\n${new Date().toISOString()}`;
      }

      const install = () => {
        renderOverlay();
        window.setInterval(renderOverlay, 1000);
      };

      if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", install, { once: true });
        return;
      }

      install();
    },
    { runID: verificationRunID },
  );
}

export async function installVerificationHeader(
  context: BrowserContext,
  verificationRunID: string,
): Promise<void> {
  await context.route(`${routeBaseURL}/**`, async (route) => {
    await route.continue({
      headers: {
        ...route.request().headers(),
        [verificationRunHeader]: verificationRunID,
      },
    });
  });
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

export async function pushVerificationRepoRevision(
  revision: string,
): Promise<VerificationRepoMeta> {
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

function normalizeBaseURL(baseURL: string): string {
  return new URL(baseURL).href.replace(/\/$/, "");
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

async function isDashboardReady({
  dashboardHeading,
  balanceCard,
  signOutLink,
}: {
  dashboardHeading: ReturnType<Page["getByRole"]>;
  balanceCard: ReturnType<Page["getByTestId"]>;
  signOutLink: ReturnType<Page["getByRole"]>;
}): Promise<boolean> {
  const [dashboardVisible, balanceVisible, signOutVisible] = await Promise.all([
    dashboardHeading.isVisible().catch(() => false),
    balanceCard.isVisible().catch(() => false),
    signOutLink.isVisible().catch(() => false),
  ]);
  return dashboardVisible && balanceVisible && signOutVisible;
}

async function waitForAuthBoundary(
  page: Page,
  {
    dashboardHeading,
    loginNameInput,
    passwordInput,
    loginRedirectButton,
    otherUserButton,
    skipButton,
  }: {
    dashboardHeading: ReturnType<Page["getByRole"]>;
    loginNameInput: ReturnType<Page["locator"]>;
    passwordInput: ReturnType<Page["locator"]>;
    loginRedirectButton: ReturnType<Page["getByRole"]>;
    otherUserButton: ReturnType<Page["getByRole"]>;
    skipButton: ReturnType<Page["getByRole"]>;
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
