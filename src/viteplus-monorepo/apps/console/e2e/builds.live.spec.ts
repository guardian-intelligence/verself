import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Builds", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("shows simple repository build rows", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/builds";
      await app.expectSSRHTML("/builds", ["Builds", "Repositories"]);
      await app.assertStableRoute({
        path: "/builds",
        ready: app.page.getByRole("heading", { name: "Builds", exact: true }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["Repositories"],
      });

      const emptyState = app.page.getByText("No repositories", { exact: true });
      if (await emptyState.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(app.page.getByText("Add a repository to run builds.")).toBeVisible();
        run.builds_state = "no_repositories";
      } else {
        const rows = app.page.getByTestId("build-repository-row");
        await expect(rows.first()).toBeVisible();
        await expect(rows.first().getByTestId("build-repository-slug")).toContainText("/");
        await expect(rows.first().getByTestId("build-active-count")).toContainText(
          /active builds?$/,
        );
        run.builds_state = "repositories";
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
