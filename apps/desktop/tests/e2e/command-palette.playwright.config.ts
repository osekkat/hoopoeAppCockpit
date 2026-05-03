import { defineConfig } from "@playwright/test";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import baseConfig from "../../../../playwright.config.ts";

const e2eDir = Path.dirname(fileURLToPath(import.meta.url));
const desktopRoot = Path.resolve(e2eDir, "../..");

export default defineConfig({
  ...baseConfig,
  testDir: ".",
  testMatch: ["command-palette.spec.ts"],
  outputDir: "/tmp/hoopoe-playwright-hp-2qgx/results",
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: "/tmp/hoopoe-playwright-hp-2qgx/html" }],
  ],
  webServer: {
    ...baseConfig.webServer,
    command: `bun run --cwd ${desktopRoot} dev`,
  },
});
