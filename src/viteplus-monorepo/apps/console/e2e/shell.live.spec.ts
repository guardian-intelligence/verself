import { env } from "./env";
import { ensureTestUserExists, expect, test } from "./harness";

test.describe("Console Shell", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("unauthenticated root redirects into the sign-in flow", async ({ app }) => {
    const run = app.createRun();

    try {
      run.detail_url = "/";

      await app.goto("/");
      // The authenticated app shell owns every chrome surface, and `/`
      // now redirects into either `/executions` (authed) or `/login`
      // (guest). A guest opening `/` should bounce to Zitadel; we assert
      // the URL lands on the Zitadel origin.
      await app.waitForCondition("guest redirect to Zitadel", 10_000, async () => {
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

  test("authenticated shell lands on executions and navigates via the rail", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/";
      run.started_balance = await app.readBalance();

      // `/` redirects to `/executions`; the shell omnibar is our stable
      // marker that the chrome mounted on the server side.
      await app.expectSSRHTML("/executions", ["Executions", "Search or jump"]);
      await app.assertStableRoute({
        path: "/executions",
        ready: app.page.getByTestId("shell-omnibar"),
        stableContent: app.page.locator("body"),
        expectedText: ["Executions", "Search or jump to"],
      });

      // Navigate from Executions to the self-scoped Settings landing surface via the evergreen rail.
      await app.page.getByTestId("nav-settings").click();
      await expect(app.page).toHaveURL(/\/settings\/profile$/);
      await expect(app.page.getByTestId("settings-tab-profile")).toHaveAttribute(
        "data-status",
        "active",
      );

      // Click the "Executions" rail item back to the product.
      await app.page.getByTestId("nav-executions").click();
      await expect(app.page).toHaveURL(/\/executions$/);

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

  test("authenticated shell navigates to schedules via the rail", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();
      await app.goto("/executions");
      run.detail_url = "/executions";

      await app.page.getByTestId("nav-schedules").click();
      await expect(app.page).toHaveURL(/\/schedules$/);
      await expect(app.page.getByRole("heading", { name: "Schedules", exact: true })).toBeVisible();
      await expect(
        app.page.getByText("Recurring source workflow dispatches backed by Temporal."),
      ).toBeVisible();

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

  test("authenticated shell navigates to source via the rail", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();
      await app.goto("/executions");
      run.detail_url = "/executions";

      await app.page.getByTestId("nav-source").click();
      await expect(app.page).toHaveURL(/\/source$/);
      await expect(app.page.getByRole("heading", { name: "Source", exact: true })).toBeVisible();
      await expect(
        app.page.getByRole("heading", { name: "Project repository", exact: true }),
      ).toBeVisible();

      const emptyStateVisible = await app.page
        .getByText("Push the first branch", { exact: true })
        .isVisible({ timeout: 1000 })
        .catch(() => false);
      const repositoryCardVisible = await app.page
        .getByText(/active branches/)
        .first()
        .isVisible({ timeout: 1000 })
        .catch(() => false);
      expect(emptyStateVisible || repositoryCardVisible).toBe(true);

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

  test("command palette opens with Cmd+K and jumps to Billing", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();
      await app.goto("/executions");
      run.detail_url = "/executions";

      const isMac = process.platform === "darwin";
      await app.page.keyboard.press(isMac ? "Meta+K" : "Control+K");
      await expect(app.page.getByTestId("command-palette-input")).toBeVisible();

      await app.page.getByTestId("command-palette-input").fill("bill");
      await expect(app.page.getByTestId("command-palette-item-settings:billing")).toBeVisible();
      await app.page.keyboard.press("Enter");

      await expect(app.page).toHaveURL(/\/settings\/billing$/);

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
