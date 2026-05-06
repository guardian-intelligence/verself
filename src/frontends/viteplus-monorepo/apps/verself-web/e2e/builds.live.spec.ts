import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Builds", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("shows deployments dashboard rows", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/builds";
      await app.expectSSRHTML("/builds", ["Deployments", "Select Date Range"]);
      await app.assertStableRoute({
        path: "/builds",
        ready: app.page.getByRole("button", { name: "Select Date Range", exact: true }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["Deployments", "Select Date Range", "Status"],
      });
      expect(new URL(app.page.url()).searchParams.get("from")).toMatch(/^\d{4}-\d{2}-\d{2}$/);
      expect(new URL(app.page.url()).searchParams.get("to")).toMatch(/^\d{4}-\d{2}-\d{2}$/);

      const emptyState = app.page.getByText("No deployments found", { exact: true });
      if (await emptyState.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(
          app.page.getByText("New GitHub runner and scheduled sandbox runs"),
        ).toBeVisible();
        run.builds_state = "no_deployments";
      } else {
        const rows = app.page.getByTestId("build-run-row");
        await expect(rows.first()).toBeVisible();
        const firstRow = rows.first();
        await expect(firstRow.getByTestId("build-run-link")).toHaveAttribute(
          "href",
          /\/executions\//,
        );
        await expect(firstRow.getByTestId("build-run-status")).toContainText(
          /Ready|Error|Building|Queued|Canceled/i,
        );
        run.builds_state = "deployments";
      }

      await app.page.screenshot({
        path: app.testInfo.outputPath("builds-headless.png"),
        fullPage: true,
      });

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
