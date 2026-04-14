import type { SandboxHarness } from "./harness";
import type { Locator } from "@playwright/test";
import { env } from "./env";

export async function completeStripeCheckout(
  app: SandboxHarness,
  options: { returnURLIncludes?: string } = {},
): Promise<void> {
  const returnURLIncludes = options.returnURLIncludes ?? "/billing?purchased=true";

  await app.waitForCondition("stripe checkout redirect", 30_000, async () => {
    return app.page.url().includes("checkout.stripe.com") ? true : false;
  });

  if (
    await app.page
      .getByText("Pay without Link")
      .isVisible()
      .catch(() => false)
  ) {
    await app.page.keyboard.type("000000", { delay: 80 });
    await app.page.getByText("Pay without Link").click();
  }

  await revealStripeCardForm(app);

  const emailField = app.page.locator("#email");
  if (await emailField.isVisible().catch(() => false)) {
    await emailField.fill(env.testEmail);
  }

  if (await isStripeCardFormVisible(app)) {
    await app.page.locator("#cardNumber").fill(env.stripeCard);
    await app.page.locator("#cardExpiry").fill(env.stripeExpiry);
    await app.page.locator("#cardCvc").fill(env.stripeCVC);
    await app.page.locator("#billingName").fill(`${env.testFirstName} ${env.testLastName}`);

    const countrySelect = app.page.locator("#billingCountry");
    if (await countrySelect.isVisible().catch(() => false)) {
      await countrySelect.selectOption("US");
    }

    const postalCode = app.page.locator("#billingPostalCode");
    if (await postalCode.isVisible().catch(() => false)) {
      await postalCode.fill("10001");
    }
  }

  await clickStripeSubmit(app);
  await app.waitForCondition("billing return redirect", 60_000, async () => {
    return app.page.url().includes(returnURLIncludes) ? true : false;
  });
}

async function revealStripeCardForm(app: SandboxHarness): Promise<void> {
  await app.waitForCondition("stripe card form", 45_000, async () => {
    if (await isStripeCardFormVisible(app)) {
      return true;
    }
    if (await isStripeSavedPaymentReady(app)) {
      return true;
    }

    for (const locator of [
      app.page.locator('button[aria-label="Pay with card"]').first(),
      app.page.locator('[data-testid="card-accordion-item-button"]').first(),
      app.page.getByRole("button", { name: /pay with card/i }).first(),
    ]) {
      if (await clickStripeCardSelector(locator)) {
        return (await isStripeCardFormVisible(app)) ? true : false;
      }
    }

    const cardRadio = app.page.getByRole("radio", { name: /^card$/i }).first();
    if (await clickStripeCardSelector(cardRadio, { force: true })) {
      return (await isStripeCardFormVisible(app)) ? true : false;
    }

    return false;
  });
}

async function isStripeSavedPaymentReady(app: SandboxHarness): Promise<boolean> {
  const buttons = app.page.getByRole("button", { name: /^(Pay|Subscribe|Save)$/i });
  const buttonCount = await buttons.count();
  for (let index = 0; index < buttonCount; index += 1) {
    if (await buttons.nth(index).isVisible().catch(() => false)) {
      return true;
    }
  }
  return false;
}

async function clickStripeCardSelector(
  locator: Locator,
  options: { force?: boolean } = {},
): Promise<boolean> {
  if (!(await locator.isVisible().catch(() => false))) {
    return false;
  }

  await locator.click({ force: options.force ?? false, timeout: 1_000 });
  return true;
}

async function isStripeCardFormVisible(app: SandboxHarness): Promise<boolean> {
  return await app.page
    .locator("#cardNumber")
    .isVisible()
    .catch(() => false);
}

async function clickStripeSubmit(app: SandboxHarness): Promise<void> {
  await app.waitForCondition("stripe checkout submit", 30_000, async () => {
    const buttons = app.page.getByRole("button", { name: /^(Pay|Subscribe|Save)\b/i });
    const buttonCount = await buttons.count();
    for (let index = 0; index < buttonCount; index += 1) {
      const button = buttons.nth(index);
      if (!(await button.isVisible().catch(() => false))) {
        continue;
      }

      const text = await button.innerText().catch(() => "");
      if (/pay with (card|link|amazon|cash app)/i.test(text)) {
        continue;
      }

      await button.click();
      return true;
    }

    return false;
  });
}
