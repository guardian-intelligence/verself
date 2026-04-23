import { expect, test } from "@playwright/test";

// Structural test for the /newsroom index. Asserts the Ramp-style layout
// lands end-to-end: masthead, featured hero (NewsroomCard), "Latest" section
// with a filterable tab strip, archive grid (NewsroomArchiveCard), pagination
// stub, and subscribe band. Each interaction is paired with a browser span
// (newsroom.index.view / tab_change / card_click / subscribe_submit); the
// post-deploy canary asserts those land in ClickHouse (verify-company-live.sh).
//
// Everything is local bare metal; actions run inside the 5-second cap set by
// playwright.config.ts per the repo convention (timeouts above 5s are a
// real bug, not a latency issue).

test.describe("/newsroom — index", () => {
  test("renders masthead, featured bulletin, tab strip, archive grid, pagination, subscribe band", async ({
    page,
  }) => {
    await page.goto("/newsroom");

    // 1. Masthead H1.
    const h1 = page.locator("main h1").first();
    await expect(h1).toBeVisible();
    await expect(h1).toContainText(/Bulletins from the house/);

    // 2. Featured hero — the one NewsroomCard with the Flare stripe + Lockup.
    // The hero aria-label is "Bulletin: <title>" per index.tsx.
    const hero = page.locator('[aria-label^="Bulletin: "]');
    await expect(hero).toHaveCount(1);
    await expect(hero).toBeVisible();

    // 3. "Latest" section with four tabs.
    await expect(page.getByRole("heading", { name: "Latest", exact: true })).toBeVisible();
    const tablist = page.locator("[data-newsroom-tabstrip]");
    await expect(tablist).toBeVisible();
    for (const label of ["All", "Announcements", "Milestones", "Notes"] as const) {
      await expect(tablist.getByRole("tab", { name: label, exact: true })).toBeVisible();
    }

    // 4. Archive grid — the seeded content has three non-featured bulletins.
    const archive = page.locator("[data-newsroom-archive-card]");
    await expect(archive).toHaveCount(3);

    // 5. Subscribe band renders and owns an email input and a submit button.
    const subscribe = page.locator("[data-newsroom-subscribe]");
    await expect(subscribe).toBeVisible();
    await expect(subscribe.getByLabel("Email address")).toBeVisible();
    await expect(subscribe.getByRole("button", { name: /Subscribe/i })).toBeVisible();

    // 6. Pagination surface is present even at sub-page-size item counts so
    // the structural assertion holds when we scale past the page size.
    await expect(page.locator("[data-newsroom-pagination]")).toBeVisible();
  });

  test("tab strip filters the archive grid by category", async ({ page }) => {
    await page.goto("/newsroom");

    const tablist = page.locator("[data-newsroom-tabstrip]");
    const archive = page.locator("[data-newsroom-archive-card]");

    // All tab shows everything that isn't the hero (three items).
    await expect(archive).toHaveCount(3);

    // Milestones tab filters to only data-category="milestone" cards. The
    // seed has two milestones (brand-system-shipped + letters-is-live) in the
    // archive.
    await tablist.getByRole("tab", { name: "Milestones", exact: true }).click();
    await expect(archive).toHaveCount(2);
    for (const locator of await archive.all()) {
      await expect(locator).toHaveAttribute("data-category", "milestone");
    }

    // Notes tab has one card (observability-in-public).
    await tablist.getByRole("tab", { name: "Notes", exact: true }).click();
    await expect(archive).toHaveCount(1);
    await expect(archive.first()).toHaveAttribute("data-category", "note");

    // Announcements archive is empty — the canonical announcement is the
    // featured hero, not in the grid. Empty-state copy renders instead.
    await tablist.getByRole("tab", { name: "Announcements", exact: true }).click();
    await expect(archive).toHaveCount(0);
    await expect(page.getByText(/Nothing in this category yet/)).toBeVisible();

    // Back to All restores the full grid.
    await tablist.getByRole("tab", { name: "All", exact: true }).click();
    await expect(archive).toHaveCount(3);
  });

  test("clicking an archive card navigates to /newsroom/<slug>", async ({ page }) => {
    await page.goto("/newsroom");

    const firstCard = page.locator("[data-newsroom-archive-card]").first();
    const slug = await firstCard.getAttribute("data-slug");
    expect(slug, "archive card must expose its slug for click-through").toBeTruthy();

    await firstCard.click();
    await expect(page).toHaveURL(new RegExp(`/newsroom/${slug}$`));
  });

  test("clicking the featured hero CTA navigates to the article route", async ({ page }) => {
    await page.goto("/newsroom");

    // The hero CTA in NewsroomCard renders as the only trailing anchor inside
    // [aria-label^="Bulletin: "]. Anchor by visible text "Read the bulletin"
    // (the default newsroomCtaLabel for the seeded announcement).
    const hero = page.locator('[aria-label^="Bulletin: "]');
    const cta = hero.getByRole("link", { name: /Read the bulletin/ });
    await expect(cta).toBeVisible();
    const href = await cta.getAttribute("href");
    expect(href).toMatch(/^\/newsroom\/[a-z0-9-]+$/);
    await cta.click();
    await expect(page).toHaveURL(new RegExp(`${href}$`));
  });

  test("subscribe form accepts an email and stays on page (noop_stub)", async ({ page }) => {
    await page.goto("/newsroom");

    const subscribe = page.locator("[data-newsroom-subscribe]");
    const email = subscribe.getByLabel("Email address");
    await email.fill("press@example.com");
    await subscribe.getByRole("button", { name: /Subscribe/i }).click();

    // Form submit is a noop_stub until the newsletter service lands; the
    // page must not navigate.
    await expect(page).toHaveURL(/\/newsroom$/);
    // Field resets on submit.
    await expect(email).toHaveValue("");
  });
});
