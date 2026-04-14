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
        "Manage repositories",
        "Invite members",
        "View billing",
      ]);
      await app.assertStableRoute({
        path: "/settings/organization",
        ready: app.page.getByRole("heading", { name: "Member capabilities" }),
        expectedText: [
          "Invite member",
          "Members",
          "Member capabilities",
          "Deploy executions",
          "Manage repositories",
          "Owner",
        ],
      });

      await expect(app.page.getByRole("button", { name: "Invite member" })).toBeEnabled();
      // Save capabilities starts disabled until the switchboard becomes dirty.
      // Asserting visibility is enough here; a separate test will exercise
      // toggle → save → reload persistence once stable.
      await expect(app.page.getByRole("button", { name: "Save capabilities" })).toBeVisible();

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
