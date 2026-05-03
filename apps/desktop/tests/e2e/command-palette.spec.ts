import { expect, test, type Page } from "@playwright/test";
import { existsSync } from "node:fs";
import {
  assertNoProductionEndpoints,
  createPhase1TestLogger,
} from "../../src/test-utils/index.ts";

const chromiumHostReady =
  process.platform !== "linux" ||
  [
    "/usr/lib/x86_64-linux-gnu/libgbm.so.1",
    "/usr/lib/aarch64-linux-gnu/libgbm.so.1",
    "/usr/lib64/libgbm.so.1",
  ].some((path) => existsSync(path));

test.describe("hp-2qgx command palette e2e", () => {
  test.skip(
    !chromiumHostReady,
    "Chromium host dependency libgbm.so.1 is missing; install Playwright deps to run hp-2qgx e2e.",
  );

  test("opens via ⌘K, fuzzy-searches commands, and executes a navigation", async ({
    page,
  }, testInfo) => {
    const logger = createPhase1TestLogger({
      suite: "hp-2qgx.e2e",
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

    logger.phase("act", { surface: "command-palette", trigger: "shortcut" });
    await page.keyboard.press("ControlOrMeta+KeyK");
    const palette = page.getByRole("dialog", { name: /command palette/i });
    await expect(palette).toBeVisible();
    await expect(palette.getByRole("listbox", { name: "Matched commands" })).toBeVisible();

    logger.phase("assert", { surface: "command-palette", check: "fuzzy-search" });
    // The input is `<input role="combobox">` per the WAI-ARIA combobox
    // pattern (combobox + aria-controls + aria-activedescendant + aria-
    // expanded), set in commit 7fd6fe3. The native `searchbox` role is
    // overridden by the explicit `combobox` role.
    const search = palette.getByRole("combobox", { name: "Search commands" });
    await expect(search).toBeFocused();
    await search.fill("swarm");
    const swarmOption = palette.getByRole("option", { name: /Go to Swarm/i });
    await expect(swarmOption).toBeVisible();
    await expect(swarmOption).toHaveAttribute("aria-selected", "true");

    logger.snapshot("command-palette.match", {
      query: "swarm",
      activeOptionVisible: true,
    });

    logger.phase("act", { surface: "command-palette", trigger: "enter" });
    await page.keyboard.press("Enter");
    await expect(palette).toHaveCount(0);
    await expect(page).toHaveURL(/\/local-demo\/swarm$/);
    await expect(page.getByText("STAGE 03")).toBeVisible();

    logger.phase("act", { surface: "command-palette", trigger: "topbar-button" });
    const commandButton = page.getByRole("button", { name: "Open command palette" });
    await commandButton.click();
    await expect(palette).toBeVisible();
    await expect(commandButton).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");
    await expect(palette).toHaveCount(0);
    await expect(commandButton).toHaveAttribute("aria-expanded", "false");

    logger.snapshot("command-palette.toggle", {
      buttonRoundtrip: "click → escape",
      finalDialogCount: 0,
    });

    expect(runtimeErrors).toEqual([]);
    logger.end("passed", { runtimeErrors });
    await testInfo.attach("hp-2qgx-structured-log.ndjson", {
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
