import { ensureTestUserExists, expect, pollIntervalMS, shortTimeoutMS, test } from "./harness";

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
      await expect(
        app.page.locator('[data-testid="notifications-popover"] button:disabled'),
      ).toHaveCount(0);
      await expect(app.page.getByTestId("notifications-error")).toHaveCount(0);

      const rows = app.page.getByTestId("notification-row");
      const firstRow = rows.first();
      const loadingRows = app.page.getByTestId("notifications-loading");
      if ((await rows.count()) === 0) {
        await app.page.getByTestId("notifications-test").click();
        await expect.poll(async () => rows.count(), { timeout: shortTimeoutMS }).toBeGreaterThan(0);
        await expect(firstRow).toContainText("Notification test", { timeout: shortTimeoutMS });
      }

      await expect(loadingRows).toHaveCount(0, { timeout: shortTimeoutMS });
      const previousRowCount = await rows.count();
      const previousFirstID =
        (await firstRow.getAttribute("data-notification-id").catch(() => "")) ?? "";

      await app.page.getByTestId("notifications-test").click();

      let currentFirstID = previousFirstID;
      const deadline = Date.now() + shortTimeoutMS;
      while (Date.now() < deadline) {
        const loadingCount = await loadingRows.count();
        if (loadingCount > 0) {
          throw new Error("notifications list showed a loading state while sending");
        }
        const currentRowCount = await rows.count();
        if (currentRowCount < previousRowCount) {
          throw new Error("notifications list dropped existing rows while sending");
        }
        currentFirstID =
          (await firstRow.getAttribute("data-notification-id").catch(() => "")) ?? "";
        if (currentFirstID && currentFirstID !== previousFirstID) {
          break;
        }
        await app.page.waitForTimeout(pollIntervalMS);
      }
      expect(currentFirstID).not.toBe(previousFirstID);
      await expect(firstRow).toContainText("Notification test", { timeout: shortTimeoutMS });
      await expect(firstRow.getByTestId("notification-created-at")).toContainText("Just now", {
        timeout: shortTimeoutMS,
      });
      await expect
        .poll(async () => firstRow.getByTestId("notification-created-at").innerText(), {
          timeout: shortTimeoutMS,
        })
        .toBe("Less than a minute ago");

      await app.page.getByTestId("notifications-mark-read").click();
      await expect(app.page.getByTestId("notifications-unread-count")).toHaveCount(0, {
        timeout: shortTimeoutMS,
      });

      await app.goto("/notifications");
      await expect(app.page.getByRole("heading", { name: "Notifications" })).toBeVisible({
        timeout: shortTimeoutMS,
      });
      await expect(app.page.getByTestId("notifications-filter-unread")).toHaveAttribute(
        "aria-pressed",
        "true",
      );
      await expect(app.page.locator("main button:disabled")).toHaveCount(0);
      await app.page.getByTestId("notifications-test").click();
      await expect
        .poll(async () => app.page.getByTestId("notification-row").count(), {
          timeout: shortTimeoutMS,
        })
        .toBeGreaterThan(0);
      await app.page.getByTestId("notifications-clear-all").click();
      await expect(app.page.getByTestId("notifications-empty")).toHaveText(
        "No unread notifications",
        {
          timeout: shortTimeoutMS,
        },
      );

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

  test("notification read cursor live-syncs across browser tabs", async ({ app }) => {
    const run = app.createRun();
    let secondTab: Awaited<ReturnType<typeof app.context.newPage>> | null = null;

    try {
      await app.ensureLoggedIn();
      app.resetBrowserSignals();
      run.detail_url = "/executions";

      await app.goto("/executions");
      secondTab = await app.context.newPage();
      await secondTab.goto("/executions", { waitUntil: "domcontentloaded" });
      await expect(secondTab.getByTestId("notifications-bell")).toBeVisible({
        timeout: shortTimeoutMS,
      });

      await app.page.getByTestId("notifications-bell").click();
      await app.page.getByTestId("notifications-test").click();
      await expect(app.page.getByTestId("notifications-unread-count")).toBeVisible({
        timeout: shortTimeoutMS,
      });
      await expect(secondTab.getByTestId("notifications-unread-count")).toBeVisible({
        timeout: shortTimeoutMS,
      });

      await app.page.getByTestId("notifications-mark-read").click();
      await expect(secondTab.getByTestId("notifications-unread-count")).toHaveCount(0, {
        timeout: shortTimeoutMS,
      });

      run.status = "succeeded";
      run.terminal_observed_at = new Date().toISOString();
    } catch (error) {
      run.status = "failed";
      run.error = error instanceof Error ? error.message : String(error);
      throw error;
    } finally {
      await secondTab?.close();
      await app.persistRun(run);
    }
  });
});
