import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { expect, type Page } from "@playwright/test";
import { env } from "./env";

const platformDir = fileURLToPath(new URL("../../../../platform/", import.meta.url));
const resendMailSendScript = fileURLToPath(
  new URL("../../../../platform/scripts/mail-send.sh", import.meta.url),
);
const shortTimeoutMS = 5_000;
const pollIntervalMS = 250;

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

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
}

export async function ensureLoggedIn(page: Page): Promise<void> {
  const mailboxNav = page.locator("aside nav");
  const inboxLink = mailboxNav.getByText("Inbox", { exact: true });
  const loginNameInput = page.locator("#loginName");
  const loginRedirectButton = page.getByRole("button", { name: /click here/i });
  const passwordInput = page.locator("#password");
  const otherUserButton = page.getByRole("button", { name: /other user/i });

  await page.goto("/");

  if (await mailboxNav.isVisible().catch(() => false)) {
    await expect(inboxLink).toBeVisible({ timeout: shortTimeoutMS });
    return;
  }

  await page.goto("/login");
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (await mailboxNav.isVisible().catch(() => false)) {
      await expect(inboxLink).toBeVisible({ timeout: shortTimeoutMS });
      return;
    }

    if (await loginRedirectButton.isVisible().catch(() => false)) {
      await loginRedirectButton.click();
      await waitForLoginBoundary(page, {
        mailboxNav,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
      });
      continue;
    }

    if (await otherUserButton.isVisible().catch(() => false)) {
      await otherUserButton.click();
      await waitForLoginBoundary(page, {
        mailboxNav,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
      });
      continue;
    }

    if (await loginNameInput.isVisible().catch(() => false)) {
      await loginNameInput.fill(env.testEmail);
      await page.locator("button[type='submit']").click();
      await waitForLoginBoundary(page, {
        mailboxNav,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
      });
      continue;
    }

    if (await passwordInput.isVisible().catch(() => false)) {
      await passwordInput.fill(env.testPassword);
      await page.locator("button[type='submit']").click();
      await waitForLoginBoundary(page, {
        mailboxNav,
        loginNameInput,
        passwordInput,
        loginRedirectButton,
        otherUserButton,
      });
      continue;
    }

    await page.waitForTimeout(pollIntervalMS);
  }

  throw new Error(`Unable to complete login flow from ${page.url()}`);
}

export async function openInbox(page: Page): Promise<void> {
  const inboxLink = page.locator("aside nav").getByText("Inbox", { exact: true });
  await expect(inboxLink).toBeVisible({ timeout: shortTimeoutMS });
  await inboxLink.click();
  await expect(page).toHaveURL(/\/mail\/[^/]+$/, { timeout: shortTimeoutMS });
}

export function sendMailToDemoInbox(subject: string, body: string): void {
  execFileSync(
    "bash",
    [resendMailSendScript, "--to", env.inboxLocalPart, "--subject", subject, "--body", body],
    {
      cwd: platformDir,
      stdio: "pipe",
      encoding: "utf8",
    },
  );
}

export async function waitForEmailSubject(page: Page, subject: string) {
  const row = page.locator("a").filter({ hasText: subject }).first();
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (await row.isVisible().catch(() => false)) {
      return row;
    }
    await page.waitForTimeout(pollIntervalMS);
  }
  throw new Error(`Email with subject "${subject}" did not appear within ${shortTimeoutMS}ms`);
}

export async function assertLoggedOut(page: Page): Promise<void> {
  const authEndSessionPrefix = `${new URL(env.zitadelBaseURL).origin}/oidc/v1/end_session`;
  const signInLink = page.getByRole("banner").getByRole("link", { name: "Sign in" });
  const endSessionRequest = page.waitForRequest(
    (request) => request.url().startsWith(authEndSessionPrefix),
    { timeout: shortTimeoutMS },
  );

  await page.getByRole("link", { name: "Sign out" }).click();
  await endSessionRequest;

  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (page.url().startsWith(env.baseURL) && (await signInLink.isVisible().catch(() => false))) {
      break;
    }
    await page.waitForTimeout(pollIntervalMS);
  }

  await expect(page).toHaveURL(new RegExp(`^${escapeRegex(env.baseURL)}(?:/)?$`), {
    timeout: shortTimeoutMS,
  });
  await expect(signInLink).toBeVisible({
    timeout: shortTimeoutMS,
  });
}

async function waitForLoginBoundary(
  page: Page,
  {
    mailboxNav,
    loginNameInput,
    passwordInput,
    loginRedirectButton,
    otherUserButton,
  }: {
    mailboxNav: ReturnType<Page["locator"]>;
    loginNameInput: ReturnType<Page["locator"]>;
    passwordInput: ReturnType<Page["locator"]>;
    loginRedirectButton: ReturnType<Page["getByRole"]>;
    otherUserButton: ReturnType<Page["getByRole"]>;
  },
): Promise<void> {
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (
      (await mailboxNav.isVisible().catch(() => false)) ||
      (await loginNameInput.isVisible().catch(() => false)) ||
      (await passwordInput.isVisible().catch(() => false)) ||
      (await loginRedirectButton.isVisible().catch(() => false)) ||
      (await otherUserButton.isVisible().catch(() => false))
    ) {
      return;
    }
    await page.waitForLoadState("domcontentloaded").catch(() => {});
    await page.waitForTimeout(pollIntervalMS);
  }
}
