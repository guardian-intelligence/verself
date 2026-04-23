import { expect, test } from "@playwright/test";

// Company-site canary. Walks every IA route, the seeded Letter, each
// dynamic OG card, and the press brand-kit download, then asserts the basic
// content shape on each. The accompanying scripts/verify-company-live.sh
// queries ClickHouse for the corresponding `company.*` spans after this
// Playwright run finishes.

const IA_ROUTES: readonly string[] = [
  "/",
  "/design",
  "/design/workshop",
  "/design/newsroom",
  "/design/letters",
  "/letters",
  "/newsroom",
  "/newsroom/we-opened-the-newsroom",
  "/solutions",
  "/company",
  "/careers",
  "/press",
  "/changelog",
  "/contact",
  "/letters/ship-the-reference-architecture",
];

// Retired routes that must not resurrect. /design/company retired with the
// three-treatment cutover (Company treatment is no longer part of the brand
// model); Dispatch retired with the Letters rename.
const RETIRED_ROUTES: readonly string[] = [
  "/design/company",
  "/dispatch",
  "/dispatch/rss",
  "/dispatch/ship-the-reference-architecture",
];

const OG_SLUGS: readonly string[] = ["home", "design", "letters", "newsroom", "solutions"];

test("company canary — walk IA + exercise OG + brand kit", async ({ page, request }) => {
  // 1. Walk every IA node. Each triggers a company.route_view browser span.
  for (const path of IA_ROUTES) {
    const response = await page.goto(path, { waitUntil: "domcontentloaded" });
    expect(response?.status(), `GET ${path}`).toBe(200);
    await expect(page).toHaveTitle(/./);
    // Scroll past the fold so the landing hero-view + section-view intersection
    // observers fire on /.
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
    await page.waitForTimeout(300);
  }

  // 2. Landing-specific assertion: Argent wings on the fold.
  await page.goto("/");
  const wings = page.locator("svg").first();
  await expect(wings).toBeVisible();

  // 3. Letters RSS must parse as well-formed XML.
  const rss = await request.get("/letters/rss");
  expect(rss.status(), "GET /letters/rss").toBe(200);
  const rssContentType = rss.headers()["content-type"] ?? "";
  expect(rssContentType).toContain("application/rss+xml");
  const rssBody = await rss.text();
  expect(rssBody).toMatch(/<rss[^>]*>/);
  expect(rssBody).toContain("Guardian");

  // 4. Every catalogued OG slug renders as a 1200×630 SVG with Argent wings
  // and no voice violations (the route returns 500 on voice failure — if any
  // catalogued spec regresses into a banned term, this test fails loudly).
  for (const slug of OG_SLUGS) {
    const og = await request.get(`/og/${slug}`);
    expect(og.status(), `GET /og/${slug}`).toBe(200);
    const ogContentType = og.headers()["content-type"] ?? "";
    expect(ogContentType).toContain("image/svg+xml");
    const ogBody = await og.text();
    expect(ogBody).toContain('width="1200"');
    expect(ogBody).toContain('height="630"');
    expect(ogBody).toContain("Guardian");
  }

  // 5. Retired routes must return 404. Asserts the Dispatch → Letters rename
  // and the apps/letters retirement actually cut over — no stale route
  // registration, no regenerated artifact, no 301 shim.
  for (const path of RETIRED_ROUTES) {
    const response = await request.get(path);
    expect(response.status(), `GET ${path} must be gone`).toBe(404);
  }
  const retiredOg = await request.get("/og/dispatch");
  expect(retiredOg.status(), "GET /og/dispatch must be gone").toBe(404);

  // 6. Brand kit download — assert the response is a real zip (starts with
  // the PK signature) and emit a click-handler-equivalent request so the
  // canary has exercised the same path a press visitor would.
  const kit = await request.get("/brand-kit/guardian-intelligence.zip");
  expect(kit.status(), "GET /brand-kit/guardian-intelligence.zip").toBe(200);
  const kitBytes = await kit.body();
  expect(kitBytes.length).toBeGreaterThan(256);
  expect(kitBytes.subarray(0, 2).toString("ascii")).toBe("PK");

  // 7. Visit /press and click the brand kit link so the
  // company.press.kit_download span fires (this is the click-time emission,
  // distinct from the GET above which goes direct to the static file).
  await page.goto("/press");
  const kitLink = page.locator('a[href="/brand-kit/guardian-intelligence.zip"]');
  await expect(kitLink).toBeVisible();
  // Click with download=true keeps the nav from leaving the page so the span
  // has a chance to export before the BSP flushes on visibilitychange.
  await kitLink.click({ modifiers: ["Alt"] });
  await page.waitForTimeout(500);

  // 8. /newsroom interaction surface. The mount-time newsroom.index.view span
  // already fires when the IA walk above hits /newsroom, but the three
  // interaction spans (tab_change, card_click, subscribe_submit) only fire
  // on user gestures. Exercise each so every canary run lands the full
  // newsroom.index.* span family in ClickHouse, which is what the live
  // verification script at src/platform/scripts/verify-company-live.sh
  // asserts via `newsroom.index.span_families = 4`.
  await page.goto("/newsroom");
  const tablist = page.locator("[data-newsroom-tabstrip]");
  await tablist.getByRole("tab", { name: "Milestones", exact: true }).click();
  await page.waitForTimeout(100);
  // Submit the noop_stub subscribe form (the span fires; the form resets).
  const subscribe = page.locator("[data-newsroom-subscribe]");
  await subscribe.getByLabel("Email address").fill("canary@example.com");
  await subscribe.getByRole("button", { name: /Subscribe/i }).click();
  await page.waitForTimeout(100);
  // Card click. Navigates to /newsroom/<slug>; that nav also fires the
  // company.newsroom_article.view span on the destination route, which the
  // live script asserts independently in poll #5.
  await page.goto("/newsroom");
  const firstCard = page.locator("[data-newsroom-archive-card]").first();
  await firstCard.click();
  await expect(page).toHaveURL(/\/newsroom\/[a-z0-9-]+$/);
  await page.waitForTimeout(300);
});
