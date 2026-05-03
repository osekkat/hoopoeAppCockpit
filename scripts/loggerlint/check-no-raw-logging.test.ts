import { describe, expect, test } from "bun:test";
import { collectViolations, scan } from "./check-no-raw-logging.ts";

describe("collectViolations (TS)", () => {
  test("flags bare console.log in production code", () => {
    const text = `
      export function f() {
        console.log("hello");
      }
    `;
    const v = collectViolations("apps/desktop/src/main/x.ts", "ts", text);
    expect(v.length).toBe(1);
    expect(v[0].call).toBe("console.log");
  });

  test("ignores console.log inside line comments", () => {
    const text = `// console.log("hello")
      const x = 1;`;
    expect(collectViolations("x.ts", "ts", text)).toEqual([]);
  });

  test("ignores console.log inside block comments", () => {
    const text = `/* example: console.log("hello") */ const x = 1;`;
    expect(collectViolations("x.ts", "ts", text)).toEqual([]);
  });

  test("flags every console.* level", () => {
    const text = `
      console.log("a");
      console.info("b");
      console.warn("c");
      console.error("d");
      console.debug("e");
    `;
    const v = collectViolations("x.ts", "ts", text);
    expect(v.length).toBe(5);
    expect(v.map((x) => x.call)).toEqual([
      "console.log",
      "console.info",
      "console.warn",
      "console.error",
      "console.debug",
    ]);
  });
});

describe("collectViolations (Go)", () => {
  test("flags fmt.Println in production code", () => {
    const text = `
      package main
      import "fmt"
      func main() {
        fmt.Println("hi")
      }
    `;
    const v = collectViolations("apps/daemon/internal/foo/foo.go", "go", text);
    expect(v.length).toBe(1);
    expect(v[0].call).toBe("fmt.Println");
  });

  test("flags every blacklisted Go call", () => {
    const text = `
      fmt.Println("a")
      fmt.Printf("%s", "b")
      fmt.Print("c")
      log.Println("d")
      log.Printf("%s", "e")
      log.Print("f")
      log.Fatalf("%s", "g")
      log.Fatal("h")
      log.Panicln("i")
      log.Panicf("%s", "j")
    `;
    const v = collectViolations("x.go", "go", text);
    expect(v.length).toBe(10);
  });

  test("ignores // comments", () => {
    const text = `
      // fmt.Println("hi")
      x := 1
    `;
    expect(collectViolations("x.go", "go", text)).toEqual([]);
  });
});

describe("scan", () => {
  test("repo is currently clean (no raw-logging violations)", () => {
    const violations = scan();
    if (violations.length > 0) {
      const sample = violations.slice(0, 5).map((v) => `${v.filePath}:${v.line} ${v.call}`);
      throw new Error(
        `unexpected raw-logging violations:\n${sample.join("\n")}\n(${violations.length} total)`,
      );
    }
    expect(violations).toEqual([]);
  });
});
