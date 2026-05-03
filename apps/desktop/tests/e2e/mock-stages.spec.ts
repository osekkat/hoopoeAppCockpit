import { existsSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import {
  assertNoProductionEndpoints,
  createPhase1TestLogger,
} from "../../src/test-utils/structured-test-logger.ts";

const hostStatus = chromiumHostStatus();

test.describe("hp-0non Mock Flywheel stage data", () => {
  test.skip(!hostStatus.ready, hostStatus.reason);

  test("renders Beads and Swarm from the scenario corpus", async ({ page }, testInfo) => {
    const logger = createPhase1TestLogger({
      suite: "hp-0non.mock-stages",
      testName: testInfo.title,
    });
    const runtimeErrors = captureRuntimeErrors(page);
    const baseURL = String(testInfo.project.use.baseURL ?? "http://127.0.0.1:5173");

    logger.start({ baseURL, scenario: "healthy-hour" });
    assertNoProductionEndpoints({ urls: [baseURL, "fixture://mock-flywheel/healthy-hour"] });

    logger.phase("act", { route: "/mock-flywheel-project/bead" });
    await page.goto("/mock-flywheel-project/bead");
    await expect(page.getByTestId("mock-beads-stage")).toBeVisible();
    await expect(page.getByText("STAGE 02")).toBeVisible();
    await expect(page.getByText("healthy-hour").first()).toBeVisible();
    await expect(page.getByText("phase0-2026-05-02").first()).toBeVisible();
    await expect(page.getByText("hp-tu1l").first()).toBeVisible();
    await expect(page.getByRole("heading", { name: "Bead board" })).toBeVisible();
    logger.assertion("beads-stage.fixture-visible", {
      scenario: "healthy-hour",
      knownBead: "hp-tu1l",
    });

    logger.phase("act", { route: "/mock-flywheel-project/swarm" });
    await page.getByRole("link", { name: "Swarm" }).click();
    await expect(page).toHaveURL(/\/mock-flywheel-project\/swarm$/);
    await expect(page.getByTestId("mock-swarm-stage")).toBeVisible();
    await expect(page.getByText("STAGE 03")).toBeVisible();
    await expect(page.getByText("healthy-hour").first()).toBeVisible();
    await expect(page.getByText("hoopoe-implementation")).toBeVisible();
    await expect(page.getByText("GreenBear")).toBeVisible();
    await expect(page.getByText("hoopoe-intro").first()).toBeVisible();
    await expect(page.getByText(/raw terminal|terminal scrollback/i)).toHaveCount(0);
    logger.assertion("swarm-stage.fixture-visible", {
      scenario: "healthy-hour",
      knownSession: "hoopoe-implementation",
      knownAgent: "GreenBear",
      knownThread: "hoopoe-intro",
    });

    expect(runtimeErrors).toEqual([]);
    logger.end("passed", { runtimeErrors });
    await testInfo.attach("hp-0non-mock-stages.ndjson", {
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

function chromiumHostStatus(): { readonly ready: boolean; readonly reason: string } {
  if (process.platform !== "linux") {
    return {
      ready: true,
      reason: "non-linux platform; Playwright bundles browser dependencies",
    };
  }

  const hasLibGbm = [
    "/usr/lib/x86_64-linux-gnu/libgbm.so.1",
    "/usr/lib/aarch64-linux-gnu/libgbm.so.1",
    "/usr/lib64/libgbm.so.1",
  ].some((path) => existsSync(path));

  return hasLibGbm
    ? { ready: true, reason: "libgbm.so.1 present" }
    : {
        ready: false,
        reason:
          "libgbm.so.1 not found; skipping Mock Flywheel e2e on this host instead of failing before app launch.",
      };
}
