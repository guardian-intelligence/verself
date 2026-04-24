import { ensureTestUserExists, expect, shortTimeoutMS, test } from "./harness";

test.describe("Rent-a-Sandbox Notifications", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("notification bell receives a synthetic realtime notification and advances the read cursor", async ({
    app,
  }) => {
    const run = app.createRun();

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();
      run.detail_url = "/executions";

      await app.goto("/executions");
      const bell = app.page.getByTestId("notifications-bell");
      await expect(bell).toBeVisible({ timeout: shortTimeoutMS });
      await bell.click();
      await expect(app.page.getByTestId("notifications-popover")).toBeVisible({
        timeout: shortTimeoutMS,
      });

      const firstRow = app.page.getByTestId("notification-row").first();
      const previousFirstID =
        (await firstRow.getAttribute("data-notification-id").catch(() => "")) ?? "";

      await app.page.getByTestId("notifications-test").click();
      await expect
        .poll(
          async () => (await firstRow.getAttribute("data-notification-id").catch(() => "")) ?? "",
          { timeout: shortTimeoutMS },
        )
        .not.toBe(previousFirstID);
      await expect(firstRow).toContainText("Notification test", { timeout: shortTimeoutMS });

      await app.page.getByTestId("notifications-mark-read").click();
      await expect(app.page.getByTestId("notifications-unread-count")).toHaveCount(0, {
        timeout: shortTimeoutMS,
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
});
