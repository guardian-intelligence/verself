import { chromium } from "@playwright/test";
import fs from "node:fs/promises";

const runId = process.env.VERIFICATION_RUN_ID;
const domain = process.env.FORGE_METAL_DOMAIN ?? "anveio.com";
const baseURL = process.env.TEST_BASE_URL ?? `https://letters.${domain}`;
const email = process.env.TEST_EMAIL ?? `ceo@${domain}`;
const password = process.env.TEST_PASSWORD;
const routeBaseURL = normalizeBaseURL(baseURL);
const appURLPattern = createURLPattern(baseURL);

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
  run_id: runId,
  email,
  slug: "",
  editor_index_url: "",
  editor_url: "",
  public_url: "",
  console_messages: consoleMessages,
  page_errors: pageErrors,
  failed_requests: failedRequests,
  editor_title_before_hydration: "",
  editor_title_after_hydration: "",
  ssr_article_text: "",
  hydrated_article_text: "",
  status: "unknown",
  timestamp: new Date().toISOString(),
};

function actionableFailures(requests) {
  return requests.filter((request) => {
    if (!request.url.startsWith(routeBaseURL)) {
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

  await page.goto("/editor", { waitUntil: "domcontentloaded" });
  const editorTitleLink = page.getByRole("link", { name: title }).first();
  await editorTitleLink.waitFor({ state: "visible", timeout: 15000 });
  result.editor_index_url = page.url();
  result.editor_title_before_hydration = normalizeText(await editorTitleLink.innerText());
  await page.waitForLoadState("networkidle");
  await editorTitleLink.waitFor({ state: "visible", timeout: 15000 });
  result.editor_title_after_hydration = normalizeText(await editorTitleLink.innerText());

  if (result.editor_title_before_hydration !== result.editor_title_after_hydration) {
    throw new Error(
      `Editor list changed during hydration: ${result.editor_title_before_hydration} -> ${result.editor_title_after_hydration}`,
    );
  }
  if (await page.getByText("No posts yet. Start writing!").isVisible().catch(() => false)) {
    throw new Error("Editor dashboard collapsed to the empty state after hydration");
  }

  result.public_url = `${baseURL}/${result.slug}`;
  await page.goto(`/${result.slug}`, { waitUntil: "domcontentloaded" });
  const article = page.locator("article");
  await page.getByRole("heading", { name: title }).waitFor({ state: "visible", timeout: 15000 });
  await page.getByText(body).waitFor({ state: "visible", timeout: 15000 });
  result.ssr_article_text = normalizeText(await article.innerText());
  await page.getByRole("button", { name: /Clap/ }).waitFor({ state: "visible", timeout: 15000 });

  let sameOriginFailures = actionableFailures(failedRequests);
  if (pageErrors.length > 0 || sameOriginFailures.length > 0) {
    throw new Error(
      `Verification errors detected: ${[pageErrors[0], sameOriginFailures[0]?.failure, sameOriginFailures[0]?.url].filter(Boolean).join(" | ")}`,
    );
  }

  await page.getByRole("heading", { name: title }).waitFor({ state: "visible", timeout: 15000 });
  await page.getByText(body).waitFor({ state: "visible", timeout: 15000 });
  result.hydrated_article_text = normalizeText(await article.innerText());

  if (result.ssr_article_text !== result.hydrated_article_text) {
    throw new Error(
      `SSR content changed during hydration: ${result.ssr_article_text} -> ${result.hydrated_article_text}`,
    );
  }

  sameOriginFailures = actionableFailures(failedRequests);
  const hydrationWarnings = consoleMessages.filter(
    (message) =>
      message.type === "error" && /hydration|did not match|text content does not match/i.test(message.text),
  );
  if (pageErrors.length > 0 || sameOriginFailures.length > 0 || hydrationWarnings.length > 0) {
    throw new Error(
      `Verification errors detected after hydration: ${[
        pageErrors[0],
        sameOriginFailures[0]?.failure,
        sameOriginFailures[0]?.url,
        hydrationWarnings[0]?.text,
      ]
        .filter(Boolean)
        .join(" | ")}`,
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

function normalizeBaseURL(baseURL) {
  return new URL(baseURL).href.replace(/\/$/, "");
}

function createURLPattern(baseURL, extraPathPatterns = []) {
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

function normalizeText(value) {
  return value.replace(/\s+/g, " ").trim();
}
