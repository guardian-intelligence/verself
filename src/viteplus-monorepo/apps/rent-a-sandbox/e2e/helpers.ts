import { execFile as execFileCallback } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { expect, type BrowserContext, type Page } from "@playwright/test";
import { env } from "./env";

const execFile = promisify(execFileCallback);
export const verificationRunHeader = "X-Forge-Metal-Verification-Run";
const routeBaseURL = normalizeBaseURL(env.baseURL);
const authOrLoginURL = createURLPattern(env.zitadelBaseURL, ["/ui/login"]);
const appURL = createURLPattern(env.baseURL);
const postPasswordURL = createURLPattern(env.baseURL, ["/ui/mfa"]);

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
  const balanceCard = page.getByTestId("balance-card");
  const loginNameInput = page.locator("#loginName");
  const loginRedirectButton = page.getByRole("button", { name: "click here" });

  await page.goto("/");
  await page.waitForLoadState("networkidle");
  if (await balanceCard.isVisible().catch(() => false)) {
    return;
  }

  await page.goto("/login");
  await Promise.race([
    balanceCard.waitFor({ state: "visible", timeout: 15_000 }),
    loginNameInput.waitFor({ state: "visible", timeout: 15_000 }),
    loginRedirectButton.waitFor({ state: "visible", timeout: 15_000 }),
  ]);
  if (await balanceCard.isVisible().catch(() => false)) {
    return;
  }
  if (await loginRedirectButton.isVisible().catch(() => false)) {
    await Promise.all([
      page.waitForURL(authOrLoginURL, { timeout: 30_000 }),
      loginRedirectButton.click(),
    ]);
  }

  await loginNameInput.waitFor({ state: "visible", timeout: 30_000 });
  await loginNameInput.fill(env.testEmail);
  await page.locator("button[type='submit']").click();

  const passwordInput = page.locator("#password");
  await passwordInput.waitFor({ state: "visible", timeout: 10_000 });
  await passwordInput.fill(env.testPassword);
  await Promise.all([
    page.waitForURL(postPasswordURL, { timeout: 30_000 }),
    page.locator("button[type='submit']").click(),
  ]);
  const skipButton = page.getByRole("button", { name: /^Skip$/ });
  if (await skipButton.isVisible().catch(() => false)) {
    await Promise.all([
      page.waitForURL(appURL, { timeout: 30_000 }),
      skipButton.click(),
    ]);
  }

  await page.waitForLoadState("networkidle");
  await expect(balanceCard).toBeVisible({ timeout: 15_000 });
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

function createURLPattern(baseURL: string, extraPathPatterns: string[] = []): RegExp {
  const normalizedBaseURL = normalizeBaseURL(baseURL);
  const basePattern = escapeRegex(normalizedBaseURL);
  if (extraPathPatterns.length === 0) {
    return new RegExp(`^${basePattern}(?:[/?#].*)?$`);
  }

  const pathPatterns = extraPathPatterns.map(
    (pathPattern) => `${basePattern}${escapeRegex(pathPattern)}(?:[?#].*)?`,
  );
  return new RegExp(`^(?:${pathPatterns.join("|")})$`);
}

function normalizeBaseURL(baseURL: string): string {
  return new URL(baseURL).href.replace(/\/$/, "");
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
