import { completeStripeCheckout } from "./billing-helpers";
import { ensureTestUserExists, expect, test, type SandboxHarness } from "./harness";

test.describe("Rent-a-Sandbox Billing", () => {
  test.describe.configure({ mode: "serial" });

  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("credit purchase returns to billing with a success flash and increased balance", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.expectSSRHTML("/billing/credits", ["Purchase Credits", "Available Credit Value"]);
      await app.assertStableRoute({
        path: "/billing/credits",
        ready: app.page.getByRole("heading", { name: "Purchase Credits" }),
        expectedText: ["Purchase Credits", "Available Credit Value", "$10"],
      });

      run.started_balance = await app.readBalance();

      await app.page.getByRole("link", { name: "Billing", exact: true }).click();
      await expect(app.page.getByRole("heading", { name: "Billing" })).toBeVisible();

      await app.page.getByRole("link", { name: "Buy Credits" }).click();
      await expect(app.page.getByRole("heading", { name: "Purchase Credits" })).toBeVisible();
      await app.page.getByRole("button", { name: /^\$10\b/ }).click();

      await completeStripeCheckout(app);
      await app.expectSSRHTML("/billing?purchased=true", [
        "Credits purchased",
        "Active Credit Grants",
      ]);

      run.detail_url = "/billing?purchased=true";
      run.finished_balance = await app.waitForCondition("purchased balance", 90_000, async () => {
        await app.goto("/billing?purchased=true");
        const currentBalance = await app.readBalance();
        const flashVisible = await app.page
          .getByText("Credits purchased successfully. Your balance has been updated.")
          .isVisible()
          .catch(() => false);

        if (flashVisible && currentBalance > run.started_balance) {
          return currentBalance;
        }

        return false;
      });

      await expect(app.page.getByRole("heading", { name: "Active Credit Grants" })).toBeVisible();
      await expect(app.page.getByText("purchase").first()).toBeVisible();
      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });

  test("subscription checkout activates Hobby and cancellation returns to free", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      await cancelExistingHobbySubscription(app);

      await activateHobbySubscription(app);

      await requestHobbyCancellation(app);

      await app.waitForCondition("hobby subscription cancellation", 90_000, async () => {
        await app.goto("/billing");
        const rowTexts = await app.page
          .getByTestId("subscription-row-sandbox-hobby")
          .allInnerTexts()
          .catch(() => []);
        const canceledRowText = rowTexts.find(
          (text) => text.includes("sandbox-hobby") && text.includes("canceled"),
        );
        const hasOpenHobbyRow = rowTexts.some(
          (text) => text.includes("sandbox-hobby") && !text.includes("canceled"),
        );

        if (
          canceledRowText?.includes("closed") &&
          !hasOpenHobbyRow &&
          !(await hasAnyVisibleHobbySubscriptionEntitlement(app))
        ) {
          return canceledRowText;
        }

        await app.page.waitForTimeout(2_000);
        return false;
      });

      run.detail_url = "/billing";
      run.finished_balance = await app.readBalance();
      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });

  test("subscription checkout activates Hobby and leaves it active", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      await cancelExistingHobbySubscription(app);
      await activateHobbySubscription(app);

      run.detail_url = "/billing?subscribed=true";
      run.finished_balance = await app.readBalance();
      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await app.persistRun(run);
    }
  });
});

async function activateHobbySubscription(app: SandboxHarness) {
  await app.expectSSRHTML("/billing/subscribe", ["Choose a Plan", "Hobby", "$5.00", "/mo"]);
  await app.assertStableRoute({
    path: "/billing/subscribe",
    ready: app.page.getByRole("heading", { name: "Choose a Plan" }),
    expectedText: ["Choose a Plan", "Hobby", "$5.00/mo"],
  });

  await beginHobbyCheckout(app);
  await completeStripeCheckout(app, { returnURLIncludes: "/billing?subscribed=true" });
  await app.expectSSRHTML("/billing?subscribed=true", ["Subscription activated", "Subscriptions"]);

  return await app.waitForCondition("hobby subscription activation", 120_000, async () => {
    await app.goto("/billing?subscribed=true");
    const rowTexts = await app.page
      .getByTestId("subscription-row-sandbox-hobby")
      .allInnerTexts()
      .catch(() => []);
    const activeRowText = rowTexts.find(
      (text) => text.includes("sandbox-hobby") && text.includes("active") && text.includes("paid"),
    );

    if (activeRowText && (await hasVisibleHobbySubscriptionEntitlements(app))) {
      return activeRowText;
    }

    await app.page.waitForTimeout(2_000);
    return false;
  });
}

