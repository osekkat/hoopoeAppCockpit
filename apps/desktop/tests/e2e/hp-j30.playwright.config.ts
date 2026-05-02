import { defineConfig } from "@playwright/test";
import * as Path from "node:path";
import { fileURLToPath } from "node:url";
import baseConfig from "../../../../playwright.config.ts";

const e2eDir = Path.dirname(fileURLToPath(import.meta.url));
const desktopRoot = Path.resolve(e2eDir, "../..");

export default defineConfig({
  ...baseConfig,
  testDir: ".",
  outputDir: "/tmp/hoopoe-playwright-hp-j30/results",
  reporter: [
    ["list"],
    ["html", { open: "never", outputFolder: "/tmp/hoopoe-playwright-hp-j30/html" }],
  ],
  webServer: {
    ...baseConfig.webServer,
    command: `bun run --cwd ${desktopRoot} dev`,
  },
});
