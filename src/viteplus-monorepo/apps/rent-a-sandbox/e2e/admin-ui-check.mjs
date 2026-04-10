import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";

const runId = process.env.VERIFICATION_RUN_ID;
const baseURL = process.env.TEST_BASE_URL ?? "https://rentasandbox.anveio.com";
const routeBaseURL = normalizeBaseURL(baseURL);
const deploymentDomain = process.env.FORGE_METAL_DOMAIN ?? inferDeploymentDomain(baseURL);
const artifactDir = process.env.ADMIN_UI_ARTIFACT_DIR ?? "artifacts/admin-ui-check";
const shortTimeoutMS = 5_000;
const pollIntervalMS = 250;
const accounts = [
  {
    label: "acme-admin",
    email: process.env.ACME_ADMIN_EMAIL ?? `acme-admin@${deploymentDomain}`,
    password: process.env.ACME_ADMIN_PASSWORD,
  },
].filter((account) => account.password);

if (!runId) {
  throw new Error("VERIFICATION_RUN_ID is required");
}

if (accounts.length === 0) {
  throw new Error("Missing account password");
}

await fs.mkdir(artifactDir, { recursive: true });

const out = [];
const browser = await chromium.launch({ headless: true });
try {
  for (const account of accounts) {
    const context = await browser.newContext({
      baseURL,
      ignoreHTTPSErrors: true,
    });
    await context.route(`${routeBaseURL}/**`, async (route) => {
      await route.continue({
        headers: {
          ...route.request().headers(),
          "X-Forge-Metal-Verification-Run": runId,
        },
      });
    });
    const page = await context.newPage();
    const consoleMessages = [];
    const pageErrors = [];
    const failedRequests = [];
    page.on("console", (msg) => consoleMessages.push({ type: msg.type(), text: msg.text() }));
    page.on("pageerror", (err) => pageErrors.push(err.stack || String(err)));
    page.on("requestfailed", (req) =>
      failedRequests.push({ url: req.url(), failure: req.failure()?.errorText || "unknown" }),
    );
    const dashboardHeading = page.getByRole("heading", { name: "Dashboard" });
    const balanceCard = page.getByTestId("balance-card");
    const signOutLink = page.getByRole("link", { name: "Sign out" });
    const loginNameInput = page.locator("#loginName");
    const passwordInput = page.locator("#password");
    const redirectButton = page.getByRole("button", { name: /click here/i });
    const otherUserButton = page.getByRole("button", { name: /other user/i });
    const skipButton = page.getByRole("button", { name: /^Skip$/ });

    const result = {
      label: account.label,
      email: account.email,
      run_id: runId,
      final_url: "",
      balance_visible: false,
      header_visible: false,
      main_text: "",
      console_messages: consoleMessages,
      page_errors: pageErrors,
      failed_requests: failedRequests,
      status: "unknown",
      timestamp: new Date().toISOString(),
    };

    try {
      await page.goto("/");
      await page.waitForLoadState("domcontentloaded");

      if (!(await isDashboardReady({ dashboardHeading, balanceCard, signOutLink }))) {
        await page.goto("/login");
        await page.waitForLoadState("domcontentloaded");
        await completeLoginFlow(page, {
          email: account.email,
          password: account.password,
          dashboardHeading,
          balanceCard,
          signOutLink,
          loginNameInput,
          passwordInput,
          redirectButton,
          otherUserButton,
          skipButton,
        });
      }

      await expect(dashboardHeading).toBeVisible({ timeout: shortTimeoutMS });
      await expect(signOutLink).toBeVisible({ timeout: shortTimeoutMS });
      await expect(balanceCard).toBeVisible({ timeout: shortTimeoutMS });

      result.final_url = page.url();
      result.balance_visible = await balanceCard.isVisible().catch(() => false);
      result.header_visible = await page
        .getByRole("link", { name: "Rent-a-Sandbox" })
        .isVisible()
        .catch(() => false);
      result.main_text = (await page.locator("main").innerText().catch(() => ""))
        .trim()
        .slice(0, 1500);
      await page.screenshot({
        path: `${artifactDir}/${account.label}-${runId}.png`,
        fullPage: true,
      });
      const sameOriginFailures = failedRequests.filter((request) =>
        request.url.startsWith(routeBaseURL),
      );
      const hydrationWarnings = consoleMessages.filter(
        (message) =>
          message.type === "error" &&
          /hydration|did not match|text content does not match/i.test(message.text),
      );
      if (pageErrors.length > 0 || sameOriginFailures.length > 0 || hydrationWarnings.length > 0) {
        result.status = "failed";
        result.error = [
          pageErrors[0],
          sameOriginFailures[0]?.failure,
          sameOriginFailures[0]?.url,
          hydrationWarnings[0]?.text,
        ]
          .filter(Boolean)
          .join(" | ");
      } else {
        result.status = "ok";
      }
    } catch (error) {
      result.status = "failed";
      result.error = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
      result.final_url = page.url();
      result.balance_visible = await balanceCard.isVisible().catch(() => false);
      result.header_visible = await page
        .getByRole("link", { name: "Rent-a-Sandbox" })
        .isVisible()
        .catch(() => false);
      result.main_text = (await page.locator("main").innerText().catch(() => ""))
        .trim()
        .slice(0, 1500);
      await page
        .screenshot({
          path: `${artifactDir}/${account.label}-${runId}-failed.png`,
          fullPage: true,
        })
        .catch(() => {});
    } finally {
      out.push(result);
      await context.close();
    }
  }
} finally {
  await browser.close();
}

