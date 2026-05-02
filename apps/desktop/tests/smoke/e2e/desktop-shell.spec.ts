import { expect, test, type Page } from "@playwright/test";

test.describe("Hoopoe desktop shell smoke", () => {
  test("boots without console errors and navigates every stage by sidebar", async ({ page }) => {
    const runtimeErrors = captureRuntimeErrors(page);

    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Local demo" })).toBeVisible();

    await page.getByRole("link", { name: /local demo/i }).click();
    await expect(page).toHaveURL(/\/local-demo\/plan$/);
    await expect(page.getByText("STAGE 01")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Planning" })).toBeVisible();

    await page.getByRole("link", { name: "Beads" }).click();
    await expect(page).toHaveURL(/\/local-demo\/bead$/);
    await expect(page.getByText("STAGE 02")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Beads" })).toBeVisible();

    await page.getByRole("link", { name: "Swarm" }).click();
    await expect(page).toHaveURL(/\/local-demo\/swarm$/);
    await expect(page.getByText("STAGE 03")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Swarm" })).toBeVisible();
    await expect(page.getByText("Bead board")).toBeVisible();
    await expect(page.getByText("Agent grid")).toBeVisible();

    await page.getByRole("link", { name: "Hardening" }).click();
    await expect(page).toHaveURL(/\/local-demo\/harden$/);
    await expect(page.getByText("STAGE 04")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Hardening" })).toBeVisible();

    await page.getByRole("link", { name: "Diagnostics" }).click();
    await expect(page).toHaveURL(/\/local-demo\/diag$/);
    await expect(page.getByText("STAGE DX")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Diagnostics" })).toBeVisible();

    expect(runtimeErrors).toEqual([]);
  });

  test("opens and closes the Activity panel drawer", async ({ page }) => {
    const runtimeErrors = captureRuntimeErrors(page);
    await page.goto("/local-demo/plan");

    const panel = page.getByRole("dialog", { name: "Activity panel" });
    await expect(panel).toHaveAttribute("data-open", "false");

    await page.getByRole("button", { name: "Open Activity panel" }).click();
    await expect(panel).toHaveAttribute("data-open", "true");
    await expect(panel.getByText("orchestrator-chat")).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(panel).toHaveAttribute("data-open", "false");
    expect(runtimeErrors).toEqual([]);
  });

  test("command palette smoke activates when the palette implementation lands", async ({ page }) => {
    await page.goto("/local-demo/plan");

    await page.getByRole("button", { name: "Open command palette" }).click();
    const palette = page.getByRole("dialog", { name: /command palette/i });
    test.skip(
      (await palette.count()) === 0,
      "CommandPalette UI is owned by hp-6f4; this smoke turns active when the dialog exists.",
    );

    await expect(palette).toBeVisible();
    await page.keyboard.type("planning");
    await expect(palette.getByText(/planning/i).first()).toBeVisible();
  });
});

function captureRuntimeErrors(page: Page): string[] {
  const runtimeErrors: string[] = [];

  page.on("console", (message) => {
    if (message.type() === "error") {
      const text = message.text();
      if (!isExpectedBrowserWarning(text)) {
        runtimeErrors.push(text);
      }
    }
  });
  page.on("pageerror", (error) => {
    runtimeErrors.push(error.message);
  });

  return runtimeErrors;
}

function isExpectedBrowserWarning(message: string): boolean {
  return message.includes(
    "The Content Security Policy directive 'frame-ancestors' is ignored when delivered via a <meta> element.",
  );
}
