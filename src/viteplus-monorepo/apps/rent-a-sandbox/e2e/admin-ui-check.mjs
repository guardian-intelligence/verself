import { chromium, expect } from "@playwright/test";
import fs from "node:fs/promises";

const runId = process.env.VERIFICATION_RUN_ID;
const baseURL = process.env.TEST_BASE_URL ?? "https://rentasandbox.anveio.com";
const authBaseURL =
  process.env.ZITADEL_BASE_URL ??
  (process.env.FORGE_METAL_DOMAIN ? `https://auth.${process.env.FORGE_METAL_DOMAIN}` : "https://auth.anveio.com");
const routeBaseURL = normalizeBaseURL(baseURL);
const deploymentDomain = process.env.FORGE_METAL_DOMAIN ?? inferDeploymentDomain(baseURL);
const artifactDir = process.env.ADMIN_UI_ARTIFACT_DIR ?? "artifacts/admin-ui-check";
const authOrLoginURL = createURLPattern(authBaseURL, ["/ui/login"]);
const appURL = createURLPattern(baseURL);
const postPasswordURL = createURLPattern(baseURL, ["/ui/mfa"]);
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
      await page.waitForLoadState("networkidle");

      const dashboardHeading = page.getByRole("heading", { name: "Dashboard" });
      const balanceCard = page.getByTestId("balance-card");
      if (!(await dashboardHeading.isVisible().catch(() => false))) {
        await page.goto("/login");
        await page.waitForLoadState("networkidle");

        const redirectButton = page.getByRole("button", { name: "click here" });
        if (await redirectButton.isVisible().catch(() => false)) {
          await Promise.all([
            page.waitForURL(authOrLoginURL, { timeout: 30000 }),
            redirectButton.click(),
          ]);
        }

        const loginNameInput = page.locator("#loginName");
        await loginNameInput.waitFor({ state: "visible", timeout: 30000 });
        await loginNameInput.fill(account.email);
        await page.locator('button[type="submit"]').click();

        const passwordInput = page.locator("#password");
        await passwordInput.waitFor({ state: "visible", timeout: 15000 });
        await passwordInput.fill(account.password);
        await Promise.all([
          page.waitForURL(postPasswordURL, { timeout: 30000 }),
          page.locator('button[type="submit"]').click(),
        ]);
        const skipButton = page.getByRole("button", { name: /^Skip$/ });
        if (await skipButton.isVisible().catch(() => false)) {
          await Promise.all([
            page.waitForURL(appURL, { timeout: 30000 }),
            skipButton.click(),
          ]);
        }
        await page.waitForLoadState("networkidle");
      }

      await expect(dashboardHeading).toBeVisible({ timeout: 15_000 });
      await expect(page.getByRole("link", { name: "Sign out" })).toBeVisible({
        timeout: 15_000,
      });
      await expect(balanceCard).toBeVisible({ timeout: 15_000 });

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

function createURLPattern(base, extraPathPatterns = []) {
  const normalizedBaseURL = normalizeBaseURL(base);
  const basePattern = escapeRegex(normalizedBaseURL);
  if (extraPathPatterns.length === 0) {
    return new RegExp(`^${basePattern}(?:[/?#].*)?$`);
  }

  const pathPatterns = extraPathPatterns.map(
    (pathPattern) => `${basePattern}${escapeRegex(pathPattern)}(?:[?#].*)?`,
  );
  return new RegExp(`^(?:${pathPatterns.join("|")})$`);
}

function normalizeBaseURL(baseURL) {
  return new URL(baseURL).href.replace(/\/$/, "");
}

function inferDeploymentDomain(baseURL) {
  const hostname = new URL(baseURL).hostname;
  const segments = hostname.split(".");
  return segments.length > 1 ? segments.slice(1).join(".") : hostname;
}

function escapeRegex(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
