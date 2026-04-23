import { expect, test } from "@playwright/test";

// Structural canary for the /design route tree. After the trifurcation:
//   /design, /design/workshop, /design/newsroom, /design/letters — three
//   treatment specimens plus the overview, all under the Workshop layout
//   (so every /design/* page carries the Workshop chrome on Iron).
//
// The specimen bodies override ground internally:
//   /design/workshop → Iron (Workshop specimen body)
//   /design/newsroom → Paper (band-on-Paper composition, Flare bounded)
//   /design/letters  → Paper (editorial register end-to-end)
//
// These assertions are DOM-truth (computed styles, visible text, tab
// navigation), not pixel checks — they survive copy edits and font tweaks
// but still fail loudly on a structural regression.

test.describe("/design — route tree", () => {
  test("overview renders three treatment cards + Applied footer", async ({ page }) => {
    await page.goto("/design");

    const h1 = page.locator("main h1").first();
    await expect(h1).toBeVisible();

    // Three treatment cards, each linking to its sub-route. Company is retired.
    for (const treatment of ["workshop", "newsroom", "letters"] as const) {
      const card = page.locator(`a[data-treatment="${treatment}"]`).first();
      await expect(card).toBeVisible();
      await expect(card).toHaveAttribute("href", `/design/${treatment}`);
    }

    // No Company card.
    await expect(page.locator('a[data-treatment="company"]')).toHaveCount(0);

    // Applied footer still carries photography + business cards.
    await expect(page.locator("main")).toContainText(/photography/i);
    await expect(page.locator("main")).toContainText(/business cards/i);
  });

  test("/design/company is retired and returns a 404-shaped response", async ({ request }) => {
    const response = await request.get("/design/company");
    expect(response.status(), "GET /design/company").toBe(404);
  });

  test("every /design/* page renders the Workshop chrome", async ({ page }) => {
    for (const route of ["/design", "/design/workshop", "/design/newsroom", "/design/letters"]) {
      await page.goto(route);
      // Header is AppChrome — on Workshop that's Iron.
      const header = page.locator("header").first();
      const headerBg = await header.evaluate((el) => getComputedStyle(el).backgroundColor);
      expect(headerBg, `${route} header is Workshop chrome`).toBe("rgb(14, 14, 14)");
    }
  });

  test("specimen bodies render on their expected ground", async ({ page }) => {
    const cases: Array<{
      route: string;
      expected: string;
      description: string;
    }> = [
      {
        route: "/design/workshop",
        expected: "rgb(14, 14, 14)",
        description: "Workshop specimen inherits Iron from the _workshop layout",
      },
      {
        route: "/design/newsroom",
        expected: "rgb(246, 244, 237)",
        description: "Newsroom specimen wrapped on Paper (band-on-Paper composition)",
      },
      {
        route: "/design/letters",
        expected: "rgb(246, 244, 237)",
        description: "Letters specimen on Paper end-to-end",
      },
    ];
    for (const { route, expected, description } of cases) {
      await page.goto(route);
      // Find the deepest data-treatment wrapper that surrounds the specimen
      // body. design/newsroom and design/letters nest a data-treatment="letters"
      // block inside main (Paper wrap). design/workshop inherits from the
      // outer _workshop layout, so we read that scope's wrapper instead.
      const specimen = await page.evaluate(() => {
        const main = document.querySelector("main");
        if (!main) return "";
        // Only treat a direct child of <main> as a page-level treatment wrap.
        // Deeper nested [data-treatment] on specimen internals (the palette
        // card, individual demonstration blocks) are component-scoped and
        // would otherwise mask the page ground.
        const directWrap = Array.from(main.children).find((el) =>
          (el as HTMLElement).hasAttribute("data-treatment"),
        ) as HTMLElement | undefined;
        if (directWrap) return getComputedStyle(directWrap).backgroundColor;
        // No page-level inner wrap — read the outer layout's data-treatment,
        // which for design/* is the _workshop layout (Iron).
        const outerScope = document.querySelector("[data-treatment]") as HTMLElement | null;
        return outerScope ? getComputedStyle(outerScope).backgroundColor : "";
      });
      expect(specimen, `${route} — ${description}`).toBe(expected);
    }
  });

  test("Newsroom specimen body contains a bounded Flare band (no full-page Flare)", async ({
    page,
  }) => {
    await page.goto("/design/newsroom");
    // Exactly one Flare-band specimen exists, and it is bounded in height.
    const flareBands = await page.evaluate(() => {
      return Array.from(document.querySelectorAll("*"))
        .filter((el) => {
          const cs = getComputedStyle(el);
          const rect = (el as HTMLElement).getBoundingClientRect();
          return cs.backgroundColor === "rgb(204, 255, 0)" && rect.height > 200;
        })
        .map((el) => (el as HTMLElement).getBoundingClientRect().height);
    });
    // At least one Flare band exists (the hero specimen) and it's bounded
    // under 520 px (plan's band ceiling was 480 px with rounding slack).
    expect(flareBands.length).toBeGreaterThanOrEqual(1);
    for (const h of flareBands) {
      expect(h, "Flare band height is bounded").toBeLessThan(520);
    }
  });

  test("Letters specimen renders a Vellum colophon block", async ({ page }) => {
    await page.goto("/design/letters");
    // Colophon block is tagged with data-block="colophon" and paints on Vellum.
    const colophon = page.locator('[data-block="colophon"]').first();
    await expect(colophon).toBeVisible();
    const bg = await colophon.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(bg, "Letters colophon on Vellum").toBe("rgb(232, 226, 210)");
  });

  test("Letters specimen renders Bordeaux ornaments", async ({ page }) => {
    await page.goto("/design/letters");
    const bordeaux = await page.evaluate(() => {
      return Array.from(document.querySelectorAll("*")).some((el) => {
        const cs = getComputedStyle(el);
        return cs.borderLeftColor === "rgb(92, 31, 30)" || cs.color === "rgb(92, 31, 30)";
      });
    });
    expect(bordeaux, "Bordeaux ornament present on /design/letters").toBe(true);
  });

  test("tab strip navigates between Overview / Workshop / Newsroom / Letters", async ({ page }) => {
    await page.goto("/design");

    // Scope the tab-strip lookups to the design-nav region; "Newsroom" and
    // "Letters" also appear in the site footer (Workshop layout's "Read"
    // column) which would otherwise match and fail strict-mode.
    const tabs = page.getByLabel("Design system treatments");

    // No Company tab.
    await expect(tabs.getByRole("link", { name: "Company", exact: true })).toHaveCount(0);

    // BRAND SYSTEM eyebrow sits above the tabs so the row reads as section nav.
    await expect(tabs).toContainText(/Brand system/i);

    await tabs.getByRole("link", { name: "Workshop", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/workshop$/);

    await tabs.getByRole("link", { name: "Newsroom", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/newsroom$/);

    await tabs.getByRole("link", { name: "Letters", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/letters$/);

    await tabs.getByRole("link", { name: "Overview", exact: true }).click();
    await expect(page).toHaveURL(/\/design$/);
  });
});

test.describe("/design — mobile responsive", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("tab strip is horizontally scrollable on mobile, not a Sheet", async ({ page }) => {
    await page.goto("/design/workshop");
    const nav = page.locator("nav").filter({ hasText: "Workshop" }).first();
    await expect(nav).toBeVisible();
    // The scroll container pairs overflow-x: auto with overflow-y: hidden so
    // the browser doesn't synthesise a phantom vertical scrollbar.
    const overflow = await nav.evaluate((el) => {
      const inner = el.querySelector(".overflow-x-auto");
      if (!inner) return { x: "", y: "" };
      const cs = getComputedStyle(inner as Element);
      return { x: cs.overflowX, y: cs.overflowY };
    });
    expect(overflow.x).toBe("auto");
    expect(overflow.y).toBe("hidden");
    await expect(page.locator("nav details")).toHaveCount(0);
  });
});