const resultsPath = `${artifactDir}/${runId}.json`;
await fs.writeFile(resultsPath, JSON.stringify(out, null, 2));
console.log(JSON.stringify({ run_id: runId, results_path: resultsPath, results: out }, null, 2));

if (out.some((result) => result.status !== "ok")) {
  process.exitCode = 1;
}

function normalizeBaseURL(baseURL) {
  return new URL(baseURL).href.replace(/\/$/, "");
}

async function completeLoginFlow(
  page,
  {
    email,
    password,
    dashboardHeading,
    balanceCard,
    signOutLink,
    loginNameInput,
    passwordInput,
    redirectButton,
    otherUserButton,
    skipButton,
  },
) {
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (await isDashboardReady({ dashboardHeading, balanceCard, signOutLink })) {
      return;
    }

    if (await redirectButton.isVisible().catch(() => false)) {
      await redirectButton.click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        redirectButton,
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
        redirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await loginNameInput.isVisible().catch(() => false)) {
      await loginNameInput.fill(email);
      await page.locator('button[type="submit"]').click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        redirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    if (await passwordInput.isVisible().catch(() => false)) {
      await passwordInput.fill(password);
      await page.locator('button[type="submit"]').click();
      await waitForAuthBoundary(page, {
        dashboardHeading,
        loginNameInput,
        passwordInput,
        redirectButton,
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
        redirectButton,
        otherUserButton,
        skipButton,
      });
      continue;
    }

    await page.waitForTimeout(pollIntervalMS);
  }

  throw new Error(`Unable to complete login flow from ${page.url()}`);
}

async function isDashboardReady({ dashboardHeading, balanceCard, signOutLink }) {
  const [dashboardVisible, balanceVisible, signOutVisible] = await Promise.all([
    dashboardHeading.isVisible().catch(() => false),
    balanceCard.isVisible().catch(() => false),
    signOutLink.isVisible().catch(() => false),
  ]);
  return dashboardVisible && balanceVisible && signOutVisible;
}

async function waitForAuthBoundary(
  page,
  {
    dashboardHeading,
    loginNameInput,
    passwordInput,
    redirectButton,
    otherUserButton,
    skipButton,
  },
) {
  for (let attempt = 0; attempt < shortTimeoutMS / pollIntervalMS; attempt += 1) {
    if (
      (await dashboardHeading.isVisible().catch(() => false)) ||
      (await loginNameInput.isVisible().catch(() => false)) ||
      (await passwordInput.isVisible().catch(() => false)) ||
      (await redirectButton.isVisible().catch(() => false)) ||
      (await otherUserButton.isVisible().catch(() => false)) ||
      (await skipButton.isVisible().catch(() => false))
    ) {
      return;
    }
    await page.waitForLoadState("domcontentloaded").catch(() => {});
    await page.waitForTimeout(pollIntervalMS);
  }
}

function inferDeploymentDomain(baseURL) {
  const hostname = new URL(baseURL).hostname;
  const segments = hostname.split(".");
  return segments.length > 1 ? segments.slice(1).join(".") : hostname;
}
