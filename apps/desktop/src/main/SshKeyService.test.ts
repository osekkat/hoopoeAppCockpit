import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  SshKeyService,
  SshKeyServiceError,
  parseSshKeygenLine,
  type ChildProcessRunner,
  type SshKeyAuditEvent,
} from "./SshKeyService.ts";

interface CallLog {
  readonly command: string;
  readonly args: readonly string[];
}

function makeRunner(handler: (call: CallLog) => Promise<{ code: number; stdout: string; stderr: string }>): {
  readonly runner: ChildProcessRunner;
  readonly calls: readonly CallLog[];
} {
  const calls: CallLog[] = [];
  const runner: ChildProcessRunner = async (command, args) => {
    const call = { command, args: args.slice() };
    calls.push(call);
    return handler(call);
  };
  return { runner, calls };
}

describe("hp-pl8h :: SshKeyService.listKeys", () => {
  let homeDir: string;

  beforeEach(async () => {
    homeDir = await mkdtemp(join(tmpdir(), "hoopoe-ssh-list-"));
    await mkdir(join(homeDir, ".ssh"), { mode: 0o700 });
  });

  afterEach(async () => {
    await rm(homeDir, { recursive: true, force: true });
  });

  test("returns an empty result when ~/.ssh/ does not exist", async () => {
    await rm(join(homeDir, ".ssh"), { recursive: true });
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({ homeDir, run: runner });
    expect(await service.listKeys()).toEqual({
      keys: [],
      truncated: false,
      totalCandidates: 0,
    });
  });

  test("lists *.pub files; ignores private keys; reports algorithm + fingerprint + comment", async () => {
    await writeFile(join(homeDir, ".ssh", "id_ed25519.pub"), "ssh-ed25519 AAAA fixture\n");
    await writeFile(join(homeDir, ".ssh", "id_ed25519"), "PRIVATE\n");
    await writeFile(join(homeDir, ".ssh", "id_rsa.pub"), "ssh-rsa AAAA legacy\n");

    const { runner, calls } = makeRunner(async (call) => {
      const path = call.args[2] ?? "";
      if (path.endsWith("id_ed25519.pub")) {
        return {
          code: 0,
          stdout: "256 SHA256:HASHED-ED25519 user@host (ED25519)\n",
          stderr: "",
        };
      }
      if (path.endsWith("id_rsa.pub")) {
        return {
          code: 0,
          stdout: "2048 SHA256:HASHED-RSA legacy@old (RSA)\n",
          stderr: "",
        };
      }
      return { code: 1, stdout: "", stderr: "unexpected" };
    });

    const service = new SshKeyService({ homeDir, run: runner });
    const result = await service.listKeys();
    expect(result.truncated).toBe(false);
    expect(result.totalCandidates).toBe(2);
    expect(result.keys.map((k) => k.name)).toEqual(["id_ed25519.pub", "id_rsa.pub"]);

    const ed = result.keys[0]!;
    expect(ed.algorithm).toBe("ed25519");
    expect(ed.fingerprint).toBe("SHA256:HASHED-ED25519");
    expect(ed.comment).toBe("user@host");
    expect(ed.bits).toBe(256);
    expect(ed.hasPrivateKey).toBe(true);

    const rsa = result.keys[1]!;
    expect(rsa.algorithm).toBe("rsa");
    expect(rsa.bits).toBe(2048);
    expect(rsa.hasPrivateKey).toBe(false);

    expect(calls.every((call) => call.args[0] === "-l" && call.args[1] === "-f")).toBe(true);
  });

  test("ssh-keygen failures surface as 'unknown' entries instead of blanking the list", async () => {
    await writeFile(join(homeDir, ".ssh", "good.pub"), "ssh-ed25519 AAA ok\n");
    await writeFile(join(homeDir, ".ssh", "broken.pub"), "garbage\n");

    const { runner } = makeRunner(async (call) => {
      const path = call.args[2] ?? "";
      if (path.endsWith("broken.pub")) {
        return { code: 255, stdout: "", stderr: "not a valid key" };
      }
      return {
        code: 0,
        stdout: "256 SHA256:OK demo@host (ED25519)\n",
        stderr: "",
      };
    });
    const service = new SshKeyService({ homeDir, run: runner });
    const result = await service.listKeys();
    expect(result.keys).toHaveLength(2);
    expect(result.truncated).toBe(false);
    const broken = result.keys.find((k) => k.name === "broken.pub")!;
    expect(broken.algorithm).toBe("unknown");
    expect(broken.fingerprint).toBe("");
  });

  test("hp-kqtt: caps fingerprint calls at maxListedKeys and reports truncated=true", async () => {
    // Seed 5 *.pub files; cap at 2. The runner will be invoked at
    // most 2 times (once per kept entry); the remaining 3 must NOT
    // trigger ssh-keygen child processes.
    for (let i = 0; i < 5; i += 1) {
      await writeFile(join(homeDir, ".ssh", `id_${i}.pub`), `ssh-ed25519 AAA key-${i}\n`);
    }
    const fingerprintCalls: string[] = [];
    const { runner } = makeRunner(async (call) => {
      const path = call.args[2] ?? "";
      fingerprintCalls.push(path);
      return {
        code: 0,
        stdout: `256 SHA256:HASHED-${path} demo@host (ED25519)\n`,
        stderr: "",
      };
    });
    const service = new SshKeyService({ homeDir, run: runner, maxListedKeys: 2 });

    const result = await service.listKeys();

    expect(result.truncated).toBe(true);
    expect(result.totalCandidates).toBe(5);
    expect(result.keys).toHaveLength(2);
    // Sort is deterministic — first two of [id_0..id_4].pub are kept.
    expect(result.keys.map((k) => k.name)).toEqual(["id_0.pub", "id_1.pub"]);
    // Crucially: only 2 child processes spawned, not 5.
    expect(fingerprintCalls).toHaveLength(2);
  });

  test("hp-kqtt: corrupted entries inside the cap stay surfaced as 'unknown' (cap doesn't suppress them)", async () => {
    // Sort order is "a-bad.pub" < "b-good.pub" < "z-extra.pub" — cap of
    // 2 keeps the first two, so the test asserts behavior on a corrupted
    // entry that landed INSIDE the cap (a-bad.pub).
    await writeFile(join(homeDir, ".ssh", "a-bad.pub"), "garbage\n");
    await writeFile(join(homeDir, ".ssh", "b-good.pub"), "ssh-ed25519 AAA ok\n");
    await writeFile(join(homeDir, ".ssh", "z-extra.pub"), "ssh-ed25519 AAA ok\n");

    const { runner } = makeRunner(async (call) => {
      const path = call.args[2] ?? "";
      if (path.endsWith("a-bad.pub")) {
        return { code: 255, stdout: "", stderr: "not a valid key" };
      }
      return {
        code: 0,
        stdout: "256 SHA256:OK demo@host (ED25519)\n",
        stderr: "",
      };
    });
    const service = new SshKeyService({ homeDir, run: runner, maxListedKeys: 2 });

    const result = await service.listKeys();

    expect(result.truncated).toBe(true);
    expect(result.totalCandidates).toBe(3);
    expect(result.keys.map((k) => k.name)).toEqual(["a-bad.pub", "b-good.pub"]);
    const broken = result.keys.find((k) => k.name === "a-bad.pub")!;
    expect(broken.algorithm).toBe("unknown");
    expect(broken.fingerprint).toBe("");
  });

  test("hp-kqtt: rejects non-positive maxListedKeys at construction", () => {
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    expect(
      () => new SshKeyService({ homeDir, run: runner, maxListedKeys: 0 }),
    ).toThrow();
    expect(
      () => new SshKeyService({ homeDir, run: runner, maxListedKeys: -1 }),
    ).toThrow();
    expect(
      () => new SshKeyService({ homeDir, run: runner, maxListedKeys: 1.5 }),
    ).toThrow();
  });
});

