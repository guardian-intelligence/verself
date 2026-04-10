import { chromium } from "@playwright/test";
import fs from "node:fs/promises";

const runId = process.env.VERIFICATION_RUN_ID;
const domain = process.env.FORGE_METAL_DOMAIN ?? "anveio.com";
const baseURL = process.env.TEST_BASE_URL ?? `https://letters.${domain}`;
const email = process.env.TEST_EMAIL ?? `ceo@${domain}`;
const password = process.env.TEST_PASSWORD;
const baseHost = new URL(baseURL).host;
const appURLPattern = new RegExp(escapeRegex(baseHost));

if (!runId) {
  throw new Error("VERIFICATION_RUN_ID is required");
}

if (!password) {
  throw new Error("TEST_PASSWORD is required");
}

await fs.mkdir("artifacts/letters-live-check", { recursive: true });

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  baseURL,
  ignoreHTTPSErrors: true,
});
await context.route(`${baseURL}/**`, async (route) => {
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
  run_id: runId,
  email,
  slug: "",
  editor_url: "",
  public_url: "",
  console_messages: consoleMessages,
  page_errors: pageErrors,
  failed_requests: failedRequests,
  status: "unknown",
  timestamp: new Date().toISOString(),
};

function actionableFailures(requests) {
  return requests.filter((request) => {
    if (!request.url.includes(baseHost)) {
      return false;
    }
    return !(
      request.failure === "net::ERR_ABORTED" &&
      request.url.includes("/v1/shape")
    );
  });
}

try {
  const title = `Verification ${runId}`;
  const body = `letters live verification ${runId}`;

  await page.goto("/editor/new");

  const loginNameInput = page.locator("#loginName");
  if (await loginNameInput.isVisible().catch(() => false)) {
    await loginNameInput.fill(email);
    await page.locator('button[type="submit"]').click();

    const passwordInput = page.locator("#password");
    await passwordInput.waitFor({ state: "visible", timeout: 15000 });
    await passwordInput.fill(password);
    await Promise.all([
      page.waitForURL(appURLPattern, { timeout: 30000 }),
      page.locator('button[type="submit"]').click(),
    ]);
  }

  await page.getByPlaceholder("Post title").fill(title);
  await page.locator(".ProseMirror").click();
  await page.locator(".ProseMirror").fill(body);

  await Promise.all([page.getByRole("button", { name: "Publish" }).click()]);
  await page.waitForURL(
    (url) => url.pathname.startsWith("/editor/") && url.pathname !== "/editor/new",
    { timeout: 30000 },
  );

  result.editor_url = page.url();
  result.slug = page.url().split("/").pop() ?? "";

  await page.getByText("Status:").waitFor({ state: "visible", timeout: 15000 });
  await page.getByText("Published", { exact: true }).waitFor({ state: "visible", timeout: 15000 });

  result.public_url = `${baseURL}/${result.slug}`;
  await page.goto(`/${result.slug}`);
  await page.getByRole("heading", { name: title }).waitFor({ state: "visible", timeout: 15000 });
  await page.getByText(body).waitFor({ state: "visible", timeout: 15000 });

  let sameOriginFailures = actionableFailures(failedRequests);
  if (pageErrors.length > 0 || sameOriginFailures.length > 0) {
    throw new Error(
      `Verification errors detected: ${[pageErrors[0], sameOriginFailures[0]?.failure, sameOriginFailures[0]?.url].filter(Boolean).join(" | ")}`,
    );
  }

  await page.reload({ waitUntil: "domcontentloaded" });
  await page.getByRole("heading", { name: title }).waitFor({ state: "visible", timeout: 15000 });
  await page.getByText(body).waitFor({ state: "visible", timeout: 15000 });

  sameOriginFailures = actionableFailures(failedRequests);
  if (pageErrors.length > 0 || sameOriginFailures.length > 0) {
    throw new Error(
      `Verification errors detected after reload: ${[pageErrors[0], sameOriginFailures[0]?.failure, sameOriginFailures[0]?.url].filter(Boolean).join(" | ")}`,
    );
  }

  await page.screenshot({
    path: `artifacts/letters-live-check/${runId}.png`,
    fullPage: true,
  });
  result.status = "ok";
} catch (error) {
  result.status = "failed";
  result.error = error instanceof Error ? `${error.name}: ${error.message}` : String(error);
  await page
    .screenshot({
      path: `artifacts/letters-live-check/${runId}-failed.png`,
      fullPage: true,
    })
    .catch(() => {});
} finally {
  const resultPath = `artifacts/letters-live-check/${runId}.json`;
  await fs.writeFile(resultPath, JSON.stringify(result, null, 2));
  console.log(JSON.stringify({ resultPath, result }, null, 2));
  await context.close();
  await browser.close();
}

function escapeRegex(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
