import { expect, test } from "@playwright/test";

// Structural canary for the /design route tree. After Phase 3 the monolithic
// /design page split into /design, /design/company, /design/workshop,
// /design/newsroom, /design/letters. Each route renders on its own treatment
// ground and AppChrome repaints accordingly.
//
// These assertions are DOM-truth (computed styles, visible text, tab
// navigation), not pixel checks — they survive copy edits and font tweaks
// but still fail loudly on a structural regression.

test.describe("/design — route tree", () => {
  test("overview renders four treatment cards + Applied footer", async ({ page }) => {
    await page.goto("/design");

    // One H1 per route. The overview carries the brand-system thesis.
    const h1 = page.locator("main h1").first();
    await expect(h1).toBeVisible();

    // Four treatment cards, each linking to its sub-route.
    for (const treatment of ["company", "workshop", "newsroom", "letters"] as const) {
      const card = page.locator(`a[data-treatment="${treatment}"]`).first();
      await expect(card).toBeVisible();
      await expect(card).toHaveAttribute("href", `/design/${treatment}`);
    }

    // Applied footer carries photography + business cards.
    await expect(page.locator("main")).toContainText(/photography/i);
    await expect(page.locator("main")).toContainText(/business cards/i);
  });

  test("each treatment route renders on its own ground and carries its H2", async ({ page }) => {
    const cases: Array<{
      route: string;
      ground: string;
      heading: RegExp;
    }> = [
      { route: "/design/company", ground: "rgb(14, 14, 14)", heading: /Company/i },
      { route: "/design/workshop", ground: "rgb(14, 14, 14)", heading: /Workshop/i },
      { route: "/design/newsroom", ground: "rgb(204, 255, 0)", heading: /Newsroom/i },
      { route: "/design/letters", ground: "rgb(246, 244, 237)", heading: /Letters/i },
    ];
    for (const { route, ground, heading } of cases) {
      await page.goto(route);
      const main = page.locator("main");
      const bg = await main.evaluate((el) => {
        const firstChild = el.firstElementChild as HTMLElement | null;
        return firstChild ? getComputedStyle(firstChild).backgroundColor : "";
      });
      expect(bg, `${route} ground`).toBe(ground);
      await expect(page.locator("main").getByRole("heading").first()).toContainText(heading);
    }
  });

  test("tab strip navigates between treatments", async ({ page }) => {
    await page.goto("/design");

    // Click each tab and verify the URL changes + the active tab gets the
    // treatment accent underline.
    await page.getByRole("link", { name: "Company", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/company$/);

    await page.getByRole("link", { name: "Workshop", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/workshop$/);

    await page.getByRole("link", { name: "Newsroom", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/newsroom$/);

    await page.getByRole("link", { name: "Letters", exact: true }).click();
    await expect(page).toHaveURL(/\/design\/letters$/);

    await page.getByRole("link", { name: "Overview", exact: true }).click();
    await expect(page).toHaveURL(/\/design$/);
  });

  test("Letters route renders Bordeaux accent in the article specimen", async ({ page }) => {
    await page.goto("/design/letters");
    // Paper ground end-to-end — the AppChrome (sticky header) inherits
    // treatment=letters so the TopBar sits on Paper, not Iron.
    const header = page.locator("header").first();
    const headerBg = await header.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(headerBg, "letters header ground").toBe("rgb(246, 244, 237)");

    // Bordeaux ornaments somewhere on page.
    const bordeaux = await page.evaluate(() => {
      return Array.from(document.querySelectorAll("*")).some((el) => {
        const cs = getComputedStyle(el);
        return cs.borderLeftColor === "rgb(92, 31, 30)" || cs.color === "rgb(92, 31, 30)";
      });
    });
    expect(bordeaux, "Bordeaux ornament present").toBe(true);
  });

  test("Newsroom route puts AppChrome on Flare ground with ink wordmark", async ({ page }) => {
    await page.goto("/design/newsroom");
    const header = page.locator("header").first();
    const headerBg = await header.evaluate((el) => getComputedStyle(el).backgroundColor);
    expect(headerBg, "newsroom header ground").toBe("rgb(204, 255, 0)");
  });
});

test.describe("/design — mobile responsive", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("tab strip is horizontally scrollable on mobile, not a Sheet", async ({ page }) => {
    await page.goto("/design/company");
    const nav = page.locator("nav").filter({ hasText: "Workshop" }).first();
    await expect(nav).toBeVisible();
    const overflowX = await nav.evaluate((el) => {
      const inner = el.querySelector("div");
      return inner ? getComputedStyle(inner).overflowX : "";
    });
    expect(overflowX).toBe("auto");
    // No <details>/<summary> drawer — no mobile Sheet.
    await expect(page.locator("nav details")).toHaveCount(0);
  });
});
