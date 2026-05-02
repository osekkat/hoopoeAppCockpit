import { describe, expect, test } from "bun:test";
import {
  collectGoModViolations,
  collectGoViolations,
  collectPackageManifestViolations,
  collectTypeScriptViolations,
} from "./check-provider-sdks.ts";

describe("provider SDK lint", () => {
  test("detects forbidden TypeScript import surfaces", () => {
    const violations = collectTypeScriptViolations(
      "apps/desktop/src/provider-surfaces.ts",
      [
        'import OpenAI from "openai";',
        'export { Anthropic } from "@anthropic-ai/sdk";',
        'const google = await import("@google/generative-ai");',
        'const anthropic = require("@anthropic-ai/anthropic");',
        'type Client = import("openai/resources").Client;',
      ].join("\n"),
    );

    expect(violations.map((violation) => violation.importPath)).toEqual([
      "openai",
      "@anthropic-ai/sdk",
      "@google/generative-ai",
      "@anthropic-ai/anthropic",
      "openai/resources",
    ]);
  });

  test("detects forbidden Go import surfaces", () => {
    const violations = collectGoViolations(
      "apps/daemon/internal/modelreach/provider.go",
      [
        "package modelreach",
        "",
        "import (",
        '  openai "github.com/openai/openai-go"',
        '  _ "github.com/google/generative-ai-go/genai"',
        ")",
      ].join("\n"),
    );

    expect(violations.map((violation) => violation.importPath)).toEqual([
      "github.com/openai/openai-go",
      "github.com/google/generative-ai-go/genai",
    ]);
  });

  test("detects provider key configuration fields", () => {
    const violations = collectTypeScriptViolations(
      "apps/desktop/src/settings.ts",
      [
        "export const settingKeys = [",
        '  "OPENAI_API_KEY",',
        '  "ANTHROPIC_API_KEY",',
        '  "GEMINI_API_KEY",',
        "];",
      ].join("\n"),
    );

    expect(violations.map((violation) => violation.importPath)).toEqual([
      "OPENAI_API_KEY",
      "ANTHROPIC_API_KEY",
      "GEMINI_API_KEY",
    ]);
  });

  test("detects forbidden package dependencies", () => {
    const violations = collectPackageManifestViolations(
      "apps/desktop/package.json",
      JSON.stringify(
        {
          dependencies: {
            "@anthropic-ai/sdk": "latest",
            "lucide-react": "latest",
          },
          devDependencies: {
            "openai": "latest",
          },
        },
        null,
        2,
      ),
    );

    expect(violations.map((violation) => violation.importPath)).toEqual([
      "@anthropic-ai/sdk",
      "openai",
    ]);
  });

  test("detects forbidden Go module dependencies", () => {
    const violations = collectGoModViolations(
      "apps/daemon/go.mod",
      [
        "module github.com/hoopoe/daemon",
        "",
        "require github.com/anthropics/anthropic-sdk-go v0.1.0",
      ].join("\n"),
    );

    expect(violations.map((violation) => violation.importPath)).toEqual([
      "github.com/anthropics/anthropic-sdk-go",
    ]);
  });

  test("allows subscription-backed CLI and browser-engine names", () => {
    const violations = collectTypeScriptViolations(
      "apps/desktop/src/model-reach.ts",
      [
        'const allowed = ["claude-code", "codex-cli", "gemini-cli"];',
        'const oracle = "oracle --engine browser";',
        'const accountPath = "CAAM";',
      ].join("\n"),
    );

    expect(violations).toHaveLength(0);
  });
});
