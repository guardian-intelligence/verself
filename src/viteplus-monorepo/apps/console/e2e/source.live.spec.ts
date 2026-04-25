import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Source", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("shows the headless source repository card or push-first setup", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/source";
      await app.expectSSRHTML("/source", ["Source", "Project repository"]);
      await app.assertStableRoute({
        path: "/source",
        ready: app.page.getByRole("heading", { name: "Source", exact: true }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["Project repository"],
      });

      const emptyState = app.page.getByRole("heading", { name: "Push the first branch" });
      if (await emptyState.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(app.page.getByText("Git remote", { exact: true })).toBeVisible();
        await expect(app.page.getByRole("button", { name: "Create Git credential" })).toBeVisible();
        await app.page.getByRole("button", { name: "Create Git credential" }).click();
        await expect(app.page.getByText("Username", { exact: true })).toBeVisible({
          timeout: shortTimeoutMS,
        });
        await expect(app.page.getByText("Token", { exact: true })).toBeVisible();
        run.source_state = "empty";
      } else {
        await expect(
          app.page.getByRole("heading", { name: "Branches", exact: true }),
        ).toBeVisible();
        await expect(app.page.getByRole("heading", { name: "CI jobs", exact: true })).toBeVisible();
        run.source_state = "repositories";
      }

      await app.page.screenshot({
        path: app.testInfo.outputPath("source-headless.png"),
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
