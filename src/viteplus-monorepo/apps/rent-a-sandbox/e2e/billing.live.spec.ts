import { completeStripeCheckout } from "./billing-helpers";
import { ensureTestUserExists, expect, shortTimeoutMS, test, type SandboxHarness } from "./harness";

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

      const topUpLedgerUnits = 1_000_000_000;
      await app.expectSSRHTML("/billing/credits", [
        "Purchase Credits",
        "Add prepaid account balance",
      ]);
      await app.assertStableRoute({
        path: "/billing/credits",
        ready: app.page.getByRole("heading", { name: "Purchase Credits" }),
        expectedText: ["Purchase Credits", "Add prepaid account balance", "$100"],
      });

      await app.goto("/billing");
      run.started_balance = await app.readBalance();
      const startedAccountBalance = await readVisibleAccountBalanceUnits(app);

      await expect(app.page.getByRole("heading", { name: "Billing" })).toBeVisible();

      await app.page.getByRole("link", { name: "Buy Credits" }).click();
      await expect(app.page.getByRole("heading", { name: "Purchase Credits" })).toBeVisible();
      await beginCreditCheckout(app, /^\$100\b/);

      await completeStripeCheckout(app);
      await app.expectSSRHTML("/billing?purchased=true", ["Credits purchased", "Account Balance"]);

      run.detail_url = "/billing?purchased=true";
      const purchaseResult = await app.waitForCondition("purchased balance", 90_000, async () => {
        await app.goto("/billing?purchased=true");
        const currentBalance = await app.readBalance();
        const currentAccountBalance = await readVisibleAccountBalanceUnits(app);
        const flashVisible = await app.page
          .getByText("Credits purchased successfully. Your account credit pool has been updated.")
          .isVisible()
          .catch(() => false);

        if (
          flashVisible &&
          currentBalance > run.started_balance &&
          currentAccountBalance >= startedAccountBalance + topUpLedgerUnits
        ) {
          return { currentBalance, currentAccountBalance };
        }

        return false;
      });
      run.finished_balance = purchaseResult.currentBalance;

      await expect(app.page.getByTestId("entitlements-account-balance")).toBeVisible();
      await expect(app.page.getByTestId("account-balance-value")).toBeVisible();
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

  test("contract checkout activates Hobby and schedules cancellation", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      await cancelExistingHobbyContract(app);

      await activateContract(app, hobbyPlan);

      await requestHobbyCancellation(app);

      await app.waitForCondition("hobby contract cancellation", 90_000, async () => {
        await app.goto("/billing");
        const rowTexts = await hobbyContractRows(app)
          .allInnerTexts()
          .catch(() => []);
        const scheduledRowText = rowTexts.find(
          (text) => text.includes("sandbox-hobby") && text.includes("cancel_scheduled"),
        );

        if (
          scheduledRowText?.includes("active")
        ) {
          return scheduledRowText;
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

  test("contract checkout activates Hobby and leaves it active", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      await cancelExistingHobbyContract(app);
      await activateContract(app, hobbyPlan);

      run.detail_url = "/billing?contracted=true";
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

  test("contract checkout upgrades Hobby to Pro and carries forward active entitlements", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.started_balance = await app.readBalance();
      await activateContract(app, proPlan);

      await app.waitForCondition("pro upgrade carries hobby forward", 120_000, async () => {
        await app.goto("/billing");
        const proRowTexts = await contractRows(app, proPlan.planID)
          .allInnerTexts()
          .catch(() => []);
        const activeProRowText = proRowTexts.find(
          (text) =>
            text.includes(proPlan.planID) && text.includes("active") && text.includes("paid"),
        );
        const activeHobbyRows = (
          await contractRows(app, hobbyPlan.planID)
            .allInnerTexts()
            .catch(() => [])
        ).filter(
          (text) =>
            text.includes(hobbyPlan.planID) && text.includes("active") && text.includes("paid"),
        );

        if (
          activeProRowText &&
          activeHobbyRows.length === 0
        ) {
          return activeProRowText;
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
});

type ContractPlanSpec = {
  planID: string;
  displayName: string;
  priceText: string;
};

const hobbyPlan: ContractPlanSpec = {
  planID: "sandbox-hobby",
  displayName: "Hobby",
  priceText: "$5.00/mo",
};

const proPlan: ContractPlanSpec = {
  planID: "sandbox-pro",
  displayName: "Pro",
  priceText: "$20.00/mo",
};

async function activateContract(app: SandboxHarness, plan: ContractPlanSpec) {
  await app.expectSSRHTML("/billing/subscribe", [
    "Choose a Plan",
    plan.displayName,
    plan.priceText.replace("/mo", ""),
    "/mo",
  ]);
  await app.assertStableRoute({
    path: "/billing/subscribe",
    ready: app.page.getByRole("heading", { name: "Choose a Plan" }),
    expectedText: ["Choose a Plan", plan.displayName, plan.priceText],
  });

  const redirect = await beginContractCheckout(app, plan);
  if (redirect === "checkout") {
    await completeStripeCheckout(app, { returnURLIncludes: "/billing?contracted=true" });
  }
  await app.expectSSRHTML("/billing?contracted=true", ["Contract checkout complete", "Contracts"]);
  await app.goto("/billing?contracted=true");

  return await app.waitForCondition(`${plan.planID} contract activation`, 120_000, async () => {
    const rowTexts = await contractRows(app, plan.planID)
      .allInnerTexts()
      .catch(() => []);
    const activeRowText = rowTexts.find(
      (text) => text.includes(plan.planID) && text.includes("active") && text.includes("paid"),
    );

    if (activeRowText) {
      return activeRowText;
    }

    await app.page.waitForTimeout(2_000);
    return false;
  });
}

async function beginCreditCheckout(app: SandboxHarness, buttonName: RegExp) {
  await app.waitForCondition("credit checkout redirect", 30_000, async () => {
    if (app.page.url().includes("checkout.stripe.com")) {
      return true;
    }

    const checkoutButton = app.page.getByRole("button", { name: buttonName });
    if (!(await checkoutButton.isVisible().catch(() => false))) {
      return false;
    }
    if (!(await checkoutButton.isEnabled().catch(() => false))) {
      return false;
    }

    // SSR can expose the button before the TanStack Start click handler hydrates.
    await checkoutButton.click();
    await app.page.waitForTimeout(500);
    return app.page.url().includes("checkout.stripe.com") ? true : false;
  });
}

async function beginContractCheckout(
  app: SandboxHarness,
  plan: ContractPlanSpec,
): Promise<"checkout" | "billing"> {
  return await app.waitForCondition(`${plan.planID} checkout redirect`, 30_000, async () => {
    if (app.page.url().includes("checkout.stripe.com")) {
      return "checkout";
    }
    if (app.page.url().includes("/billing?contracted=true")) {
      return "billing";
    }

    const subscribeButton = app.page.getByTestId(`start-contract-plan-${plan.planID}`);
    if (!(await subscribeButton.isVisible().catch(() => false))) {
      return false;
    }
    if (!(await subscribeButton.isEnabled().catch(() => false))) {
      const buttonText = await subscribeButton.innerText().catch(() => "");
      if (buttonText.includes("Current plan")) {
        await app.goto("/billing?contracted=true");
        return "billing";
      }
      return false;
    }

    // SSR can expose the button before the TanStack Start click handler hydrates.
    await subscribeButton.click();
    await app.page.waitForTimeout(500);
    if (app.page.url().includes("checkout.stripe.com")) {
      return "checkout";
    }
    if (app.page.url().includes("/billing?contracted=true")) {
      return "billing";
    }
    return false;
  });
}

async function cancelExistingHobbyContract(app: SandboxHarness) {
  for (let attempt = 0; attempt < 5; attempt += 1) {
    await app.goto("/billing");
    const cancelButton = hobbyCancelButtons(app).first();
    if (await cancelButton.isVisible().catch(() => false)) {
      await requestHobbyCancellation(app);
      await app.waitForCondition("existing hobby contract cancellation", 90_000, async () => {
        await app.goto("/billing");
        const rowText = await hobbyContractRows(app)
          .first()
          .innerText()
          .catch(() => "");
        if (rowText.includes("cancel_scheduled")) {
          return true;
        }
        if (
          await hobbyCancelButtons(app)
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

  throw new Error("existing hobby contract cancellation did not converge");
}

async function requestHobbyCancellation(app: SandboxHarness) {
  await app.waitForCondition("hobby cancellation confirmation", 10_000, async () => {
    if (await isHobbyCancellationScheduled(app)) {
      return true;
    }

    const confirmButton = app.page.getByRole("button", { name: "Confirm Cancellation" });
    if (await confirmButton.isVisible().catch(() => false)) {
      return true;
    }

    const cancelButton = hobbyCancelButtons(app).first();
    if (!(await cancelButton.isVisible().catch(() => false))) {
      return false;
    }

    // TanStack Start can expose SSR HTML before the client click handler is hydrated.
    await cancelButton.click();
    return (await confirmButton.isVisible().catch(() => false)) ? true : false;
  });

  if (!(await isHobbyCancellationScheduled(app))) {
    await app.page.getByRole("button", { name: "Confirm Cancellation" }).click();
  }
}

async function isHobbyCancellationScheduled(app: SandboxHarness) {
  const rowText = await hobbyContractRows(app)
    .first()
    .innerText()
    .catch(() => "");
  return rowText.includes("sandbox-hobby") && rowText.includes("cancel_scheduled");
}

function hobbyContractRows(app: SandboxHarness) {
  return contractRows(app, hobbyPlan.planID);
}

function hobbyCancelButtons(app: SandboxHarness) {
  return hobbyContractRows(app).getByRole("button", { name: "Cancel" });
}

function contractRows(app: SandboxHarness, planID: string) {
  return app.page.locator('[data-testid^="contract-row-"]').filter({ hasText: planID });
}

async function readVisibleAccountBalanceUnits(app: SandboxHarness) {
  const accountBalance = app.page.getByTestId("entitlements-account-balance").first();
  await accountBalance.waitFor({ state: "visible", timeout: shortTimeoutMS });
  const raw = await accountBalance.getAttribute("data-account-balance-units");
  if (raw === null) {
    throw new Error("account balance missing data-account-balance-units");
  }
  const units = Number.parseInt(raw, 10);
  if (!Number.isFinite(units)) {
    throw new Error(`account balance units is not numeric: ${raw}`);
  }
  return units;
}
