import type { SandboxHarness } from "./harness";
import { env } from "./env";

export async function completeStripeCheckout(app: SandboxHarness): Promise<void> {
  await app.waitForCondition("stripe checkout redirect", 30_000, async () => {
    return app.page.url().includes("checkout.stripe.com") ? true : false;
  });

  await app.waitForCondition("stripe card form", 30_000, async () => {
    const cardNumberVisible = await app.page.locator("#cardNumber").isVisible().catch(() => false);
    const payWithoutLinkVisible = await app.page
      .getByText("Pay without Link")
      .isVisible()
      .catch(() => false);

    return cardNumberVisible || payWithoutLinkVisible;
  });

  if (await app.page.getByText("Pay without Link").isVisible().catch(() => false)) {
    await app.page.keyboard.type("000000", { delay: 80 });
    await app.page.getByText("Pay without Link").click();
  }

  const emailField = app.page.locator("#email");
  if (await emailField.isVisible().catch(() => false)) {
    await emailField.fill(env.testEmail);
  }

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

  await app.page.getByRole("button", { name: /^Pay/ }).click();
  await app.waitForCondition("billing return redirect", 60_000, async () => {
    return app.page.url().includes("/billing?purchased=true") ? true : false;
  });
}
