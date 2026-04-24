import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Rent-a-Sandbox Profile", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("profile settings persist identity and preference changes through profile-service", async ({
    app,
  }) => {
    const run = app.createRun();
    const suffix = app.runID.replace(/[^A-Za-z0-9]/g, "").slice(-10) || "proof";
    const givenName = `Profile${suffix}`;
    const familyName = "Proof";
    const displayName = `${givenName} ${familyName}`;
    const target = {
      defaultSurface: process.env.PROFILE_PROOF_DEFAULT_SURFACE || "schedules",
      locale: process.env.PROFILE_PROOF_LOCALE || "en-GB",
      theme: process.env.PROFILE_PROOF_THEME || "dark",
      timeDisplay: process.env.PROFILE_PROOF_TIME_DISPLAY || "local",
      timezone: process.env.PROFILE_PROOF_TIMEZONE || "America/New_York",
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

      await app.page.getByLabel("Given name").fill(givenName);
      await app.page.getByLabel("Family name").fill(familyName);
      await app.page.getByLabel("Display name").fill(displayName);
      await app.page.getByRole("button", { name: /save identity/i }).click();
      await expect(app.page.getByRole("button", { name: /save identity/i })).toBeDisabled({
        timeout: shortTimeoutMS,
      });

      await app.page.getByLabel("Locale").selectOption(target.locale);
      await app.page.getByLabel("Time zone").selectOption(target.timezone);
      await app.page.getByLabel("Time display").selectOption(target.timeDisplay);
      await app.page.getByLabel("Theme").selectOption(target.theme);
      await app.page.getByLabel("Default surface").selectOption(target.defaultSurface);
      await app.page.getByRole("button", { name: /save preferences/i }).click();
      await expect(app.page.getByRole("button", { name: /save preferences/i })).toBeDisabled({
        timeout: shortTimeoutMS,
      });

      await app.page.reload({ waitUntil: "domcontentloaded" });
      await expect(app.page.getByLabel("Given name")).toHaveValue(givenName);
      await expect(app.page.getByLabel("Family name")).toHaveValue(familyName);
      await expect(app.page.getByLabel("Display name")).toHaveValue(displayName);
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