describe("hp-pl8h :: SshKeyService.generateKey", () => {
  let homeDir: string;

  beforeEach(async () => {
    homeDir = await mkdtemp(join(tmpdir(), "hoopoe-ssh-gen-"));
  });

  afterEach(async () => {
    await rm(homeDir, { recursive: true, force: true });
  });

  test("rejects runIds that don't match the strict allowlist", async () => {
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({ homeDir, run: runner });
    for (const runId of ["", "../etc/shadow", "abc def", "../bad", ";rm -rf /", "$(whoami)"]) {
      await expect(service.generateKey({ runId })).rejects.toBeInstanceOf(SshKeyServiceError);
    }
  });

  test("rejects comments containing characters outside the ASCII safe-set", async () => {
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({ homeDir, run: runner });
    await expect(
      service.generateKey({ runId: "abcd1234efgh", comment: "$(rm -rf /)" }),
    ).rejects.toMatchObject({ code: "ssh.comment-invalid" });
  });

  test("invokes ssh-keygen with explicit argv (no shell, no metacharacters in path)", async () => {
    const { runner, calls } = makeRunner(async (call) => {
      if (call.args[0] === "-l") {
        return { code: 0, stdout: "256 SHA256:NEW hoopoe-vps-abcd1234efgh (ED25519)\n", stderr: "" };
      }
      // generate run — write fake key files so the fingerprint step can probe them.
      const fileFlagIndex = call.args.indexOf("-f");
      const targetPath = call.args[fileFlagIndex + 1] ?? "";
      if (targetPath) {
        await writeFile(targetPath, "PRIVATE\n");
        await writeFile(`${targetPath}.pub`, "ssh-ed25519 AAA hoopoe-vps-abcd1234efgh\n");
      }
      return { code: 0, stdout: "", stderr: "" };
    });
    const service = new SshKeyService({ homeDir, run: runner });
    const result = await service.generateKey({ runId: "abcd1234efgh" });

    expect(result.algorithm).toBe("ed25519");
    expect(result.fingerprint).toBe("SHA256:NEW");
    expect(result.privatePath.endsWith("/.ssh/hoopoe-vps-abcd1234efgh")).toBe(true);
    expect(result.path.endsWith("/.ssh/hoopoe-vps-abcd1234efgh.pub")).toBe(true);

    const generateCall = calls[0]!;
    expect(generateCall.command).toBe("ssh-keygen");
    expect(generateCall.args[0]).toBe("-t");
    expect(generateCall.args[1]).toBe("ed25519");
    expect(generateCall.args[4]).toBe("-N");
    expect(generateCall.args[5]).toBe("");
    expect(generateCall.args[6]).toBe("-C");
    expect(generateCall.args[7]).toBe("hoopoe-vps-abcd1234efgh");
  });

  test("refuses to overwrite an existing key for the same runId", async () => {
    await mkdir(join(homeDir, ".ssh"), { mode: 0o700 });
    await writeFile(join(homeDir, ".ssh", "hoopoe-vps-abcd1234efgh"), "PRIVATE\n");
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({ homeDir, run: runner });
    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh.key-already-exists",
    });
  });

  test("ssh-keygen exit-code != 0 surfaces as ssh-keygen-exit with stderr", async () => {
    const { runner } = makeRunner(async () => ({
      code: 1,
      stdout: "",
      stderr: "ssh-keygen: bad permissions",
    }));
    const service = new SshKeyService({ homeDir, run: runner });
    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh-keygen-exit",
    });
  });

  test("missing ssh-keygen binary surfaces as ssh-keygen-not-installed", async () => {
    const runner: ChildProcessRunner = async () => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    };
    const service = new SshKeyService({ homeDir, run: runner });
    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh-keygen-not-installed",
    });
  });

  test("hp-ig3r: succeeded path emits ssh.key_generation_succeeded with public fingerprint", async () => {
    const audit: SshKeyAuditEvent[] = [];
    const { runner } = makeRunner(async (call) => {
      if (call.args[0] === "-l") {
        return {
          code: 0,
          stdout: "256 SHA256:GENERATED hoopoe-vps-abcd1234efgh (ED25519)\n",
          stderr: "",
        };
      }
      const fileFlagIndex = call.args.indexOf("-f");
      const targetPath = call.args[fileFlagIndex + 1] ?? "";
      if (targetPath) {
        await writeFile(targetPath, "PRIVATE\n");
        await writeFile(`${targetPath}.pub`, "ssh-ed25519 AAA hoopoe-vps-abcd1234efgh\n");
      }
      return { code: 0, stdout: "", stderr: "" };
    });
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: (event) => audit.push(event),
      now: () => new Date("2026-05-04T01:02:03.456Z"),
    });

    await service.generateKey({ runId: "abcd1234efgh" });

    expect(audit).toEqual([
      {
        kind: "ssh.key_generation_succeeded",
        at: "2026-05-04T01:02:03.456Z",
        runId: "abcd1234efgh",
        keyName: "hoopoe-vps-abcd1234efgh",
        algorithm: "ed25519",
        fingerprint: "SHA256:GENERATED",
      },
    ]);
  });

  test("hp-ig3r: invalid runId emits ssh.key_generation_refused with the rejected runId", async () => {
    const audit: SshKeyAuditEvent[] = [];
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: (event) => audit.push(event),
      now: () => new Date("2026-05-04T01:02:03.456Z"),
    });

    await expect(service.generateKey({ runId: "../etc/shadow" })).rejects.toBeInstanceOf(
      SshKeyServiceError,
    );

    expect(audit).toHaveLength(1);
    expect(audit[0]).toMatchObject({
      kind: "ssh.key_generation_refused",
      at: "2026-05-04T01:02:03.456Z",
      runId: "../etc/shadow",
      errorCode: "ssh.runId-invalid",
    });
  });

  test("hp-ig3r: existing key path emits ssh.key_generation_refused with key-already-exists", async () => {
    const audit: SshKeyAuditEvent[] = [];
    await mkdir(join(homeDir, ".ssh"), { mode: 0o700 });
    await writeFile(join(homeDir, ".ssh", "hoopoe-vps-abcd1234efgh"), "PRIVATE\n");
    const { runner } = makeRunner(async () => ({ code: 0, stdout: "", stderr: "" }));
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: (event) => audit.push(event),
    });

    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh.key-already-exists",
    });

    expect(audit).toHaveLength(1);
    expect(audit[0]!.kind).toBe("ssh.key_generation_refused");
    if (audit[0]!.kind === "ssh.key_generation_refused") {
      expect(audit[0]!.errorCode).toBe("ssh.key-already-exists");
      expect(audit[0]!.runId).toBe("abcd1234efgh");
    }
  });

  test("hp-ig3r: ssh-keygen non-zero exit emits ssh.key_generation_failed (not refused)", async () => {
    const audit: SshKeyAuditEvent[] = [];
    const { runner } = makeRunner(async () => ({
      code: 1,
      stdout: "",
      stderr: "ssh-keygen: bad permissions",
    }));
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: (event) => audit.push(event),
    });

    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh-keygen-exit",
    });

    expect(audit).toHaveLength(1);
    expect(audit[0]!.kind).toBe("ssh.key_generation_failed");
    if (audit[0]!.kind === "ssh.key_generation_failed") {
      expect(audit[0]!.errorCode).toBe("ssh-keygen-exit");
      expect(audit[0]!.errorMessage).toContain("bad permissions");
    }
  });

  test("hp-ig3r: missing ssh-keygen binary emits ssh.key_generation_failed with not-installed code", async () => {
    const audit: SshKeyAuditEvent[] = [];
    const runner: ChildProcessRunner = async () => {
      throw Object.assign(new Error("ENOENT"), { code: "ENOENT" });
    };
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: (event) => audit.push(event),
    });

    await expect(service.generateKey({ runId: "abcd1234efgh" })).rejects.toMatchObject({
      code: "ssh-keygen-not-installed",
    });

    expect(audit).toHaveLength(1);
    expect(audit[0]!.kind).toBe("ssh.key_generation_failed");
    if (audit[0]!.kind === "ssh.key_generation_failed") {
      expect(audit[0]!.errorCode).toBe("ssh-keygen-not-installed");
    }
  });

  test("hp-ig3r: a throwing audit sink does not derail key generation", async () => {
    const { runner } = makeRunner(async (call) => {
      if (call.args[0] === "-l") {
        return {
          code: 0,
          stdout: "256 SHA256:OK hoopoe-vps-abcd1234efgh (ED25519)\n",
          stderr: "",
        };
      }
      const fileFlagIndex = call.args.indexOf("-f");
      const targetPath = call.args[fileFlagIndex + 1] ?? "";
      if (targetPath) {
        await writeFile(targetPath, "PRIVATE\n");
        await writeFile(`${targetPath}.pub`, "ssh-ed25519 AAA hoopoe-vps-abcd1234efgh\n");
      }
      return { code: 0, stdout: "", stderr: "" };
    });
    const service = new SshKeyService({
      homeDir,
      run: runner,
      audit: () => {
        throw new Error("audit sink exploded");
      },
    });

    const result = await service.generateKey({ runId: "abcd1234efgh" });
    expect(result.fingerprint).toBe("SHA256:OK");
  });
});

describe("hp-pl8h :: parseSshKeygenLine", () => {
  test("parses bits + fingerprint + comment + algorithm", () => {
    const parsed = parseSshKeygenLine("256 SHA256:Nz1B+abc demo@host (ED25519)\n");
    expect(parsed.bits).toBe(256);
    expect(parsed.fingerprint).toBe("SHA256:Nz1B+abc");
    expect(parsed.comment).toBe("demo@host");
    expect(parsed.algorithm).toBe("ed25519");
  });

  test("falls back to algorithm=unknown when ssh-keygen returns an unfamiliar shape", () => {
    const parsed = parseSshKeygenLine("weird output\n");
    expect(parsed.algorithm).toBe("unknown");
  });

  test("treats RSA bit length 4096 correctly", () => {
    const parsed = parseSshKeygenLine("4096 SHA256:RSA legacy@old (RSA)\n");
    expect(parsed.bits).toBe(4096);
    expect(parsed.algorithm).toBe("rsa");
  });

  test("throws when ssh-keygen produces no output", () => {
    expect(() => parseSshKeygenLine("")).toThrow(SshKeyServiceError);
  });
});
