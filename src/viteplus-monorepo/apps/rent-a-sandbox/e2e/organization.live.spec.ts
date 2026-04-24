import { ensureTestUserExists, expect, test } from "./harness";

test.describe("Rent-a-Sandbox Organization", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("organization management route renders the capability switchboard from identity-service", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.expectSSRHTML("/settings/organization", [
        "Invite member",
        "Members",
        "Member capabilities",
        "Deploy executions",
        "Invite members",
        "View billing",
      ]);
      await app.assertStableRoute({
        path: "/settings/organization",
        ready: app.page.getByRole("heading", { name: "Member capabilities" }),
        expectedText: ["Invite member", "Members", "Member capabilities", "Deploy executions"],
      });

      await expect(app.page.getByRole("button", { name: "Invite" })).toBeEnabled();
      await expect(app.page.getByRole("button", { name: /^save/i })).toHaveCount(0);
      await expect(app.page.locator("button:disabled")).toHaveCount(0);
      await expect(app.page.getByRole("switch", { name: "Deploy executions" })).toBeChecked();

      run.detail_url = "/settings/organization";
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
