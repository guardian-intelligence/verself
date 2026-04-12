import { completeStripeCheckout } from "./billing-helpers";
import { ensureTestUserExists, expect, test } from "./harness";

test.describe("Rent-a-Sandbox Billing", () => {
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
});
