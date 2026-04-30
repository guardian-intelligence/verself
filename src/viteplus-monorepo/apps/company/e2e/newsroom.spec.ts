import { expect, test } from "@playwright/test";

// Structural test for the /newsroom index. Asserts the Ramp-style layout
// lands end-to-end: masthead, the one Flare giant bulletin banner, and the
// article metadata strip below. Archive grid, tab strip, pagination, and
// subscribe band are retired until we have a second bulletin and a
// newsletter service — the rule is one Flare giant bulletin per page. Each
// interaction fires a browser span (newsroom.index.view / bulletin_click);
// deployment verification asserts those land in ClickHouse.
//
// Everything is local bare metal; actions run inside the 5-second cap set by
// playwright.config.ts per the repo convention (timeouts above 5s are a
// real bug, not a latency issue).

test.describe("/newsroom — index", () => {
  test("renders masthead, Flare giant bulletin, and article metadata", async ({ page }) => {
    await page.goto("/newsroom");

    // 1. Masthead H1.
    const h1 = page.locator("main h1").first();
    await expect(h1).toBeVisible();
    await expect(h1).toContainText(/Bulletins from the house/);

    // 2. Exactly one Flare giant bulletin — the rule for Newsroom is one
    // Flare event per page. The bulletin is a link to the article route.
    const bulletin = page.locator("[data-newsroom-bulletin]");
    await expect(bulletin).toHaveCount(1);
    await expect(bulletin).toBeVisible();
    const href = await bulletin.getAttribute("href");
    expect(href).toMatch(/^\/newsroom\/[a-z0-9-]+$/);

    // 3. The Flare bulletin paints plain Flare (#CCFF00) — no pattern, no
    // gradient. Asserting via computed background-color keeps the "one
    // Flare per page" rule provable and catches regressions where the band
    // quietly switches to Argent or Iron.
    const bg = await bulletin.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg.replace(/\s+/g, "")).toBe("rgb(204,255,0)");

    // 4. Article metadata strip renders under the bulletin, carrying the
    // deck copy and the author name.
    const meta = page.locator("[data-newsroom-bulletin-meta]");
    await expect(meta).toBeVisible();
    await expect(meta).toContainText(/Bulletins, milestones, and public notes/);
    await expect(meta).toContainText(/Guardian/);

    // 5. The retired surfaces are gone — no archive grid, no tab strip,
    // no pagination stub, no subscribe band.
    await expect(page.locator("[data-newsroom-archive-card]")).toHaveCount(0);
    await expect(page.locator("[data-newsroom-tabstrip]")).toHaveCount(0);
    await expect(page.locator("[data-newsroom-pagination]")).toHaveCount(0);
    await expect(page.locator("[data-newsroom-subscribe]")).toHaveCount(0);
  });

  test("clicking the Flare giant bulletin navigates to the article route", async ({ page }) => {
    await page.goto("/newsroom");

    const bulletin = page.locator("[data-newsroom-bulletin]");
    const slug = await bulletin.getAttribute("data-slug");
    expect(slug, "bulletin must expose its slug for click-through").toBeTruthy();

    await bulletin.click();
    await expect(page).toHaveURL(new RegExp(`/newsroom/${slug}$`));
  });
});
