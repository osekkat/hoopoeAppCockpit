// `@hoopoe/test-evidence` — JUnit XML parser for Bun-test (and others) (hp-6sv).
//
// Bun test (1.2+) emits JUnit XML via `--reporter=junit --reporter-outfile=<path>`.
// We parse the subset of the format we need: testsuites > testsuite >
// testcase[name, classname, time] with optional <failure> / <skipped>
// children. No namespaces, no CDATA quirks beyond Bun's actual output.
//
// We hand-roll because the package has zero npm deps; if the format
// drifts, swap in `fast-xml-parser`.

import type { TestResult, TestStatus } from "./envelope.ts";

const TESTCASE_OPEN_RE =
  /<testcase\b([^>]*?)(?:\/>|>([\s\S]*?)<\/testcase>)/g;
const ATTR_RE = /([A-Za-z_][A-Za-z0-9_:.-]*)\s*=\s*"([^"]*)"/g;

interface ParsedAttrs {
  name?: string;
  classname?: string;
  file?: string;
  time?: string;
  type?: string;
  message?: string;
}

function parseAttrs(raw: string): ParsedAttrs {
  const out: ParsedAttrs = {};
  ATTR_RE.lastIndex = 0;
  let m: RegExpExecArray | null = ATTR_RE.exec(raw);
  while (m !== null) {
    const key = m[1] as string;
    const value = decodeXmlEntities(m[2] as string);
    if (key === "name") out.name = value;
    else if (key === "classname") out.classname = value;
    else if (key === "file") out.file = value;
    else if (key === "time") out.time = value;
    else if (key === "type") out.type = value;
    else if (key === "message") out.message = value;
    m = ATTR_RE.exec(raw);
  }
  return out;
}

function decodeXmlEntities(text: string): string {
  return text
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&apos;/g, "'")
    .replace(/&amp;/g, "&");
}

function statusFromBody(body: string): { status: TestStatus; errorMessage?: string } {
  const failureMatch = /<failure\b([^>]*)(?:\/>|>([\s\S]*?)<\/failure>)/i.exec(body);
  if (failureMatch) {
    const attrs = parseAttrs(failureMatch[1] ?? "");
    const inner = decodeXmlEntities((failureMatch[2] ?? "").trim());
    const message = attrs.type ?? attrs.message ?? attrs.name ?? inner.split("\n")[0] ?? "failure";
    return { status: "failed", errorMessage: message };
  }
  const errorMatch = /<error\b([^>]*)(?:\/>|>([\s\S]*?)<\/error>)/i.exec(body);
  if (errorMatch) {
    const attrs = parseAttrs(errorMatch[1] ?? "");
    const inner = decodeXmlEntities((errorMatch[2] ?? "").trim());
    const message = attrs.type ?? attrs.message ?? attrs.name ?? inner.split("\n")[0] ?? "error";
    return { status: "failed", errorMessage: message };
  }
  if (/<skipped\b/i.test(body)) {
    return { status: "skipped" };
  }
  return { status: "passed" };
}

export interface ParsedJunit {
  testCount: number;
  cases: TestResult[];
}

/** Parse Bun-test JUnit XML. Each `<testcase>` becomes one TestResult.
 *  Tags (`@slo:foo`, `@e2e`) are NOT stripped here — see `slo-tags.ts`. */
export function parseJunitXml(xml: string): ParsedJunit {
  const cases: TestResult[] = [];
  TESTCASE_OPEN_RE.lastIndex = 0;
  let match: RegExpExecArray | null = TESTCASE_OPEN_RE.exec(xml);
  while (match !== null) {
    const attrs = parseAttrs(match[1] ?? "");
    const body = match[2] ?? "";
    const { status, errorMessage } = statusFromBody(body);
    const timeSec = attrs.time !== undefined ? Number(attrs.time) : 0;
    const result: TestResult = {
      name: attrs.name ?? "(unnamed)",
      file: attrs.file ?? attrs.classname ?? "(unknown)",
      status,
      durationMs: Number.isFinite(timeSec) ? Math.max(0, Math.round(timeSec * 1000)) : 0,
    };
    if (attrs.classname !== undefined) {
      result.classname = attrs.classname;
    }
    if (errorMessage !== undefined) {
      result.errorMessage = errorMessage;
    }
    cases.push(result);
    match = TESTCASE_OPEN_RE.exec(xml);
  }
  return { testCount: cases.length, cases };
}
