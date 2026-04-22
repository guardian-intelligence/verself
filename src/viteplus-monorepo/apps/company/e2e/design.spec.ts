import { expect, test } from "@playwright/test";

// Structural canary for the restructured /design page. Asserts the 6-entry
// rail, the per-treatment role-grammar palette grid, and each treatment's
// distinctive marker (Flare hairline on Company signature; wings-only +
// team badge on Workshop; Flare dot + Newsroom label; Bordeaux hairline +
// "Founder" identity on Letters paper card). These are DOM truths rather
// than visual pixel checks, so they survive font/color tweaks but still
// fail loudly on a structural regression.
//
// Desktop viewport is the Playwright default (1280×720, close enough to
// the 1440×900 design target for structural assertions). Mobile variant
// at the bottom of the file confirms the palette grid collapses from four
// columns to two.

test.describe("/design — treatment-first structure", () => {
  test("rail has 6 entries in Treatments + Applied groups, active state sets in Argent", async ({
    page,
  }) => {
    await page.goto("/design");

    // Desktop rail duplicates in DOM with the mobile <details> version, so
    // the total count is 12 (6 × 2). Each id resolves to one desktop + one
    // mobile link.
    const allEntries = page.locator('[data-testid^="design-nav-"]');
    await expect(allEntries).toHaveCount(12);
    for (const id of [
      "company",
      "workshop",
      "newsroom",
      "letters",
      "photography",
      "business-cards",
    ]) {
      await expect(page.locator(`[data-testid="design-nav-${id}"]`)).toHaveCount(2);
    }

    // Old System ids must not resolve to DOM sections.
    await expect(page.locator("#mark")).toHaveCount(0);
    await expect(page.locator("#typography")).toHaveCount(0);

    // Photography sub-label is gone; Business Cards is Title Case.
    await expect(
      page.locator('[data-testid="design-nav-photography"]').first(),
    ).toContainText(/Photography/);
    await expect(
      page.locator('[data-testid="design-nav-photography"]').first(),
    ).not.toContainText("scrim");
    await expect(
      page.locator('[data-testid="design-nav-business-cards"]').first(),
    ).toContainText("Business Cards");

    // Active-state number colour: deep-link via hash so useActiveAnchor's
    // hash-prime path picks Company immediately (the IntersectionObserver
    // path is flakey to time, so we exercise the deterministic code path).
    // The 01 number must render in Argent (rgb(255,255,255)) — not Flare.
    await page.goto("/design#company");
    await page.waitForTimeout(300);
    const companyLink = page.locator('[data-testid="design-nav-company"]').last();
    const numberSpan = companyLink.locator("span").first();
    const color = await numberSpan.evaluate((el) => getComputedStyle(el).color);
    expect(color, "active rail number").toBe("rgb(255, 255, 255)");
  });

  test("palette grammar is Ground · Accent · Mark · Muted across every treatment", async ({
    page,
  }) => {
    await page.goto("/design");

    // Exactly four palette grids, one per treatment.
    const grids = page.locator(".treatment-palette-grid");
    await expect(grids).toHaveCount(4);

    // Each grid declares the four role labels, in order.
    for (let i = 0; i < 4; i++) {
      const roleLabels = grids.nth(i).locator(".treatment-palette-role");
      await expect(roleLabels).toHaveText(["GROUND", "ACCENT", "MARK", "MUTED"], {
        useInnerText: true,
      });
    }

    // Newsroom's Muted column renders the "not used" placeholder because
    // Newsroom is broadcast, not reading. All other cells render swatches.
    const newsroomCells = page
      .locator("#newsroom .treatment-palette-grid .treatment-palette-cell");
    await expect(newsroomCells.nth(3)).toContainText("not used");
  });

  test("Workshop treatment content declines Fraunces", async ({ page }) => {
    await page.goto("/design");
    const workshop = page.locator("#workshop");

    // Workshop signature must carry a team badge ("PLATFORM · ENGINEERING")
    // next to wings, and no Fraunces-set wordmark inside the card.
    await expect(workshop).toContainText("Platform · Engineering");
    await expect(workshop).toContainText("Engineer Name");

    // Amber status dot + "INCIDENT RESPONSE · PAGEABLE" badge present.
    await expect(workshop).toContainText("incident response · pageable");

    // No Fraunces inside the treatment body — scoped to elements WITHIN the
    // palette, type ladder, UI specimen, and signature. The page's section
    // H2 (rendered by section-shell, outside the treatment body) is
    // deliberately Fraunces across every section and not asserted here.
    const fonts = await workshop.evaluate((root) => {
      const tables = Array.from(root.querySelectorAll("table"));
      const paletteRoles = Array.from(root.querySelectorAll(".treatment-palette-role"));
      const signatureCards = Array.from(
        root.querySelectorAll("div[style*='background: rgb(255, 255, 255)']"),
      );
      const walk = (nodes: Element[]) =>
        nodes.flatMap((n) =>
          Array.from(n.querySelectorAll("*")).map((el) => getComputedStyle(el).fontFamily),
        );
      return [...walk(tables), ...walk(paletteRoles), ...walk(signatureCards)].join(" | ");
    });
    expect(fonts, "Workshop body fonts").not.toMatch(/Fraunces/i);
  });

  test("Newsroom signature carries a Flare dot + NEWSROOM label, no hairline", async ({
    page,
  }) => {
    await page.goto("/design");
    const newsroom = page.locator("#newsroom");

    await expect(newsroom).toContainText("Press Officer Name");
    // The accent label sets in uppercase via text-transform; the DOM text is
    // "Newsroom", which getByText resolves regardless of CSS casing.
    await expect(newsroom.getByText("Newsroom", { exact: false }).first()).toBeVisible();

    // Flare (#CCFF00) appears on >= 2 distinct elements in Newsroom: the
    // Ground swatch in the palette, the mark carrier ground, the hero
    // surface, and the signature accent dot. Query spans + divs both.
    const flareCount = await newsroom.evaluate((root) => {
      return Array.from(root.querySelectorAll("span, div")).filter(
        (el) => getComputedStyle(el).backgroundColor === "rgb(204, 255, 0)",
      ).length;
    });
    expect(flareCount, "Flare-backed elements in Newsroom").toBeGreaterThanOrEqual(2);
  });

  test("Letters signature identifies by NAME · ROLE, not a valediction, and has a 3 px Bordeaux hairline", async ({
    page,
  }) => {
    await page.goto("/design");
    const letters = page.locator("#letters");

    // Signature identity line is a name — not the "— the founder" valediction
    // the baseline put here.
    const sigName = letters.getByText("Founder Name", { exact: true });
    await expect(sigName).toBeVisible();
    await expect(letters).toContainText("Founder · Guardian Intelligence");

    // The valediction moved into the article body specimen, above the sig.
    await expect(letters).toContainText(/— the founder/);

    // Bordeaux accent hairline in the signature card resolves to rgb(92, 31, 30)
    // at height 3px.
    const bordeauxHairline = await letters.evaluate((root) => {
      return Array.from(root.querySelectorAll("div")).some((el) => {
        const cs = getComputedStyle(el);
        return (
          cs.backgroundColor === "rgb(92, 31, 30)" &&
          parseInt(cs.height, 10) === 3 &&
          parseInt(cs.width, 10) === 44
        );
      });
    });
    expect(bordeauxHairline, "3px × 44px Bordeaux hairline").toBe(true);
  });

  test("Letters palette collapses to 4 role cells; no duplicate Stone chip", async ({ page }) => {
    await page.goto("/design");
    const cells = page.locator("#letters .treatment-palette-grid .treatment-palette-cell");
    // Four cells (Ground · Accent · Mark · Muted) — not the old 5 (Stone as a
    // fake fifth).
    await expect(cells).toHaveCount(4);
    // "Stone" appears in the muted role's opacity-ramp note, but NOT as a
    // swatch name.
    const swatchNames = await page
      .locator("#letters .treatment-palette-cell div:nth-of-type(2) > div:first-child")
      .allInnerTexts();
    expect(swatchNames).toEqual(expect.arrayContaining(["Paper", "Bordeaux", "Argent", "Ink"]));
    expect(swatchNames).not.toContain("Stone");
  });
});

test.describe("/design — mobile responsive", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("palette grid collapses to 2 columns at 390 px", async ({ page }) => {
    await page.goto("/design");
    const grid = page.locator("#company .treatment-palette-grid");
    await grid.scrollIntoViewIfNeeded();

    const cols = await grid.evaluate((el) => {
      const raw = getComputedStyle(el).gridTemplateColumns;
      // Rough count of columns in the computed template string — each track
      // renders as a pixel value separated by spaces.
      return raw.trim().split(/\s+/).length;
    });
    expect(cols).toBe(2);
  });

  test("rail collapses to <details> disclosure on mobile, closed by default", async ({ page }) => {
    await page.goto("/design");
    const disclosure = page.locator('nav details[open]');
    await expect(disclosure).toHaveCount(0);
    const summary = page.locator("nav details > summary");
    await expect(summary).toBeVisible();
    await expect(summary).toContainText("Jump to section");
  });
});
