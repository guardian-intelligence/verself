import { ensureTestUserExists, expect, test } from "./harness";

test.describe("Rent-a-Sandbox Organization", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("organization management route renders from the identity service", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      await app.expectSSRHTML("/organization", [
        "Invite member",
        "Members",
        "Policy",
        "identity-service",
        "identity:policy:write",
      ]);
      await app.assertStableRoute({
        path: "/organization",
        ready: app.page.getByRole("heading", { name: "Policy" }),
        expectedText: [
          "Invite member",
          "Members",
          "Policy",
          "forge_org_owner",
          "identity-service",
          "identity:policy:write",
        ],
      });

      await expect(app.page.getByRole("button", { name: "Invite member" })).toBeEnabled();
      await expect(app.page.getByRole("button", { name: "Save policy" })).toBeEnabled();

      run.detail_url = "/organization";
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
