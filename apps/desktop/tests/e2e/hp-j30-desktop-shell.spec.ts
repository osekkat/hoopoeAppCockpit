import { expect, test, type Page } from "@playwright/test";
import {
  assertNoProductionEndpoints,
  chromiumHostStatus,
  createPhase1TestLogger,
} from "../../src/test-utils/index.ts";

// hp-411d: shared helper so the smoke + hp-j30 suites skip-or-run together
// instead of one failing 3/3 while the other skips with a different message.
const hostStatus = chromiumHostStatus();

test.describe("hp-j30 desktop shell e2e", () => {
  test.skip(!hostStatus.ready, hostStatus.reason);

  test("navigates stages, exercises Activity, and records command-palette wiring", async ({
    page,
  }, testInfo) => {
    const logger = createPhase1TestLogger({
      suite: "hp-j30.e2e",
      testName: testInfo.title,
    });
    const runtimeErrors = captureRuntimeErrors(page);
    const baseURL = String(testInfo.project.use.baseURL ?? "http://127.0.0.1:5173");

    logger.start({ baseURL });
    assertNoProductionEndpoints({ urls: [baseURL] });

    logger.phase("act", { route: "/" });
    await page.goto("/");
    await expect(page.getByRole("heading", { name: "Local demo" })).toBeVisible();

    await page.getByRole("link", { name: /local demo/i }).click();
    await expect(page).toHaveURL(/\/local-demo\/plan$/);
    await expect(page.getByText("STAGE 01")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Planning" })).toBeVisible();

    await page.getByRole("link", { name: "Beads" }).click();
    await expect(page).toHaveURL(/\/local-demo\/bead$/);
    await expect(page.getByText("STAGE 02")).toBeVisible();

    await page.getByRole("link", { name: "Swarm" }).click();
    await expect(page).toHaveURL(/\/local-demo\/swarm$/);
    await expect(page.getByText("STAGE 03")).toBeVisible();
    await expect(page.getByText("Agent grid")).toBeVisible();
    await expect(page.getByText(/raw terminal|terminal scrollback/i)).toHaveCount(0);

    await page.getByRole("link", { name: "Hardening" }).click();
    await expect(page).toHaveURL(/\/local-demo\/harden$/);
    await expect(page.getByText("STAGE 04")).toBeVisible();

    await page.getByRole("link", { name: "Diagnostics" }).click();
    await expect(page).toHaveURL(/\/local-demo\/diag$/);
    await expect(page.getByText("STAGE DX")).toBeVisible();

    logger.phase("assert", { surface: "activity" });
    const panel = page.getByRole("dialog", { name: "Activity panel" });
    await expect(panel).toHaveAttribute("data-open", "false");
    await page.getByRole("button", { name: "Open Activity panel" }).click();
    await expect(panel).toHaveAttribute("data-open", "true");
    await expect(panel.getByText("orchestrator-chat")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(panel).toHaveAttribute("data-open", "false");

    logger.phase("assert", { surface: "command-palette" });
    const commandButton = page.getByRole("button", { name: "Open command palette" });
    await expect(commandButton).toBeVisible();
    await commandButton.click();
    await page.keyboard.press("ControlOrMeta+K");
    const palette = page.getByRole("dialog", { name: /command palette/i });
    const paletteCount = await palette.count();
    logger.snapshot("command-palette.wiring", {
      buttonVisible: true,
      dialogCount: paletteCount,
      note:
        paletteCount === 0
          ? "Renderer topbar exposes the command control; hp-6f4 component wiring is not mounted here yet."
          : "CommandPalette dialog mounted.",
    });
    if (paletteCount > 0) {
      await expect(palette).toBeVisible();
      await page.keyboard.type("planning");
      await expect(palette.getByText(/planning/i).first()).toBeVisible();
    }

    expect(runtimeErrors).toEqual([]);
    logger.end("passed", { runtimeErrors });
    await testInfo.attach("hp-j30-structured-log.ndjson", {
      body: logger.jsonLines(),
      contentType: "application/x-ndjson",
    });
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
