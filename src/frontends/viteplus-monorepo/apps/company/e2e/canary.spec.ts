import { expect, test } from "@playwright/test";

// Company-site canary. Walks every IA route, the seeded Letter, each
// dynamic OG card, and the press brand-kit download, then asserts the basic
// content shape on each. Deploy verification queries ClickHouse for the
// corresponding `company.*` spans after this Playwright run finishes.

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
// model); Dispatch retired with the Letters rename; /letters/rss retired with
// the gazette-layout cutover (the index now reads like a periodical front
// page; readers who want a feed get the Newsroom strip on the home page).
const RETIRED_ROUTES: readonly string[] = [
  "/design/company",
  "/dispatch",
  "/dispatch/rss",
  "/dispatch/ship-the-reference-architecture",
  "/letters/rss",
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

  // 2. Landing-specific assertion: blank optical plate on the fold.
  await page.goto("/");
  await expect(page.locator("main h1")).toHaveCount(0);
  await expect(page.locator("main p")).toHaveCount(0);
  await expect(page.locator("footer")).toHaveCount(0);
  await expect(page.locator("canvas")).toHaveCount(1);

  // 2a. Masthead cutover — every chrome-bearing layout renders the wordmark
  //     in tracked uppercase Geist. The Fraunces masthead retired 2026-04-24;
  //     the canary fails loudly if a layout regresses to the old face. Also
  //     asserts the section suffix: root bears no suffix, /letters carries
  //     "Letters", /newsroom carries "Newsroom" (accent-quoted case-
  //     insensitively; CSS does the uppercasing, source keeps the mixed
  //     form).
  const mastheadCases: ReadonlyArray<{ readonly path: string; readonly section: string | null }> = [
    { path: "/", section: null },
    { path: "/letters", section: "Letters" },
    { path: "/newsroom", section: "Newsroom" },
  ];
  for (const { path, section } of mastheadCases) {
    await page.goto(path, { waitUntil: "domcontentloaded" });
    const wordmark = page.locator("header [data-lockup-wordmark]");
    await expect(wordmark, `masthead wordmark on ${path}`).toBeVisible();
    const styles = await wordmark.evaluate((el) => {
      const s = window.getComputedStyle(el);
      return {
        fontFamily: s.fontFamily,
        textTransform: s.textTransform,
        fontWeight: s.fontWeight,
        fontSize: s.fontSize,
      };
    });
    expect(styles.fontFamily, `wordmark face on ${path} — must be Geist`).toMatch(/Geist/i);
    expect(styles.fontFamily, `wordmark must not regress to Fraunces on ${path}`).not.toMatch(
      /Fraunces/i,
    );
    expect(styles.textTransform, `wordmark must be uppercase on ${path}`).toBe("uppercase");
    expect(Number(styles.fontWeight), `wordmark weight on ${path}`).toBeGreaterThanOrEqual(500);
    // Quiet-masthead size gate. Guardian whispers at the top of the page;
    // anything that doubles the font in a future change has regressed the
    // whole point. 16 px is a generous ceiling — the sm Lockup currently
    // ships at 11 px and sub-16 covers any reasonable retune.
    const fontPx = Number(styles.fontSize.replace("px", ""));
    expect(fontPx, `masthead font-size on ${path} — must stay quiet`).toBeLessThan(16);

    const text = (await wordmark.textContent()) ?? "";
    expect(text, `wordmark must say "Guardian" on ${path}`).toMatch(/guardian/i);
    if (section === null) {
      expect(text, `root masthead must NOT carry a section suffix`).not.toMatch(/·/);
    } else {
      expect(text, `${path} masthead must carry the section suffix`).toMatch(
        new RegExp(`·\\s*${section}`, "i"),
      );
    }
  }

  // 3. Every catalogued OG slug renders as a 1200×630 SVG.
  // The route returns 500 on OG-card validation failure.
  for (const slug of OG_SLUGS) {
    const og = await request.get(`/og/${slug}`);
    expect(og.status(), `GET /og/${slug}`).toBe(200);
    const ogContentType = og.headers()["content-type"] ?? "";
    expect(ogContentType).toContain("image/svg+xml");
    const ogBody = await og.text();
    expect(ogBody).toContain('width="1200"');
    expect(ogBody).toContain('height="630"');
    // Masthead wordmark ships in uppercase Geist — match case-insensitively
    // so the assertion survives any future case flip without drifting.
    expect(ogBody).toMatch(/guardian/i);
  }

  // 4. Retired routes must return 404. Asserts the Dispatch → Letters rename
  // and the apps/letters retirement actually cut over — no stale route
  // registration, no regenerated artifact, no 301 shim.
  for (const path of RETIRED_ROUTES) {
    const response = await request.get(path);
    expect(response.status(), `GET ${path} must be gone`).toBe(404);
  }
  const retiredOg = await request.get("/og/dispatch");
  expect(retiredOg.status(), "GET /og/dispatch must be gone").toBe(404);

  // 5. Brand kit download — assert the response is a real zip (starts with
  // the PK signature) and emit a click-handler-equivalent request so the
  // canary has exercised the same path a press visitor would.
  const kit = await request.get("/brand-kit/guardian-intelligence.zip");
  expect(kit.status(), "GET /brand-kit/guardian-intelligence.zip").toBe(200);
  const kitBytes = await kit.body();
  expect(kitBytes.length).toBeGreaterThan(256);
  expect(kitBytes.subarray(0, 2).toString("ascii")).toBe("PK");

  // 6. Visit /press and click the brand kit link so the
  // company.press.kit_download span fires (this is the click-time emission,
  // distinct from the GET above which goes direct to the static file).
  await page.goto("/press");
  const kitLink = page.locator('a[href="/brand-kit/guardian-intelligence.zip"]');
  await expect(kitLink).toBeVisible();
  // Click with download=true keeps the nav from leaving the page so the span
  // has a chance to export before the BSP flushes on visibilitychange.
  await kitLink.click({ modifiers: ["Alt"] });
  await page.waitForTimeout(500);

  // 7. /newsroom interaction surface. The mount-time newsroom.index.view span
  // already fires when the IA walk above hits /newsroom. The bulletin_click
  // span only fires on a user gesture, so drive the giant bulletin click
  // here — that also navigates to /newsroom/<slug>, which fires
  // company.newsroom_article.view on the destination route (asserted
  // independently in the ClickHouse verification window).
  await page.goto("/newsroom");
  const bulletin = page.locator("[data-newsroom-bulletin]");
  await expect(bulletin).toBeVisible();
  await bulletin.click();
  await expect(page).toHaveURL(/\/newsroom\/[a-z0-9-]+$/);
  // The browser tracer batches for 2s; wait one batch interval so the
  // click-time span lands before ClickHouse is queried.
  await page.waitForTimeout(2_500);
});
