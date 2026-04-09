import { test, expect, type Page } from "@playwright/test";
import { env } from "./env";
import { ensureLoggedIn, ensureTestUserExists, readBalance } from "./helpers";

/**
 * Complete Stripe Checkout with the test card.
 *
 * Handles the Stripe Link OTP flow: in test mode, typing "000000"
 * authenticates the Link account, then "Pay without Link" reveals the
 * standard card form.
 *
 * Card form selectors (confirmed against live Stripe Checkout):
 *   #cardNumber, #cardExpiry, #cardCvc, #billingName, #billingPostalCode
 *   — all in the main frame, not iframes.
 */
async function completeStripeCheckout(page: Page): Promise<void> {
  await page.waitForURL(/checkout\.stripe\.com/, { timeout: 30_000 });
  await page.waitForLoadState("domcontentloaded");
  await page.waitForTimeout(3_000);

  // If Stripe Link OTP modal is present, authenticate then dismiss.
  const hasLinkOTP =
    (await page
      .locator("text=000000")
      .count()
      .catch(() => 0)) > 0;

  if (hasLinkOTP) {
    await page.keyboard.type("000000", { delay: 80 });
    await page.waitForTimeout(2_000);
    const payWithoutLink = page.getByText("Pay without Link");
    await payWithoutLink.waitFor({ state: "visible", timeout: 10_000 });
    await payWithoutLink.click();
    await page.waitForTimeout(1_000);
  }

  // Fill the standard card form. Stripe may show an email field above the card inputs.
  const emailField = page.locator("#email");
  if ((await emailField.count()) > 0 && (await emailField.isVisible())) {
    await emailField.fill(env.testEmail);
  }

  await page.locator("#cardNumber").waitFor({ state: "visible", timeout: 10_000 });
  await page.locator("#cardNumber").fill(env.stripeCard);
  await page.locator("#cardExpiry").fill(env.stripeExpiry);
  await page.locator("#cardCvc").fill(env.stripeCVC);
  await page.locator("#billingName").fill("Demo User");

  // Stripe conditionally shows country dropdown and/or postal code.
  // Select US if a country selector is visible, then fill postal code if it appears.
  const countrySelect = page.locator("#billingCountry");
  if ((await countrySelect.count()) > 0 && (await countrySelect.isVisible())) {
    await countrySelect.selectOption("US");
    await page.waitForTimeout(500);
  }
  const postalCode = page.locator("#billingPostalCode");
  if ((await postalCode.count()) > 0 && (await postalCode.isVisible())) {
    await postalCode.fill("10001");
  }

  await page.getByRole("button", { name: /^Pay/ }).click();
  await page.waitForURL(/purchased=true/, { timeout: 60_000 });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe("Credit Purchase Flow", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test.beforeEach(async ({ page }) => {
    await ensureLoggedIn(page);
  });

  test("purchase $10 credit pack via Stripe, verify balance increase and billing history", async ({
    page,
  }) => {
    // 1. Record initial balance.
    const initialBalance = await readBalance(page);

    // 2. Navigate to credits purchase page.
    await page.getByRole("link", { name: "Buy Credits" }).click();
    await expect(page.getByRole("heading", { name: "Purchase Credits" })).toBeVisible();

    // 3. Click the $10 credit pack.
    await page.getByRole("button", { name: /\$10\b/ }).click();

    // 4. Complete Stripe Checkout.
    await completeStripeCheckout(page);

    // 5. Verify redirect with success indicator.
    await expect(page).toHaveURL(/purchased=true/);
    await expect(page.getByText("Credits purchased successfully")).toBeVisible({ timeout: 10_000 });

    // 6. Verify balance increased.
    // Deposit is async: webhook → task queue → worker (5s poll) → TigerBeetle.
    // Observed latency: 5-60s. Poll for up to 90s.
    await expect(async () => {
      await page.goto("/?purchased=true");
      await page.waitForLoadState("domcontentloaded");
      const newBalance = await readBalance(page);
      expect(newBalance).toBeGreaterThan(initialBalance);
    }).toPass({ timeout: 90_000, intervals: [5_000, 5_000, 10_000] });

    // 7. Verify billing history shows the purchase grant.
    await page.getByRole("link", { name: "Billing", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Active Credit Grants" })).toBeVisible();

    const grantsTable = page.locator("table").filter({ hasText: "Source" }).last();

    // At least one "purchase" grant should be visible.
    await expect(grantsTable.getByText("purchase").first()).toBeVisible({
      timeout: 10_000,
    });

    // Multiple grant rows should exist (seeded + purchased).
    const grantRows = grantsTable.locator("tbody tr");
    await expect(grantRows).not.toHaveCount(0);

    // The Subscriptions section should render (even if empty).
    await expect(page.getByRole("heading", { name: "Subscriptions" })).toBeVisible();
  });

  test("balance card shows purchased vs free tier breakdown", async ({ page }) => {
    await expect(page.getByText("Available Credits")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText("free tier")).toBeVisible();
    await expect(page.getByText("purchased")).toBeVisible();
  });
});
