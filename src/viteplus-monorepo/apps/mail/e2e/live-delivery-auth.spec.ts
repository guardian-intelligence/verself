import { expect, test } from "@playwright/test";
import {
  assertLoggedOut,
  ensureLoggedIn,
  ensureTestUserExists,
  openInbox,
  sendMailToDemoInbox,
  waitForEmailSubject,
} from "./helpers";

test.describe("Webmail Live Delivery Auth", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test("receives mail live, logs out cleanly, and logs back in", async ({ page }) => {
    await ensureLoggedIn(page);
    await openInbox(page);

    const subject = `FM live inbox ${Date.now()}`;
    const body = `body ${subject}`;

    await expect(page.locator("a").filter({ hasText: subject })).toHaveCount(0, {
      timeout: 5_000,
    });

    sendMailToDemoInbox(subject, body);

    const emailRow = await waitForEmailSubject(page, subject);
    await expect(emailRow).toBeVisible({ timeout: 5_000 });

    await emailRow.click();
    await expect(page.locator("main h2")).toHaveText(subject, { timeout: 5_000 });
    await expect(page.locator("pre, .email-body-frame").first()).toContainText(body, {
      timeout: 5_000,
    });

    await assertLoggedOut(page);

    await ensureLoggedIn(page);
    await openInbox(page);
    await expect(page.locator("a").filter({ hasText: subject }).first()).toBeVisible({
      timeout: 5_000,
    });
  });
});
