import { expect, test } from "@playwright/test";

// Minimal structural tests for /design while the three-room model is still
// iterating. The prior audit pointed out that deep DOM-invariant assertions
// (Flare bands, exact computed backgrounds, wordmark fonts per route) would
// have caught several bugs — but they also fight the iteration loop at this
// stage. Kept here: route existence, /design/company retirement, tab-strip
// navigation. Visual gates live in the agent-browser screenshot review, not
// in Playwright, until the brand surfaces settle.

test.describe("/design — route tree", () => {
  test("overview renders and three treatment cards link to their sub-routes", async ({ page }) => {
    await page.goto("/design");

    await expect(page.locator("main h1").first()).toBeVisible();

    // The treatment cards link to their specimen sub-route. Matching by href
    // avoids ambiguity with the tab-strip anchors (which also carry a
    // data-treatment attribute for accent scoping).
    for (const treatment of ["workshop", "newsroom", "letters"] as const) {
      await expect(page.locator(`a[href="/design/${treatment}"]`).first()).toBeVisible();
    }

    // No retired Company route.
    await expect(page.locator('a[href="/design/company"]')).toHaveCount(0);
  });

  test("/design/company is retired and returns 404", async ({ request }) => {
    const response = await request.get("/design/company");
    expect(response.status(), "GET /design/company").toBe(404);
  });

  test("tab strip navigates between Overview / Workshop / Newsroom / Letters", async ({ page }) => {
    await page.goto("/design");
    const tabs = page.getByLabel("Design system treatments");

    await expect(tabs.getByRole("link", { name: "Company", exact: true })).toHaveCount(0);

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
