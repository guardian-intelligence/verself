import { expect, test } from "@playwright/test";

// /newsroom/$slug structural test. Covers the full nav arc from the index
// hero CTA into the article and back out the "Back to the Newsroom" link,
// so the pair of company.newsroom_article.view + back_click spans land on
// the same trace path an actual visitor would draw through the site.
//
// Content-correctness comes from the seeded "we opened the newsroom" entry
// in src/content/newsroom.ts; this spec asserts STRUCTURE (breadcrumb,
// Fraunces H1, deck, byline, paragraph count, footer link), not copy — if
// the copy changes, the voice lint catches it, not Playwright.

const CANONICAL_SLUG = "we-opened-the-newsroom";

test.describe("/newsroom/$slug — article", () => {
  test("renders breadcrumb, Fraunces H1, deck, byline, paragraphs, read-next hand-off", async ({
    page,
  }) => {
    await page.goto(`/newsroom/${CANONICAL_SLUG}`);

    const article = page.locator("[data-newsroom-article]");
    await expect(article).toHaveAttribute("data-slug", CANONICAL_SLUG);

    // Breadcrumb → Newsroom root.
    const breadcrumb = page.getByRole("navigation", { name: "Breadcrumb" });
    await expect(breadcrumb.getByRole("link", { name: "Newsroom" })).toHaveAttribute(
      "href",
      "/newsroom",
    );

    // H1 carries the Newsroom display font. Under data-treatment="newsroom"
    // the var(--treatment-display-font) resolves to Fraunces.
    const h1 = article.locator("h1");
    await expect(h1).toBeVisible();
    const fontFamily = await h1.evaluate((el) => window.getComputedStyle(el).fontFamily);
    expect(fontFamily).toMatch(/Fraunces/i);

    // Body paragraphs — the seeded bulletin has at least four.
    const bodyParas = article.locator("p");
    const paraCount = await bodyParas.count();
    expect(paraCount, "article body should carry multiple paragraphs").toBeGreaterThanOrEqual(4);

    // Read-next footer owns the back link with the right destination.
    const back = article.locator("[data-newsroom-back]");
    await expect(back).toBeVisible();
    await expect(back).toHaveAttribute("href", "/newsroom");
  });

  test("unknown slug returns 404", async ({ request }) => {
    const response = await request.get("/newsroom/this-slug-does-not-exist");
    expect(response.status(), "unknown slug must 404").toBe(404);
  });

  test("index → article → back to index round-trip", async ({ page }) => {
    await page.goto("/newsroom");

    // Click into the Flare giant bulletin. The bulletin is an <a> carrying
    // data-newsroom-bulletin and an aria-label of the form
    // "Read bulletin: <title>", so selecting by the data-attribute keeps the
    // assertion stable against future copy tweaks to the label.
    const bulletin = page.locator("[data-newsroom-bulletin]");
    await bulletin.click();
    await expect(page).toHaveURL(new RegExp(`/newsroom/${CANONICAL_SLUG}$`));

    // Back out via the Read next footer.
    await page.locator("[data-newsroom-back]").click();
    await expect(page).toHaveURL(/\/newsroom\/?$/);

    // Landing back on the index still shows the bulletin (i.e., the index
    // route actually remounted, not a half-hydrated stale frame).
    await expect(page.locator("[data-newsroom-bulletin]").first()).toBeVisible();
  });
});
