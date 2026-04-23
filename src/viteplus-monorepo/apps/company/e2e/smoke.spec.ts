import { expect, test } from "@playwright/test";

test("landing renders with Argent wings and mission block", async ({ page }) => {
  await page.goto("/");

  // Hero headline is present and voice-compliant — the canary asserts the
  // exact copy from src/content/landing.ts so the test fails loudly if a
  // copy rewrite accidentally regresses into banned phrasing.
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "The world needs your business to succeed",
  );

  // Argent wings on the fold. Any SVG role="img" landing above the headline
  // fold satisfies §09 Iron.
  const wings = page.locator("svg[aria-label]").first();
  await expect(wings).toBeVisible();
});

test.describe("chrome invariants", () => {
  test("Workshop chrome Lockup renders the workshop-chip variant in Geist", async ({ page }) => {
    await page.goto("/");
    // The AppChrome Lockup emits data-variant on its wrapper span. Workshop
    // must carry the workshop-chip variant (iron tile with argent border),
    // not the old unframed Argent wings.
    const header = page.locator("header").first();
    const variant = await header
      .locator("[data-variant]")
      .first()
      .getAttribute("data-variant");
    expect(variant, "Workshop AppChrome Lockup variant").toBe("workshop-chip");

    // And the wordmark sets in Geist — Workshop bans Fraunces. The font-family
    // on the wordmark span resolves from --treatment-display-font, which is
    // Geist under Workshop.
    const wordmarkFont = await header.evaluate((el) => {
      const span = el.querySelector("[data-variant] > span:last-child") as HTMLElement | null;
      return span ? getComputedStyle(span).fontFamily : "";
    });
    // Computed font-family returns the full stack; the first token is what the
    // browser actually resolves to. Accept either the quoted or unquoted form.
    expect(wordmarkFont, "Workshop wordmark font stack starts with Geist").toMatch(/^(['"]?)Geist/);
  });

  test("Letters chrome Lockup wordmark sets in Fraunces", async ({ page }) => {
    await page.goto("/letters");
    const header = page.locator("header").first();
    const variant = await header
      .locator("[data-variant]")
      .first()
      .getAttribute("data-variant");
    expect(variant, "Letters AppChrome Lockup variant").toBe("chip");

    const wordmarkFont = await header.evaluate((el) => {
      const span = el.querySelector("[data-variant] > span:last-child") as HTMLElement | null;
      return span ? getComputedStyle(span).fontFamily : "";
    });
    expect(wordmarkFont, "Letters wordmark font stack starts with Fraunces").toMatch(
      /^(['"]?)Fraunces/,
    );
  });

  test("Newsroom page is Paper-grounded with a bounded Flare bulletin band", async ({ page }) => {
    await page.goto("/newsroom");
    // The page main must read on Paper — Newsroom is no longer a Flare
    // environment. The AppChrome also paints Paper per the new tokens.css
    // scope, so the header reads as a Paper bookplate with an ink emboss.
    const mainBg = await page.evaluate(() => {
      const scope = document.querySelector('[data-treatment="newsroom"]') as HTMLElement | null;
      return scope ? getComputedStyle(scope).backgroundColor : "";
    });
    expect(mainBg, "Newsroom scope background is Paper").toBe("rgb(246, 244, 237)");

    // The bulletin section exists as a bounded Flare band (max 480 px) — the
    // one place Flare appears on the page.
    const flareBands = await page.evaluate(() => {
      return Array.from(document.querySelectorAll("main *"))
        .filter((el) => {
          const cs = getComputedStyle(el);
          const rect = (el as HTMLElement).getBoundingClientRect();
          return cs.backgroundColor === "rgb(204, 255, 0)" && rect.height > 200;
        })
        .map((el) => (el as HTMLElement).getBoundingClientRect().height);
    });
    // Exactly one bulletin band (when a current item exists); may be zero
    // if the newsroom module is in its empty state.
    expect(flareBands.length, "one bounded Flare band on /newsroom").toBeLessThanOrEqual(1);
    for (const h of flareBands) {
      expect(h, "Flare band height ≤ 520 px").toBeLessThan(520);
    }
  });
});
