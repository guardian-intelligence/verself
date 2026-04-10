import { env } from "./env";
import { ensureTestUserExists, expect, test } from "./harness";

test.describe("Rent-a-Sandbox Shell", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("guest shell is server-rendered and protected routes redirect to auth", async ({ app }) => {
    const run = app.createRun();

    try {
      run.detail_url = "/";

      await app.expectSSRHTML("/", ["Rent-a-Sandbox", "Sign in to manage sandboxes"]);
      await app.assertStableRoute({
        path: "/",
        ready: app.page.getByText("Sign in to manage sandboxes"),
        expectedText: ["Rent-a-Sandbox", "Sign in to manage sandboxes", "Firecracker CI sandboxes"],
        exactText: true,
      });

      await app.page.getByRole("link", { name: "Repos", exact: true }).click();
      await app.waitForCondition("protected route redirect", 10_000, async () => {
        return app.page.url().startsWith(env.zitadelBaseURL) ? true : false;
      });

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

  test("authenticated dashboard shell preserves SSR through hydration", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/";
      run.started_balance = await app.readBalance();

      await app.expectSSRHTML("/", ["Dashboard", "Available Credits"]);
      await app.assertStableRoute({
        path: "/",
        ready: app.page.getByRole("heading", { name: "Dashboard" }),
        expectedText: ["Dashboard", "Available Credits", "Repos", "Executions"],
        exactText: true,
      });

      await app.page.getByRole("link", { name: "Billing", exact: true }).click();
      await expect(app.page).toHaveURL(/\/billing$/);
      await expect(app.page.getByRole("heading", { name: "Billing" })).toBeVisible();

      await app.page.getByRole("link", { name: "Dashboard", exact: true }).click();
      await expect(app.page).toHaveURL(/\/$/);
      await expect(app.page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

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
