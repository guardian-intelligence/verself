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
      await app.expectSSRHTML("/settings/billing/credits", [
        "Purchase credits",
        "Add prepaid account balance",
      ]);
      await app.assertStableRoute({
        path: "/settings/billing/credits",
        ready: app.page.getByRole("heading", { name: "Purchase credits" }),
        expectedText: ["Purchase credits", "Add prepaid account balance", "$100"],
      });

      await app.goto("/settings/billing");
      run.started_balance = await app.readBalance();
      const startedAccountBalance = await readVisibleAccountBalanceUnits(app);

      await expect(app.page.getByTestId("settings-tab-billing")).toHaveAttribute(
        "data-status",
        "active",
      );

      await app.page.getByRole("link", { name: "Buy credits" }).click();
      await expect(app.page.getByRole("heading", { name: "Purchase credits" })).toBeVisible();
      await beginCreditCheckout(app, /^\$100\b/);

      await completeStripeCheckout(app);
      await app.expectSSRHTML("/settings/billing?purchased=true", [
        "Credits purchased",
        "Account Balance",
      ]);

      run.detail_url = "/settings/billing?purchased=true";
      const purchaseResult = await app.waitForCondition("purchased balance", 90_000, async () => {
        await app.goto("/settings/billing?purchased=true");
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
      await resetContractState(app);

      await activateContract(app, hobbyPlan);

      await requestHobbyCancellation(app);

      await app.waitForCondition("hobby contract cancellation", 30_000, async () => {
        await app.goto("/settings/billing");
        const rowTexts = await hobbyContractRows(app)
          .allInnerTexts()
          .catch(() => []);
        const scheduledRowText = rowTexts.find(
          (text) => text.includes("sandbox-hobby") && text.includes("cancel_scheduled"),
        );
        return scheduledRowText?.includes("active") ? scheduledRowText : false;
      });

      run.detail_url = "/settings/billing";
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
      await resetContractState(app);
      await activateContract(app, hobbyPlan);

      run.detail_url = "/settings/billing?contracted=true";
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
      await resetContractState(app);
      await activateContract(app, hobbyPlan);
      await activateContract(app, proPlan);

      await app.waitForCondition("pro upgrade carries hobby forward", 30_000, async () => {
        await app.goto("/settings/billing");
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
        return activeProRowText && activeHobbyRows.length === 0 ? activeProRowText : false;
      });

      run.detail_url = "/settings/billing";
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

  test("period-end downgrade applies when billing business time reaches the next cycle", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      const fixture = await app.setBillingUserState({
        businessNow: contractFixtureNow(app),
        state: "pro",
      });

      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.goto("/settings/billing");
      run.started_balance = await app.readBalance();

      await app.assertStableRoute({
        path: "/settings/billing/subscribe",
        ready: app.page.getByRole("heading", { name: "Choose a plan" }),
        expectedText: ["Choose a Plan", hobbyPlan.displayName, hobbyPlan.priceText],
      });

      const redirect = await beginContractCheckout(app, hobbyPlan);
      if (redirect === "checkout") {
        throw new Error("period-end downgrade unexpectedly required Stripe checkout");
      }
      const downgradeEffectiveAt = contractEffectiveAtFromCurrentURL(app);

      const clock = await app.setBillingClock({
        orgID: fixture.org_id,
        reason: `${app.runID}-period-end-downgrade`,
        set: oneSecondAfter(downgradeEffectiveAt),
      });
      expect(clock.cycles_rolled_over).toBeGreaterThanOrEqual(1);
      expect(clock.contract_changes_applied).toBeGreaterThanOrEqual(1);
      expect(clock.entitlements_ensured).toBeGreaterThanOrEqual(1);

      await app.waitForCondition(
        "hobby active after period-end downgrade",
        shortTimeoutMS,
        async () => {
          await app.goto("/settings/billing");
          const activeHobbyRowText = (
            await contractRows(app, hobbyPlan.planID)
              .allInnerTexts()
              .catch(() => [])
          ).find(
            (text) =>
              text.includes(hobbyPlan.planID) && text.includes("active") && text.includes("paid"),
          );
          const activeProRows = (
            await contractRows(app, proPlan.planID)
              .allInnerTexts()
              .catch(() => [])
          ).filter(
            (text) =>
              text.includes(proPlan.planID) && text.includes("active") && text.includes("paid"),
          );

          if (activeHobbyRowText && activeProRows.length === 0) {
            return activeHobbyRowText;
          }

          return false;
        },
      );

      const document = await app.waitForCondition(
        "period-end billing document issued",
        shortTimeoutMS,
        async () => {
          const documents = await app.readBillingDocuments({ orgID: fixture.org_id });
          return (
            documents.find(
              (candidate) =>
                candidate.document_kind === "statement" &&
                candidate.status === "issued" &&
                candidate.payment_status === "n_a" &&
                candidate.finalization_id &&
                candidate.cycle_id,
            ) ?? false
          );
        },
      );
      expect(document.document_number).toMatch(/^FM-\d{4}-\d{6}$/);

      run.detail_url = "/settings/billing";
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

  test("paid-plan billing hero shows a real renewal date, not placeholder copy", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      // Stage: CEO on Pro with no pending changes — steady-state between
      // cycles, the shape every paying customer sees most of the time.
      await app.setBillingUserState({
        businessNow: contractFixtureNow(app),
        state: "pro",
      });

      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.goto("/settings/billing");

      const hero = app.page.getByTestId("plan-hero");
      await expect(hero).toBeVisible();

      // Why: data-account-kind is the machine-readable projection of the
      // BillingAccount discriminant. If it drifts from the rendered copy,
      // we have a split brain between deriveBillingAccount and the hero
      // template — a class of bug that looks right locally and wrong in
      // production. Catching it at the attribute level is cheap and exact.
      await expect(hero).toHaveAttribute("data-account-kind", "active");
      await expect(hero).toHaveAttribute("data-plan-id", "sandbox-pro");

      // Why: a regression here covers three different classes of bug at
      // once — BillingPlan.display_name dropped at the apiwire boundary,
      // the "active" branch of deriveBillingAccount misrouting to
      // no_contract, or the hero template flipping to the free variant
      // for an account that holds a contract. All three would land a
      // paying customer on the Free plan card.
      await expect(hero.getByRole("heading", { name: "Pro plan" })).toBeVisible();

      // Why: the monetary literal is hardcoded to the seed fixture so that
      // edits to plan pricing fail here instead of silently shipping to
      // customers. When this breaks, check marketing, subscribe page, and
      // the Stripe product catalog in the same commit.
      await expect(hero).toContainText("$20.00 / month");

      // Why: this is the assertion that would have caught the bug we
      // shipped today. If phase_end is NULL in contract_phases, or the
      // apiwire boundary drops the field, or the parser fails to hydrate
      // it, renewalLineFor() falls through to its placeholder branch
      // ("at the end of this cycle"). The customer then reads a card
      // that promises a renewal date and doesn't name one — worse than
      // saying nothing. The regex enforces "a real date is present"
      // without coupling the test to formatDateUTC's exact output.
      const renewal = hero.getByTestId("plan-hero-renewal");
      await expect(renewal).toHaveText(/Your subscription will auto-renew on \d{4}-\d{2}-\d{2}\./);

      run.detail_url = "/settings/billing";
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

  test("pending-downgrade billing hero names the target plan and the effective date", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      // Stage: CEO on Pro, then schedule a period-end downgrade to Hobby
      // through the real subscribe page. Driving the live flow (not a
      // fixture short-circuit) means a regression in the downgrade HANDLER
      // also surfaces here, not just the render layer — we test the pipe
      // end-to-end.
      await app.setBillingUserState({
        businessNow: contractFixtureNow(app),
        state: "pro",
      });

      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.goto("/settings/billing/subscribe");
      const redirect = await beginContractCheckout(app, hobbyPlan);
      if (redirect === "checkout") {
        throw new Error("period-end downgrade unexpectedly required Stripe checkout");
      }

      await app.goto("/settings/billing");

      const hero = app.page.getByTestId("plan-hero");
      await expect(hero).toBeVisible();

      // Why: during the grace period before a scheduled downgrade applies,
      // the customer still holds the CURRENT plan's entitlements. The hero
      // must continue to advertise the current plan as the headline so the
      // customer doesn't think their access changed the moment they
      // scheduled. A regression that flipped this to the target plan would
      // manufacture support tickets at the rate of one per downgrade.
      await expect(hero.getByRole("heading", { name: "Pro plan" })).toBeVisible();
      await expect(hero).toHaveAttribute("data-plan-id", "sandbox-pro");

      // Why: the discriminant and the copy must agree. This assertion
      // catches render bugs where the copy says "downgrades" but the
      // state is still "active" — split brain between the state machine
      // and the template is the ugliest billing-bug class to diagnose in
      // production because it usually looks right when an engineer opens
      // the page locally with a different fixture.
      await expect(hero).toHaveAttribute("data-account-kind", "pending_downgrade");

      // Why: the hero must name the target plan explicitly (so the
      // customer knows what they're downgrading TO) and the effective
      // date (so they know WHEN). A regression that dropped either
      // leaves the customer with ambient anxiety about an impending
      // change they can't see. The regex enforces both: target plan
      // name and a real date shape, without coupling to formatDateUTC's
      // exact output.
      const renewal = hero.getByTestId("plan-hero-renewal");
      await expect(renewal).toHaveText(/Downgrades to Hobby on \d{4}-\d{2}-\d{2}\./);

      run.detail_url = "/settings/billing";
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

  test("same-plan start resumes a scheduled downgrade without Stripe checkout", async ({ app }) => {
    const run = app.createRun();

    try {
      const fixture = await app.setBillingUserState({
        businessNow: contractFixtureNow(app),
        state: "pro",
      });

      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.goto("/settings/billing");
      run.started_balance = await app.readBalance();

      await app.assertStableRoute({
        path: "/settings/billing/subscribe",
        ready: app.page.getByRole("heading", { name: "Choose a plan" }),
        expectedText: ["Choose a Plan", hobbyPlan.displayName, hobbyPlan.priceText],
      });

      const downgradeRedirect = await beginContractCheckout(app, hobbyPlan);
      if (downgradeRedirect === "checkout") {
        throw new Error("period-end downgrade unexpectedly required Stripe checkout");
      }
      const downgradeEffectiveAt = contractEffectiveAtFromCurrentURL(app);

      await app.assertStableRoute({
        path: "/settings/billing/subscribe",
        ready: app.page.getByRole("heading", { name: "Choose a plan" }),
        expectedText: ["Choose a Plan", `Resume ${proPlan.displayName}`],
      });

      const resumeRedirect = await beginContractCheckout(app, proPlan);
      if (resumeRedirect === "checkout") {
        throw new Error("same-plan resume unexpectedly required Stripe checkout");
      }

      const clock = await app.setBillingClock({
        orgID: fixture.org_id,
        reason: `${app.runID}-period-end-after-resume`,
        set: oneSecondAfter(downgradeEffectiveAt),
      });
      expect(clock.cycles_rolled_over).toBeGreaterThanOrEqual(1);
      expect(clock.entitlements_ensured).toBeGreaterThanOrEqual(1);

      await app.waitForCondition(
        "pro remains active after resumed downgrade",
        shortTimeoutMS,
        async () => {
          await app.goto("/settings/billing");
          const activeProRowText = (
            await contractRows(app, proPlan.planID)
              .allInnerTexts()
              .catch(() => [])
          ).find(
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

          if (activeProRowText && activeHobbyRows.length === 0) {
            return activeProRowText;
          }

          return false;
        },
      );

      run.detail_url = "/settings/billing";
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

async function resetContractState(app: SandboxHarness) {
  await app.setBillingUserState({
    businessNow: contractFixtureNow(app),
    state: "free",
  });
}

function contractFixtureNow(app: SandboxHarness) {
  const seconds = stableSecondOffset(app.runID);
  return new Date(Date.UTC(2026, 3, 13, 0, 0, seconds)).toISOString().replace(".000Z", "Z");
}

function stableSecondOffset(value: string) {
  let hash = 0;
  for (const char of value) {
    hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  }
  return hash % (12 * 60 * 60);
}

function contractEffectiveAtFromCurrentURL(app: SandboxHarness) {
  const raw = new URL(app.page.url()).searchParams.get("contractEffectiveAt");
  if (!raw) {
    throw new Error(`expected contractEffectiveAt in ${app.page.url()}`);
  }
  const effectiveAt = new Date(raw);
  if (Number.isNaN(effectiveAt.getTime())) {
    throw new Error(`invalid contractEffectiveAt ${raw}`);
  }
  return effectiveAt;
}

function oneSecondAfter(value: Date) {
  return new Date(value.getTime() + 1_000).toISOString().replace(".000Z", "Z");
}

async function activateContract(app: SandboxHarness, plan: ContractPlanSpec) {
  await app.expectSSRHTML("/settings/billing/subscribe", [
    "Choose a Plan",
    plan.displayName,
    plan.priceText.replace("/mo", ""),
    "/mo",
  ]);
  await app.assertStableRoute({
    path: "/settings/billing/subscribe",
    ready: app.page.getByRole("heading", { name: "Choose a plan" }),
    expectedText: ["Choose a Plan", plan.displayName, plan.priceText],
  });

  const redirect = await beginContractCheckout(app, plan);
  if (redirect === "checkout") {
    await completeStripeCheckout(app, { returnURLIncludes: "/settings/billing?contracted=true" });
  }
  await app.expectSSRHTML("/settings/billing?contracted=true", [
    "Plan checkout complete",
    "Contracts",
  ]);
  await app.goto("/settings/billing?contracted=true");

  return await app.waitForCondition(`${plan.planID} contract activation`, 30_000, async () => {
    await app.goto("/settings/billing?contracted=true");
    const rowTexts = await contractRows(app, plan.planID)
      .allInnerTexts()
      .catch(() => []);
    const activeRowText = rowTexts.find(
      (text) => text.includes(plan.planID) && text.includes("active") && text.includes("paid"),
    );
    return activeRowText ?? false;
  });
}

async function beginCreditCheckout(app: SandboxHarness, buttonName: RegExp) {
  if (app.page.url().includes("checkout.stripe.com")) {
    return;
  }
  const checkoutButton = app.page.getByRole("button", { name: buttonName });
  await expect(checkoutButton).toBeVisible({ timeout: shortTimeoutMS });
  await expect(checkoutButton).toBeEnabled({ timeout: shortTimeoutMS });
  await checkoutButton.click();
  await app.page.waitForURL(/checkout\.stripe\.com/, { timeout: 30_000 });
}

async function beginContractCheckout(
  app: SandboxHarness,
  plan: ContractPlanSpec,
): Promise<"checkout" | "billing"> {
  if (app.page.url().includes("checkout.stripe.com")) {
    return "checkout";
  }
  if (isContractedBillingURL(app.page.url())) {
    return "billing";
  }

  const subscribeButton = app.page.getByTestId(`start-contract-plan-${plan.planID}`);
  await expect(subscribeButton).toBeVisible({ timeout: shortTimeoutMS });

  // A disabled button carrying "Current plan" copy means the user is already
  // on this plan — we short-circuit to the billing view because there is no
  // checkout to wait for.
  if (!(await subscribeButton.isEnabled().catch(() => false))) {
    const buttonText = await subscribeButton.innerText().catch(() => "");
    if (buttonText.includes("Current plan")) {
      await app.goto("/settings/billing?contracted=true");
      return "billing";
    }
    await expect(subscribeButton).toBeEnabled({ timeout: shortTimeoutMS });
  }

  await subscribeButton.click();
  // The handler either redirects to Stripe or to /settings/billing?contracted=true
  // — wait for whichever lands first. A legal post-click URL that still renders
  // AppNotFound would otherwise sail past this predicate as a false green, so
  // we also assert the app didn't land on the 404 boundary.
  await app.page.waitForURL(
    (url) =>
      url.toString().includes("checkout.stripe.com") || isContractedBillingURL(url.toString()),
    { timeout: 30_000 },
  );
  if (!app.page.url().includes("checkout.stripe.com")) {
    await expect(
      app.page.getByRole("heading", { name: "Not found" }),
      "plan card click landed on the app-level 404 boundary — check that billing mutations redirect under /settings/billing",
    ).toHaveCount(0);
  }
  return app.page.url().includes("checkout.stripe.com") ? "checkout" : "billing";
}

function isContractedBillingURL(rawURL: string) {
  return rawURL.includes("/settings/billing") && rawURL.includes("contracted=true");
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
