import { expect, test } from "@playwright/test";

test("landing renders with Argent wings and hero headline", async ({ page }) => {
  await page.goto("/");

  // Hero headline present — copy comes from src/content/landing.ts.
  await expect(page.getByRole("heading", { level: 1 })).toContainText(
    "The world needs your business to succeed",
  );

  // An aria-labelled SVG (the Argent wings) lands on the fold. Any chrome
  // regression that drops the mark entirely surfaces here.
  const wings = page.locator("svg[aria-label]").first();
  await expect(wings).toBeVisible();
});
