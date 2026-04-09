import { test, expect } from "@playwright/test";
import { ensureLoggedIn, ensureTestUserExists } from "./helpers";

test.describe("Webmail", () => {
  test.beforeAll(async () => {
    await ensureTestUserExists();
  });

  test.beforeEach(async ({ page }) => {
    await ensureLoggedIn(page);
  });

  test("displays mailbox list with Inbox after sign-in", async ({ page }) => {
    // Should be on /mail after login
    await expect(page).toHaveURL(/\/mail/);

    // Sidebar should contain at least an Inbox link
    const sidebar = page.locator("aside nav");
    await expect(sidebar).toBeVisible();
    await expect(sidebar.getByText("Inbox")).toBeVisible({ timeout: 30_000 });
  });

  test("navigates to Inbox and shows email list", async ({ page }) => {
    const sidebar = page.locator("aside nav");
    await expect(sidebar.getByText("Inbox")).toBeVisible({ timeout: 30_000 });

    // Click Inbox
    await sidebar.getByText("Inbox").click();
    await expect(page).toHaveURL(/\/mail\//);

    // Email list pane should appear. It may be empty or have emails.
    // If there are emails, each row should have a sender and subject.
    const emailList = page.locator('[class*="divide-y"]');
    await emailList.waitFor({ state: "visible", timeout: 15_000 });
  });

  test("opens an email and displays body", async ({ page }) => {
    const sidebar = page.locator("aside nav");
    await expect(sidebar.getByText("Inbox")).toBeVisible({ timeout: 30_000 });
    await sidebar.getByText("Inbox").click();

    // Wait for at least one email to appear
    const firstEmail = page.locator('[class*="divide-y"] > a').first();
    await firstEmail.waitFor({ state: "visible", timeout: 30_000 });

    // Click the first email
    await firstEmail.click();

    // Email viewer should display subject and body
    const viewer = page.locator("main").locator("h2");
    await expect(viewer).toBeVisible({ timeout: 15_000 });

    // The email body area should render (either HTML or text content)
    const bodyArea = page.locator(".email-body-frame, pre");
    await expect(bodyArea.first()).toBeVisible({ timeout: 15_000 });
  });

  test("flags and unflags an email", async ({ page }) => {
    const sidebar = page.locator("aside nav");
    await expect(sidebar.getByText("Inbox")).toBeVisible({ timeout: 30_000 });
    await sidebar.getByText("Inbox").click();

    // Open first email
    const firstEmail = page.locator('[class*="divide-y"] > a').first();
    await firstEmail.waitFor({ state: "visible", timeout: 30_000 });
    await firstEmail.click();

    // Wait for the email viewer toolbar to appear
    const starButton = page.getByTitle("Star").or(page.getByTitle("Remove star"));
    await starButton.waitFor({ state: "visible", timeout: 15_000 });

    // Toggle flag on
    await starButton.click();
    // The button title should change to "Remove star" after flagging
    await expect(page.getByTitle("Remove star")).toBeVisible({ timeout: 10_000 });

    // Toggle flag off
    await page.getByTitle("Remove star").click();
    await expect(page.getByTitle("Star")).toBeVisible({ timeout: 10_000 });
  });

  test("trashes an email and navigates back to list", async ({ page }) => {
    const sidebar = page.locator("aside nav");
    await expect(sidebar.getByText("Inbox")).toBeVisible({ timeout: 30_000 });
    await sidebar.getByText("Inbox").click();

    // Open first email
    const firstEmail = page.locator('[class*="divide-y"] > a').first();
    await firstEmail.waitFor({ state: "visible", timeout: 30_000 });
    const emailText = await firstEmail.textContent();
    await firstEmail.click();

    // Click trash button
    const trashButton = page.getByTitle("Move to Trash");
    await trashButton.waitFor({ state: "visible", timeout: 15_000 });
    await trashButton.click();

    // Should navigate back to the mailbox list (no email selected)
    await expect(page).toHaveURL(/\/mail\/[^/]+$/);

    // The trashed email should eventually disappear from Inbox
    // (via Electric sync after the JMAP backend processes it)
    if (emailText) {
      // Wait a moment for Electric to sync the move
      await page.waitForTimeout(3_000);
    }
  });
});
