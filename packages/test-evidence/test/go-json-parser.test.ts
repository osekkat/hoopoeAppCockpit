import { describe, expect, test } from "bun:test";
import { parseGoTestNdjson } from "../src/index.ts";

const SAMPLE = [
  '{"Time":"2026-05-04T00:00:00Z","Action":"run","Package":"hoopoe/internal/x","Test":"TestParent"}',
  '{"Time":"2026-05-04T00:00:00Z","Action":"run","Package":"hoopoe/internal/x","Test":"TestParent/Child1"}',
  '{"Time":"2026-05-04T00:00:00Z","Action":"output","Package":"hoopoe/internal/x","Test":"TestParent/Child1","Output":"running\\n"}',
  '{"Time":"2026-05-04T00:00:01Z","Action":"pass","Package":"hoopoe/internal/x","Test":"TestParent/Child1","Elapsed":0.123}',
  '{"Time":"2026-05-04T00:00:01Z","Action":"run","Package":"hoopoe/internal/x","Test":"TestParent/Child2"}',
  '{"Time":"2026-05-04T00:00:01Z","Action":"output","Package":"hoopoe/internal/x","Test":"TestParent/Child2","Output":"--- FAIL: TestParent/Child2\\n"}',
  '{"Time":"2026-05-04T00:00:01Z","Action":"output","Package":"hoopoe/internal/x","Test":"TestParent/Child2","Output":"    main_test.go:42: expected 1 got 2\\n"}',
  '{"Time":"2026-05-04T00:00:02Z","Action":"fail","Package":"hoopoe/internal/x","Test":"TestParent/Child2","Elapsed":0.5}',
  '{"Time":"2026-05-04T00:00:02Z","Action":"fail","Package":"hoopoe/internal/x","Test":"TestParent","Elapsed":0.6}',
  '{"Time":"2026-05-04T00:00:03Z","Action":"run","Package":"hoopoe/internal/x","Test":"TestSkip"}',
  '{"Time":"2026-05-04T00:00:03Z","Action":"skip","Package":"hoopoe/internal/x","Test":"TestSkip","Elapsed":0}',
  '{"Time":"2026-05-04T00:00:03Z","Action":"output","Package":"hoopoe/internal/y","Output":"FAIL\\thoopoe/internal/y [build failed]\\n"}',
  '{"Time":"2026-05-04T00:00:03Z","Action":"fail","Package":"hoopoe/internal/y"}',
  "",
].join("\n");

describe("hp-6sv :: parseGoTestNdjson", () => {
  test("emits leaf tests only, with correct status / durationMs / errorMessage", () => {
    const parsed = parseGoTestNdjson(SAMPLE);
    const names = parsed.cases.map((c) => c.name);
    expect(names).toEqual(["TestParent/Child1", "TestParent/Child2", "TestSkip"]);

    const child1 = parsed.cases.find((c) => c.name === "TestParent/Child1");
    expect(child1?.status).toBe("passed");
    expect(child1?.durationMs).toBe(123);

    const child2 = parsed.cases.find((c) => c.name === "TestParent/Child2");
    expect(child2?.status).toBe("failed");
    expect(child2?.durationMs).toBe(500);
    expect(child2?.errorMessage).toContain("expected 1 got 2");

    const skip = parsed.cases.find((c) => c.name === "TestSkip");
    expect(skip?.status).toBe("skipped");
  });

  test("captures package-level build failures into buildErrors", () => {
    const parsed = parseGoTestNdjson(SAMPLE);
    expect(parsed.buildErrors.length).toBeGreaterThan(0);
    const joined = parsed.buildErrors.join(" ");
    expect(joined).toContain("hoopoe/internal/y");
    expect(joined).toContain("build failed");
  });

  test("tolerates malformed NDJSON without throwing", () => {
    const parsed = parseGoTestNdjson('not-json\n{"Action":"pass","Package":"p","Test":"T","Elapsed":0.001}');
    expect(parsed.testCount).toBe(1);
    expect(parsed.buildErrors.some((e) => e.includes("malformed JSON"))).toBe(true);
  });
});
