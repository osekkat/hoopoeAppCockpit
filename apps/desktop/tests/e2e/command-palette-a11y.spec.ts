// review/p2-a11y — CommandPalette modal focus containment e2e.
//
// Closes the [MEDIUM] cross-review finding "CommandPalette host lacks
// modal focus containment" by exercising the four invariants that a
// well-behaved modal MUST satisfy:
//
//   1. Focus trap — Tab/Shift+Tab cycle within the dialog; never escape
//      to the underlying page.
//   2. Focus restoration — Escape returns focus to the invoking control.
//   3. aria-activedescendant — search input's attribute updates to the
//      stable id of the currently-highlighted option as the user arrows
//      up/down.
//   4. Selection on click — pointer enter still updates aria-selected
//      and aria-activedescendant.
//
// Uses the shared `chromiumHostStatus()` helper (hp-411d) so this suite
// pass-or-skips together with the rest of the desktop e2e fleet.

import { expect, test, type Page } from "@playwright/test";
import {
  assertNoProductionEndpoints,
  chromiumHostStatus,
  createPhase1TestLogger,
} from "../../src/test-utils/index.ts";

const hostStatus = chromiumHostStatus();

test.describe("command-palette a11y — modal focus containment", () => {
  test.skip(!hostStatus.ready, hostStatus.reason);

  test("traps focus, restores focus on Escape, updates aria-activedescendant on arrow nav", async ({
    page,
  }, testInfo) => {
    const logger = createPhase1TestLogger({
      suite: "command-palette-a11y",
      testName: testInfo.title,
    });
    const runtimeErrors = captureRuntimeErrors(page);
    const baseURL = String(testInfo.project.use.baseURL ?? "http://127.0.0.1:5173");
    logger.start({ baseURL });
    assertNoProductionEndpoints({ urls: [baseURL] });

    logger.phase("act", { route: "/local-demo/plan" });
    await page.goto("/");
    await page.getByRole("link", { name: /local demo/i }).click();
    await expect(page).toHaveURL(/\/local-demo\/plan$/);

    // ── 1. Focus restoration — open via top-bar button so we have a
    //     concrete trigger to assert against. After Escape, focus must
    //     return to that exact button.
    const trigger = page.getByRole("button", { name: "Open command palette" });
    await trigger.focus();
    await trigger.click();
    const palette = page.getByRole("dialog", { name: /command palette/i });
    await expect(palette).toBeVisible();
    const search = palette.getByRole("searchbox", { name: "Search commands" });
    await expect(search).toBeFocused();
    logger.phase("assert", { surface: "palette", check: "open + initial focus" });

    // ── 2. aria-activedescendant connects search input → active option.
    //     Type to surface results, then arrow up/down and confirm the
    //     attribute tracks the active option id.
    await search.fill("go");
    // First arrow-down should land on the first result.
    await page.keyboard.press("ArrowDown");
    const activeId1 = await search.getAttribute("aria-activedescendant");
    expect(activeId1).not.toBeNull();
    expect(activeId1).toMatch(/^hh-command-palette-option-/);
    // Verify the option that owns this id has aria-selected="true".
    const activeOption1 = page.locator(`#${activeId1}`);
    await expect(activeOption1).toHaveAttribute("aria-selected", "true");

    await page.keyboard.press("ArrowDown");
    const activeId2 = await search.getAttribute("aria-activedescendant");
    expect(activeId2).not.toBeNull();
    expect(activeId2).not.toBe(activeId1);
    logger.snapshot("palette.activedescendant", {
      first: activeId1,
      second: activeId2,
    });

    // ── 3. Focus trap — Tab from the search input should land on the
    //     next focusable inside the modal (Close button or an option),
    //     not escape to the underlying page. We verify the focused
    //     element STAYS inside the dialog after Tab and Shift+Tab.
    await search.focus();
    await page.keyboard.press("Tab");
    const afterTab = await page.evaluate(() => {
      const root = document.querySelector('[role="dialog"][aria-modal="true"]');
      return root && root.contains(document.activeElement)
        ? document.activeElement?.tagName ?? null
        : "ESCAPED";
    });
    expect(afterTab).not.toBe("ESCAPED");

    // Shift+Tab from the first focusable should wrap to the last.
    await search.focus();
    await page.keyboard.press("Shift+Tab");
    const afterShiftTab = await page.evaluate(() => {
      const root = document.querySelector('[role="dialog"][aria-modal="true"]');
      return root && root.contains(document.activeElement)
        ? document.activeElement?.tagName ?? null
        : "ESCAPED";
    });
    expect(afterShiftTab).not.toBe("ESCAPED");
    logger.phase("assert", { surface: "palette", check: "focus trap" });

    // ── 4. Focus restoration on Escape — return to trigger.
    await page.keyboard.press("Escape");
    await expect(palette).toHaveCount(0);
    await expect(trigger).toBeFocused();
    logger.phase("assert", { surface: "palette", check: "escape restores trigger focus" });

    // ── 5. Re-open + selection-by-click does not break focus contract.
    await trigger.click();
    await expect(palette).toBeVisible();
    const firstOption = palette.getByRole("option").first();
    await firstOption.hover();
    const optionId = await firstOption.getAttribute("id");
    expect(optionId).toMatch(/^hh-command-palette-option-/);
    const activeIdAfterHover = await search.getAttribute("aria-activedescendant");
    expect(activeIdAfterHover).toBe(optionId);

    await page.keyboard.press("Escape");
    await expect(palette).toHaveCount(0);
    await expect(trigger).toBeFocused();

    expect(runtimeErrors).toEqual([]);
    logger.end("passed", { runtimeErrors });
    await testInfo.attach("command-palette-a11y.ndjson", {
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
      if (!isExpectedBrowserWarning(text)) runtimeErrors.push(text);
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
