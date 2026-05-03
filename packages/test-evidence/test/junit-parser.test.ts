import { describe, expect, test } from "bun:test";
import { parseJunitXml } from "../src/index.ts";

const SAMPLE = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites name="bun test" tests="3" failures="1" skipped="1" time="0.045">
  <testsuite name="src/foo.test.ts" tests="3" failures="1" skipped="1" time="0.045">
    <testcase classname="src/foo.test.ts" name="boots cleanly @unit" time="0.012" />
    <testcase classname="src/foo.test.ts" name="reconnects within budget @slo:desktop.reconnect.p95" time="0.020">
      <failure type="AssertionError" message="expected 9999 to be &lt;= 8000">stack trace
multi line</failure>
    </testcase>
    <testcase classname="src/foo.test.ts" name="todo @smoke" time="0.000">
      <skipped />
    </testcase>
  </testsuite>
</testsuites>`;

describe("hp-6sv :: parseJunitXml", () => {
  test("captures count, names, statuses, durations, errors, classnames", () => {
    const parsed = parseJunitXml(SAMPLE);
    expect(parsed.testCount).toBe(3);
    expect(parsed.cases.map((c) => c.status)).toEqual(["passed", "failed", "skipped"]);
    expect(parsed.cases[0]?.name).toBe("boots cleanly @unit");
    expect(parsed.cases[0]?.durationMs).toBe(12);
    expect(parsed.cases[1]?.errorMessage).toBe("AssertionError");
    expect(parsed.cases[1]?.classname).toBe("src/foo.test.ts");
    expect(parsed.cases[2]?.durationMs).toBe(0);
  });

  test("decodes XML entities in attributes (failure type) and self-closing tags", () => {
    const xml = `<testsuite>
  <testcase classname="x" name="quotes &amp; angles &lt;&gt;" time="0.001" />
</testsuite>`;
    const parsed = parseJunitXml(xml);
    expect(parsed.testCount).toBe(1);
    expect(parsed.cases[0]?.name).toBe("quotes & angles <>");
    expect(parsed.cases[0]?.status).toBe("passed");
  });
});
