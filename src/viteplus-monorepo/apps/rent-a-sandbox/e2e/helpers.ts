import { expect, type Page } from "@playwright/test";
import { env } from "./env";

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
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  const dashboardHeading = page.getByRole("heading", { name: "Dashboard" });
  const signInButton = page.getByRole("button", { name: "Sign in" });

  await Promise.race([
    dashboardHeading.waitFor({ state: "visible", timeout: 10_000 }),
    signInButton.waitFor({ state: "visible", timeout: 10_000 }),
  ]);

  if (await dashboardHeading.isVisible()) return;

  // Arm the cross-origin wait before the click so Playwright does not miss the
  // OIDC redirect when the browser navigates away quickly.
  await Promise.all([
    page.waitForURL(/auth\.anveio\.com|\/ui\/login/, { timeout: 30_000 }),
    signInButton.click(),
  ]);

  const loginNameInput = page.locator("#loginName");
  await loginNameInput.waitFor({ state: "visible", timeout: 10_000 });
  await loginNameInput.fill(env.testEmail);
  await page.locator("button[type='submit']").click();

  const passwordInput = page.locator("#password");
  await passwordInput.waitFor({ state: "visible", timeout: 10_000 });
  await passwordInput.fill(env.testPassword);
  await Promise.all([
    page.waitForURL(/rentasandbox\./, { timeout: 30_000 }),
    page.locator("button[type='submit']").click(),
  ]);

  await page.waitForLoadState("networkidle");
  await expect(dashboardHeading).toBeVisible({ timeout: 15_000 });
}

export async function readBalance(page: Page): Promise<number> {
  const balanceText = page
    .locator("div")
    .filter({ hasText: /^Available Credits$/ })
    .locator("..")
    .locator(".text-4xl");

  await balanceText.waitFor({ state: "visible", timeout: 10_000 });
  const raw = await balanceText.textContent();
  if (!raw) throw new Error("Could not read balance text");
  return parseInt(raw.replace(/[^0-9-]/g, ""), 10);
}

export async function installVerificationOverlay(page: Page, verificationRunID: string): Promise<void> {
  await page.addInitScript(({ runID }) => {
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
  }, { runID: verificationRunID });
}
