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
        const firstRow = rows.first();
        await expect(firstRow.getByTestId("build-repository-slug")).toContainText("/");

        const repoId = await firstRow.getAttribute("data-repo-id");
        const repoSlug = await firstRow.getAttribute("data-repo-slug");
        const activeBuildCountText = await firstRow.getAttribute("data-active-build-count");
        expect(repoId, "repository row must expose a repo id for the builds route").toBeTruthy();
        expect(repoSlug, "repository row must expose the slug shown to users").toContain("/");
        expect(
          activeBuildCountText,
          "repository row must expose the live active build count",
        ).toMatch(/^\d+$/);

        const activeBuildCount = Number.parseInt(activeBuildCountText ?? "", 10);
        const activeBuildLabel = `${activeBuildCount} ${
          activeBuildCount === 1 ? "active build" : "active builds"
        }`;
        await expect(firstRow).toContainText(activeBuildLabel);
        const activeLink = firstRow.getByTestId("build-active-link");
        if (activeBuildCount === 0) {
          await expect(activeLink).toHaveCount(0);
        } else if (activeBuildCount === 1) {
          const href = await activeLink.getAttribute("href");
          expect(href).toContain("/executions/");
        } else {
          const href = await activeLink.getAttribute("href");
          expect(href).toContain(`/builds/${repoId}`);
        }

        await app.expectSSRHTML(`/builds/${repoId}`, ["Builds", repoSlug ?? ""]);
        await app.assertStableRoute({
          path: `/builds/${repoId}`,
          ready: app.page.getByRole("heading", { name: repoSlug ?? "", exact: true }),
          stableContent: app.page.locator("main").last(),
          expectedText: ["Active Builds"],
        });
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