async function beginHobbyCheckout(app: SandboxHarness) {
  await app.waitForCondition("hobby checkout redirect", 30_000, async () => {
    if (app.page.url().includes("checkout.stripe.com")) {
      return true;
    }

    const subscribeButton = app.page.getByTestId("subscribe-plan-sandbox-hobby");
    if (!(await subscribeButton.isVisible().catch(() => false))) {
      return false;
    }

    // SSR can expose the button before the TanStack Start click handler hydrates.
    await subscribeButton.click();
    await app.page.waitForTimeout(500);
    return app.page.url().includes("checkout.stripe.com") ? true : false;
  });
}

async function cancelExistingHobbySubscription(app: SandboxHarness) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await app.goto("/billing");
    const cancelButton = app.page.getByTestId("cancel-subscription-sandbox-hobby").first();
    if (await cancelButton.isVisible().catch(() => false)) {
      await requestHobbyCancellation(app);
      await app.waitForCondition("existing hobby subscription cancellation", 90_000, async () => {
        await app.goto("/billing");
        if (
          await app.page
            .getByTestId("cancel-subscription-sandbox-hobby")
            .first()
            .isVisible()
            .catch(() => false)
        ) {
          await app.page.waitForTimeout(2_000);
          return false;
        }
        return true;
      });
      continue;
    }
    return;
  }

  throw new Error("existing hobby subscription cancellation did not converge");
}

async function requestHobbyCancellation(app: SandboxHarness) {
  await app.waitForCondition("hobby cancellation confirmation", 10_000, async () => {
    const confirmButton = app.page.getByRole("button", { name: "Confirm Cancellation" });
    if (await confirmButton.isVisible().catch(() => false)) {
      return true;
    }

    const cancelButton = app.page.getByTestId("cancel-subscription-sandbox-hobby").first();
    if (!(await cancelButton.isVisible().catch(() => false))) {
      return false;
    }

    // TanStack Start can expose SSR HTML before the client click handler is hydrated.
    await cancelButton.click();
    return (await confirmButton.isVisible().catch(() => false)) ? true : false;
  });

  await app.page.getByRole("button", { name: "Confirm Cancellation" }).click();
}

// The entitlements view flattens bucket-scoped sources into each SKU row's
// receipt cell. The subscription contribution shows up as a
// `<dt data-source="subscription">` inside every SKU row under the bucket the
// subscription funds. The Hobby plan has to fund every Sandbox bucket —
// compute, memory, block_storage — for the test to consider it visible.
const hobbySubscriptionBuckets = ["compute", "memory", "block_storage"];

async function hasVisibleHobbySubscriptionEntitlements(app: SandboxHarness) {
  for (const bucket of hobbySubscriptionBuckets) {
    const entry = app.page
      .locator(`tr[data-bucket-id="${bucket}"] [data-source="subscription"]`)
      .first();
    if (!(await entry.isVisible().catch(() => false))) {
      return false;
    }
    const row = app.page.locator(`tr[data-bucket-id="${bucket}"]`).first();
    const text = await row.innerText().catch(() => "");
    if (!text.includes("$")) {
      return false;
    }
  }
  return true;
}

async function hasAnyVisibleHobbySubscriptionEntitlement(app: SandboxHarness) {
  for (const bucket of hobbySubscriptionBuckets) {
    const entry = app.page
      .locator(`tr[data-bucket-id="${bucket}"] [data-source="subscription"]`)
      .first();
    if (await entry.isVisible().catch(() => false)) {
      return true;
    }
  }
  return false;
}
