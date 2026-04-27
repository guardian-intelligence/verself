import type { BrowserContext, Locator, Page } from "@playwright/test";
import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Profile", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("profile settings persist identity and preference changes through profile-service", async ({
    app,
  }) => {
    const run = app.createRun();
    const suffix = app.runID.replace(/[^A-Za-z0-9]/g, "").slice(-10) || "smoke";
    const givenName = `Profile${suffix}`;
    const familyName = "Smoke Test";
    const displayName = `${givenName} ${familyName}`;
    const initialDisplayName = `${displayName} Initial`;
    const conflictRemoteDisplayName = `${displayName} Remote`;
    const conflictLocalDisplayName = displayName;
    const target = {
      defaultSurface: process.env.PROFILE_SMOKE_TEST_DEFAULT_SURFACE || "schedules",
      locale: process.env.PROFILE_SMOKE_TEST_LOCALE || "en-GB",
      theme: process.env.PROFILE_SMOKE_TEST_THEME || "dark",
      timeDisplay: process.env.PROFILE_SMOKE_TEST_TIME_DISPLAY || "local",
      timezone: process.env.PROFILE_SMOKE_TEST_TIMEZONE || "America/New_York",
    };

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/settings/profile";
      await app.expectSSRHTML("/settings/profile", ["Identity", "Preferences"]);
      await app.assertStableRoute({
        path: "/settings/profile",
        ready: app.page.getByRole("heading", { name: "Identity" }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["Identity", "Preferences", "Time display"],
      });

      await expect(app.page.getByTestId("settings-tab-profile")).toHaveAttribute(
        "data-status",
        "active",
      );

      const syncStatus = app.page.getByTestId("profile-sync-status");
      await expect(syncStatus).toContainText("Last synced");
      await expect(
        app.page.getByRole("button", { name: /save (identity|preferences)/i }),
      ).toHaveCount(0);
      await expect(app.page.locator("form button:disabled")).toHaveCount(0);

      const initialSyncedAt = await syncStatus.getAttribute("data-synced-at");
      await app.page.getByLabel("Given name").fill(givenName);
      await app.page.getByLabel("Family name").fill(familyName);
      await app.page.getByLabel("Display name").fill(initialDisplayName);
      await app.page.getByLabel("Locale").focus();
      const identitySyncedAt = await waitForProfileSync(syncStatus, initialSyncedAt);
      await expect(app.page.getByTestId("profile-sync-status")).toContainText(
        /Last synced (Just now|Less than a minute ago)/,
      );
      await expect(app.page.getByTestId("profile-sync-status")).not.toContainText("UTC");
      await expect(app.page.getByTestId("shell-account-display-name")).toContainText(
        initialDisplayName,
      );

      const releaseServerFunctions = await holdServerFunctions(app.page);
      await app.goto("/executions");
      await expect(app.page.getByTestId("shell-account-trigger")).toBeVisible({
        timeout: shortTimeoutMS,
      });
      const accountLabel = app.page.getByTestId("shell-account-display-name");
      try {
        await expect(accountLabel).toHaveAttribute("data-account-source", "pending");
        await expect(accountLabel).toHaveText("");
      } finally {
        await releaseServerFunctions();
      }
      await expect(accountLabel).toHaveAttribute("data-account-source", "profile");
      await expect(accountLabel).toContainText(initialDisplayName);
      await app.goto("/settings/profile");
      await expect(app.page.getByRole("heading", { name: "Identity" })).toBeVisible({
        timeout: shortTimeoutMS,
      });

      const releaseProfileReads = await holdServerFunctionReads(app.page);
      try {
        await updateDisplayNameInParallel(app.context, conflictRemoteDisplayName);
        await app.page.getByLabel("Display name").fill(conflictLocalDisplayName);
        await app.page.getByLabel("Locale").focus();
        await expect(app.page.getByTestId("profile-identity-sync-error")).toContainText(
          "Profile changed elsewhere",
        );
        await expect(app.page.getByLabel("Display name")).toHaveValue(conflictLocalDisplayName);
      } finally {
        await releaseProfileReads();
      }
      await app.page.getByTestId("profile-identity-sync-latest").click();
      const conflictSyncedAt = await waitForProfileSync(syncStatus, identitySyncedAt);
      await expect(app.page.getByTestId("profile-identity-sync-error")).toHaveCount(0);
      await expect(app.page.getByLabel("Display name")).toHaveValue(conflictLocalDisplayName);
      await expect(app.page.getByTestId("shell-account-display-name")).toContainText(
        conflictLocalDisplayName,
      );

      await app.page.getByLabel("Locale").selectOption(target.locale);
      await app.page.getByLabel("Time zone").selectOption(target.timezone);
      await app.page.getByLabel("Time display").selectOption(target.timeDisplay);
      await app.page.getByLabel("Theme").selectOption(target.theme);
      await app.page.getByLabel("Default surface").selectOption(target.defaultSurface);
      await waitForProfileSync(syncStatus, conflictSyncedAt);

      await app.page.reload({ waitUntil: "domcontentloaded" });
      await expect(app.page.getByLabel("Given name")).toHaveValue(givenName);
      await expect(app.page.getByLabel("Family name")).toHaveValue(familyName);
      await expect(app.page.getByLabel("Display name")).toHaveValue(conflictLocalDisplayName);
      await expect(app.page.getByLabel("Locale")).toHaveValue(target.locale);
      await expect(app.page.getByLabel("Time zone")).toHaveValue(target.timezone);
      await expect(app.page.getByLabel("Time display")).toHaveValue(target.timeDisplay);
      await expect(app.page.getByLabel("Theme")).toHaveValue(target.theme);
      await expect(app.page.getByLabel("Default surface")).toHaveValue(target.defaultSurface);
      await app.assertHealthy();

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

async function holdServerFunctionReads(page: Page): Promise<() => Promise<void>> {
  let release: (() => void) | undefined;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  const handler: Parameters<Page["route"]>[1] = async (route) => {
    if (route.request().method() === "GET") {
      await released;
    }
    await route.fallback();
  };

  await page.route("**/_serverFn/**", handler);

  return async () => {
    release?.();
    await page.unroute("**/_serverFn/**", handler);
  };
}

async function holdServerFunctions(page: Page): Promise<() => Promise<void>> {
  let release: (() => void) | undefined;
  const released = new Promise<void>((resolve) => {
    release = resolve;
  });
  const handler: Parameters<Page["route"]>[1] = async (route) => {
    await released;
    await route.fallback();
  };

  await page.route("**/_serverFn/**", handler);

  return async () => {
    release?.();
    await page.unroute("**/_serverFn/**", handler);
  };
}

async function updateDisplayNameInParallel(
  context: BrowserContext,
  displayName: string,
): Promise<void> {
  const page = await context.newPage();
  try {
    await page.goto("/settings/profile", { waitUntil: "domcontentloaded" });
    await expect(page.getByRole("heading", { name: "Identity" })).toBeVisible({
      timeout: shortTimeoutMS,
    });

    const syncStatus = page.getByTestId("profile-sync-status");
    const previousSyncedAt = await syncStatus.getAttribute("data-synced-at");
    await page.getByLabel("Display name").fill(displayName);
    await expect(page.getByLabel("Display name")).toHaveValue(displayName);
    await page.getByTestId("profile-identity-form").evaluate((form: HTMLFormElement) => {
      form.requestSubmit();
    });
    await waitForProfileSync(syncStatus, previousSyncedAt);
  } finally {
    await page.close();
  }
}

async function waitForProfileSync(
  syncStatus: Locator,
  previousSyncedAt: string | null,
): Promise<string | null> {
  await expect
    .poll(async () => syncStatus.getAttribute("data-synced-at"), {
      intervals: [100],
      timeout: shortTimeoutMS,
    })
    .not.toBe(previousSyncedAt ?? "");
  await expect(syncStatus).toHaveAttribute("data-sync-state", "idle", {
    timeout: shortTimeoutMS,
  });
  return syncStatus.getAttribute("data-synced-at");
}
