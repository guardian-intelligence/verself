import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Source", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("shows project-scoped source setup or repository cards", async ({ app }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/source";
      await app.expectSSRHTML("/source", ["Source", "Repositories"]);
      await app.assertStableRoute({
        path: "/source",
        ready: app.page.getByRole("heading", { name: "Source", exact: true }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["Repositories"],
      });

      const createProject = app.page.getByRole("heading", { name: "Create a project" });
      const createRepository = app.page.getByRole("heading", { name: "Create a repository" });
      const pushFirstBranch = app.page.getByRole("heading", { name: "Push the first branch" });
      if (await createProject.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(app.page.getByRole("button", { name: "Create project" })).toBeVisible();
        run.source_state = "no_projects";
      } else if (await createRepository.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(app.page.getByRole("button", { name: "Create repository" })).toBeVisible();
        run.source_state = "no_repository";
      } else if (await pushFirstBranch.isVisible({ timeout: shortTimeoutMS }).catch(() => false)) {
        await expect(app.page.getByText("Git remote", { exact: true }).first()).toBeVisible();
        const credentialButton = app.page
          .getByRole("button", { name: "Create Git credential" })
          .first();
        await expect(credentialButton).toBeVisible();
        await app.waitForCondition("git credential creation", 10_000, async () => {
          if (
            await app.page
              .getByText("Username", { exact: true })
              .isVisible()
              .catch(() => false)
          ) {
            return true;
          }
          if (!(await credentialButton.isVisible().catch(() => false))) {
            return false;
          }
          // TanStack Start can expose SSR HTML before the client click handler is hydrated.
          await credentialButton.click();
          return await app.page
            .getByText("Username", { exact: true })
            .isVisible()
            .catch(() => false);
        });
        await expect(app.page.getByText("Username", { exact: true })).toBeVisible({
          timeout: shortTimeoutMS,
        });
        await expect(app.page.getByText("Token", { exact: true })).toBeVisible();
        run.source_state = "push_empty";
      } else {
        await expect(
          app.page.getByRole("heading", { name: "Branches", exact: true }).first(),
        ).toBeVisible();
        await expect(app.page.getByText("active branches").first()).toBeVisible();
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
