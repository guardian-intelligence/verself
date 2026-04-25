import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Console Source", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("creates a private source repository and reads Forgejo-backed refs and tree", async ({
    app,
  }) => {
    const run = app.createRun();
    const suffix = app.runID.replace(/[^A-Za-z0-9]/g, "").slice(-10) || "proof";
    const repoName = `source-proof-${suffix.toLowerCase()}`;
    const description = `Source proof ${app.runID}`;

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();

      run.detail_url = "/source";
      await app.expectSSRHTML("/source", ["Source", "New repository", "Repositories"]);
      await app.assertStableRoute({
        path: "/source",
        ready: app.page.getByRole("heading", { name: "Source", exact: true }),
        stableContent: app.page.locator("main").last(),
        expectedText: ["New repository", "Repositories"],
      });

      await app.page.getByLabel("Name").fill(repoName);
      await app.page.getByLabel("Default branch").fill("main");
      await app.page.getByLabel("Description").fill(description);
      await app.page.getByRole("button", { name: "Create", exact: true }).click();

      await expect(app.page).toHaveURL(/\/source\/[0-9a-f-]{36}$/i, { timeout: shortTimeoutMS });
      const repoID = new URL(app.page.url()).pathname.split("/").at(-1) ?? "";
      if (!/^[0-9a-f-]{36}$/i.test(repoID)) {
        throw new Error(`created source route did not expose a repo UUID: ${app.page.url()}`);
      }

      run.repo_id = repoID;
      run.detail_url = `/source/${repoID}`;

      await expect(app.page.getByRole("heading", { name: repoName, exact: true })).toBeVisible({
        timeout: shortTimeoutMS,
      });
      await expect(app.page.getByText(description)).toBeVisible({ timeout: shortTimeoutMS });
      await expect(app.page.getByRole("heading", { name: "Refs", exact: true })).toBeVisible();
      await expect(app.page.getByRole("heading", { name: "Tree", exact: true })).toBeVisible();

      await expect
        .poll(async () => app.readText(app.page.locator("main").last()), {
          timeout: shortTimeoutMS,
        })
        .toContain("main");
      await expect
        .poll(async () => app.readText(app.page.locator("main").last()), {
          timeout: shortTimeoutMS,
        })
        .toMatch(/README\.md|Empty path/);
      await app.page.screenshot({
        path: app.testInfo.outputPath("source-created.png"),
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
