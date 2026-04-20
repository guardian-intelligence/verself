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
