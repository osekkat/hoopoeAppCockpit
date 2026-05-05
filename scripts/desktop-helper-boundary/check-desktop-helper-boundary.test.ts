import { expect, test } from "bun:test";
import { scanSource } from "./check-desktop-helper-boundary.ts";

const root = process.cwd();
const path = (rel: string): string => `${root}/${rel}`;

test("desktop helper boundary: allows daemon bootstrap dynamic spawn only in BackendLifecycle", () => {
  const findings = scanSource(
    path("apps/desktop/src/main/BackendLifecycle.ts"),
    `const child = spawnImpl(options.daemonBinaryPath, args, spawnOptions);`,
  );
  expect(findings).toEqual([]);
});

test("desktop helper boundary: allows ssh-keygen helper", () => {
  const findings = scanSource(
    path("apps/desktop/src/main/SshKeyService.ts"),
    `const child = spawn("ssh-keygen", ["-t", "ed25519"]);`,
  );
  expect(findings).toEqual([]);
});

test("desktop helper boundary: allows read-only local clone git plumbing", () => {
  const findings = scanSource(
    path("apps/desktop/electron/clone/git.ts"),
    `const result = run("git", ["fetch", "--all", "--tags", "--prune"]);`,
  );
  expect(findings).toEqual([]);
});

test("desktop helper boundary: allows project readiness br init but not br mutation", () => {
  expect(
    scanSource(
      path("apps/desktop/electron/projects/lifecycle.ts"),
      `const result = run("br", ["init"], { cwd: root });`,
    ),
  ).toEqual([]);

  const findings = scanSource(
    path("apps/desktop/electron/projects/lifecycle.ts"),
    `const result = run("br", ["update", beadId, "--status", "closed"], { cwd: root });`,
  );
  expect(findings).toHaveLength(1);
  expect(findings[0]?.message).toContain("br update");
});

test("desktop helper boundary: rejects direct project jobs in desktop source", () => {
  const findings = scanSource(
    path("apps/desktop/src/main/UnsafeProjectJob.ts"),
    `const child = spawn("ntm", ["launch", "--swarm"]);`,
  );
  expect(findings).toHaveLength(1);
  expect(findings[0]?.rule).toBe("desktop-helper.unapproved-process");
});

test("desktop helper boundary: rejects build and test subprocesses in production desktop code", () => {
  const findings = scanSource(
    path("apps/desktop/electron/projects/buildRunner.ts"),
    `const child = spawn("bun", ["run", "test"]);`,
  );
  expect(findings).toHaveLength(1);
  expect(findings[0]?.command).toBe("bun");
});

test("desktop helper boundary: ignores tests and comments", () => {
  expect(
    scanSource(
      path("apps/desktop/src/main/Foo.test.ts"),
      `const child = spawn("ntm", ["launch"]);`,
    ),
  ).toEqual([]);

  expect(
    scanSource(
      path("apps/desktop/src/main/Foo.ts"),
      `// const child = spawn("ntm", ["launch"]);`,
    ),
  ).toEqual([]);
});
