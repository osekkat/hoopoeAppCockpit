import { expect, test } from "bun:test";
import {
  escapeHtml,
  previewPolicy,
  sanitizeMarkdownPreview,
  sanitizeUrl,
} from "./index.ts";

test("sanitizeMarkdownPreview: escapes raw scripts and event attributes by default", () => {
  const { html, policy } = sanitizeMarkdownPreview([
    "# Title",
    "<script>alert(1)</script>",
    "<img src=x onerror=alert(1)>",
  ].join("\n"));

  expect(policy.rawHtml).toBe("escaped");
  expect(html).not.toContain("<script");
  expect(html).not.toContain("<img");
  expect(html).not.toContain("onerror=\"");
  expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
});

test("sanitizeMarkdownPreview: rejects javascript and data URI links", () => {
  const { html } = sanitizeMarkdownPreview([
    "[xss](javascript:alert(1))",
    "[encoded](java&#x73;cript:alert(1))",
    "[data](data:text/html,<script>alert(1)</script>)",
    "[safe](https://example.com/docs?a=1&b=2)",
  ].join("\n"));

  expect(html).not.toContain("javascript:");
  expect(html).not.toContain("data:text/html");
  expect(html).toContain('<a href="https://example.com/docs?a=1&amp;b=2" rel="noreferrer">safe</a>');
});

test("sanitizeMarkdownPreview: escapes code payloads and tolerates long lines", () => {
  const longLine = `const value = "${"<".repeat(5000)}";`;
  const unicodeConfusable = "const раураl = `<script>alert(1)</script>`;";
  const { html } = sanitizeMarkdownPreview([
    "```ts",
    longLine,
    unicodeConfusable,
    "```",
  ].join("\n"));

  expect(html).toContain("<pre><code>");
  expect(html).not.toContain("<script>");
  expect(html).toContain("const раураl");
  expect(html.length).toBeGreaterThan(5000);
});

test("sanitizeMarkdownPreview: nested bracket bombs stay bounded and inert", () => {
  const bomb = `${"[".repeat(20_000)}javascript:alert(1)`;
  const started = performance.now();
  const { html } = sanitizeMarkdownPreview(bomb);
  const elapsedMs = performance.now() - started;

  expect(html).not.toContain("<a ");
  expect(html).toContain("[[[");
  expect(elapsedMs).toBeLessThan(500);
});

test("sanitizeUrl: filters dangerous schemes after entity and whitespace normalization", () => {
  expect(sanitizeUrl("java\nscript:alert(1)")).toBeNull();
  expect(sanitizeUrl("java&#x73;cript:alert(1)")).toBeNull();
  expect(sanitizeUrl("vbscript:msgbox(1)")).toBeNull();
  expect(sanitizeUrl("data:text/html,<h1>x</h1>")).toBeNull();
  expect(sanitizeUrl("//example.com/path")).toBeNull();
  expect(sanitizeUrl("./docs/readme.md#intro")).toBe("./docs/readme.md#intro");
  expect(sanitizeUrl("mailto:user@example.com")).toBe("mailto:user@example.com");
});

test("previewPolicy: unsafe raw HTML requires an explicit per-project warning acknowledgement", () => {
  expect(previewPolicy(null)).toEqual({
    rawHtml: "escaped",
    warningRequired: false,
    allowedProtocols: ["http:", "https:", "mailto:"],
  });
  expect(previewPolicy({
    projectId: "proj_01",
    allowUnsafeHtml: true,
    warningAcceptedAt: null,
  }).warningRequired).toBe(true);
});

test("escapeHtml: escapes attribute and tag delimiters", () => {
  expect(escapeHtml(`<img alt="'x'" src=x>`)).toBe("&lt;img alt=&quot;&#39;x&#39;&quot; src=x&gt;");
});
