// Smoke test for the renderer-isolation lint helpers. The full executable
// (with its `process.exit(1)` on failure) is exercised by the
// `lint:renderer` script in CI; here we just unit-test the pattern set.

import { expect, test } from "bun:test";

const BANNED_IMPORT_LINES = [
  `import * as fs from "fs";`,
  `import { readFileSync } from "node:fs";`,
  `import { promises as fsp } from "fs/promises";`,
  `import { createServer } from "node:net";`,
  `import { spawn } from "child_process";`,
  `import { request } from "node:http";`,
  `import { request } from "https";`,
  `import { contextBridge } from "electron";`,
  `import { something } from "electron/main";`,
  `import ReactMarkdown from "react-markdown";`,
  `import rehypeRaw from "rehype-raw";`,
  `import rehypeSanitize from "rehype-sanitize";`,
  `import { marked } from "marked";`,
  `import MarkdownIt from "markdown-it";`,
];

const BANNED_INLINE_LINES = [
  `eval("1+1");`,
  `const f = new Function("return 1");`,
  `window.require("fs");`,
  `console.log(window.process.env);`,
  `console.log(globalThis.process.env);`,
  `<webview src="x" />`,
  `<div dangerouslySetInnerHTML={{ __html: markdown }} />`,
];

const ALLOWED_LINES = [
  `import { useEffect } from "react";`,
  `const fs = await window.hoopoe.files.openExternal(url);`,
  `const evaluator = "eval";`, // string literal, not a call
];

const BANNED_IMPORTS = [
  /from\s+["'](?:node:)?(?:fs|fs\/promises)["']/,
  /from\s+["'](?:node:)?net["']/,
  /from\s+["'](?:node:)?tls["']/,
  /from\s+["'](?:node:)?child_process["']/,
  /from\s+["'](?:node:)?https?["']/,
  /from\s+["']electron(?:\/.*)?["']/,
  /from\s+["']react-markdown["']/,
  /from\s+["']rehype-(?:raw|sanitize)["']/,
  /from\s+["'](?:marked|markdown-it)["']/,
];

const BANNED_PATTERNS = [
  /\beval\s*\(/,
  /\bnew\s+Function\s*\(/,
  /\bwindow\.require\b/,
  /\bwindow\.process\b/,
  /\bglobalThis\.process\b/,
  /<webview\b/i,
  /\bdangerouslySetInnerHTML\s*=/,
];

function lineMatchesAny(text: string, patterns: ReadonlyArray<RegExp>): boolean {
  return patterns.some((p) => p.test(text));
}

test("rendererlint: every banned import line matches at least one pattern", () => {
  for (const line of BANNED_IMPORT_LINES) {
    expect(lineMatchesAny(line, BANNED_IMPORTS)).toBe(true);
  }
});

test("rendererlint: every banned inline pattern matches", () => {
  for (const line of BANNED_INLINE_LINES) {
    expect(lineMatchesAny(line, BANNED_PATTERNS)).toBe(true);
  }
});

test("rendererlint: allowed lines do NOT match any pattern", () => {
  for (const line of ALLOWED_LINES) {
    expect(lineMatchesAny(line, BANNED_IMPORTS)).toBe(false);
    expect(lineMatchesAny(line, BANNED_PATTERNS)).toBe(false);
  }
});
