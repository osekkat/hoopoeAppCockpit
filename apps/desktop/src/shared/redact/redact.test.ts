import { describe, expect, test } from "bun:test";
import { Redactor, SurfaceLogger, SurfaceAudit, SurfaceEvents } from "./index.ts";
import type { TraceEvent } from "./index.ts";

const F = (now = "2026-05-04T00:00:00Z") => new Redactor(() => new Date(now));

describe("Redactor.redactText", () => {
  // DSA isn't in the regex's prefix alternation list (RSA|OPENSSH|EC|PGP)
  // — the canonical Go patterns.go has the same gap. PGP must use the
  // BLOCK suffix (separate test below).
  test.each(["RSA", "OPENSSH", "EC"])("private key block (%s)", (kind) => {
    const r = F();
    const input = `loading: -----BEGIN ${kind} PRIVATE KEY-----\nbody\n-----END ${kind} PRIVATE KEY-----\nok`;
    const { redacted, events } = r.redactText(SurfaceLogger, "test", input);
    expect(redacted).toContain("[private-key-redacted]");
    expect(events[0].patternId).toBe("private-key-block");
  });

  test("PGP private key block", () => {
    const r = F();
    const input = `-----BEGIN PGP PRIVATE KEY BLOCK-----\nbody\n-----END PGP PRIVATE KEY BLOCK-----`;
    const { redacted } = r.redactText(SurfaceLogger, "test", input);
    expect(redacted).toBe("[private-key-redacted]");
  });

  test("JWT bearer-hmac", () => {
    const r = F();
    const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c";
    const { redacted, events } = r.redactText(SurfaceLogger, "msg", `Bearer ${jwt}`);
    expect(redacted).not.toContain(jwt);
    // Either bearer-hmac fires (JWT shape) or bearer-token (generic Bearer).
    expect(events.some((e) => e.patternId === "bearer-hmac" || e.patternId === "bearer-token")).toBe(true);
  });

  test("generic Bearer token (non-JWT)", () => {
    const r = F();
    const { redacted, events } = r.redactText(SurfaceLogger, "header", "Bearer abc123def456ghi789jkl012mno345");
    expect(redacted).not.toContain("abc123def456");
    expect(events.find((e) => e.patternId === "bearer-token")).toBeDefined();
  });

  test.each([
    ["openai", "sk-abcdef0123456789ABCDEF0123456789", "provider-key-sk"],
    ["anthropic", "sk-ant-api03-abcdefABCDEF1234567890", "provider-key-anthropic"],
    ["google", "AIzaSyA0123456789abcdefghijklmnopqrstuv", "provider-key-google"],
    ["aws", "AKIAIOSFODNN7EXAMPLE", "provider-key-aws"],
    ["github", "ghp_abcdefghijklmnopqrstuvwxyz0123456789", "provider-key-github"],
    ["slack", "xoxb-123456789012-1234567890123-AbCdEfGhIjKl", "provider-key-slack"],
  ])("provider key (%s)", (_n, secret, patternId) => {
    const r = F();
    const { redacted, events } = r.redactText(SurfaceLogger, "test", `KEY=${secret}`);
    expect(redacted).not.toContain(secret);
    expect(events.find((e) => e.patternId === patternId)).toBeDefined();
  });

  test("pairing token", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "test", "paired with H-ABCDEFGHJKM");
    expect(redacted).toContain("[pairing-token-redacted]");
  });

  test("HTTP Authorization header", () => {
    const r = F();
    const { redacted, events } = r.redactText(
      SurfaceAudit,
      "request",
      "Authorization: Bearer sk-abcdef0123456789ABCDEF0123456789",
    );
    expect(redacted).not.toContain("sk-abcdef");
    expect(events.find((e) => e.patternId === "http-header-authorization")).toBeDefined();
  });

  test("HTTP Cookie + Set-Cookie headers", () => {
    const r = F();
    const { redacted: rSet } = r.redactText(SurfaceLogger, "h", "Set-Cookie: session=secret; Path=/");
    expect(rSet).not.toContain("secret");
    expect(rSet).toContain("[redacted-header]");

    const { redacted: rReq } = r.redactText(SurfaceLogger, "h", "Cookie: session=secret");
    expect(rReq).not.toContain("secret");
  });

  test("SSH passphrase", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "Passphrase=hunter2");
    expect(redacted).not.toContain("hunter2");
  });

  test("ChatGPT cookie", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "__Secure-next-auth.session-token=abc.def.ghi");
    expect(redacted).not.toContain("abc.def.ghi");
  });

  test("Claude session cookie", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "claude_session=abc123");
    expect(redacted).not.toContain("abc123");
    expect(redacted).toContain("claude-session=[redacted]");
  });

  test("OAI session cookie", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "oai_session=abc123");
    expect(redacted).not.toContain("abc123");
  });

  test("Telegram bot token", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "token=123456789:AAEhBP0avQ7AdEXAMPLE_THISisJUST_a_PLACEHOLDER");
    expect(redacted).not.toContain("123456789:AAE");
  });

  test("email last-4", () => {
    const r = F();
    const { redacted } = r.redactText(SurfaceLogger, "h", "notify alice@example.com");
    expect(redacted).not.toContain("alice@example.com");
    expect(redacted).toContain("@example.com");
  });

  test("sensitive file paths", () => {
    const r = F();
    for (const input of [
      "reading ~/.ssh/id_rsa",
      "shadow at /etc/shadow",
      "keychain at /private/var/db/login.keychain-db",
    ]) {
      const { redacted, events } = r.redactText(SurfaceLogger, "h", input);
      expect(redacted).not.toBe(input);
      expect(events.length).toBeGreaterThan(0);
    }
  });

  test("oracle profile path + user-home + vps-project", () => {
    const r = F();
    for (const input of [
      "loaded /home/ubuntu/.config/oracle/profiles/main",
      "loaded /Users/admin/.oracle/foo",
      "/data/projects/my-repo/build/output.json",
    ]) {
      const { redacted } = r.redactText(SurfaceLogger, "h", input);
      expect(redacted).not.toBe(input);
    }
  });

  // Adversarial cases per hp-je1p DOD.
  test("adversarial: nested in JSON", () => {
    const r = F();
    const { redacted } = r.redactText(
      SurfaceEvents,
      "payload",
      `{"args":{"key":"sk-abcdef0123456789ABCDEF0123456789"}}`,
    );
    expect(redacted).not.toContain("sk-abcdef");
  });

  test("adversarial: URL query string", () => {
    const r = F();
    const { redacted } = r.redactText(
      SurfaceLogger,
      "msg",
      "GET /v1/jobs?token=sk-abcdef0123456789ABCDEF0123456789",
    );
    expect(redacted).not.toContain("sk-abcdef");
  });

  test("adversarial: stack trace", () => {
    const r = F();
    const stack = `Error
    at handleAuth (auth.ts:42)
    bearer=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`;
    const { redacted } = r.redactText(SurfaceLogger, "msg", stack);
    expect(redacted).not.toContain("eyJhbGciOiJIUzI1NiJ9");
  });

  test("adversarial: multi-line value", () => {
    const r = F();
    const input = `payload <<EOF
sk-abcdef0123456789ABCDEF0123456789
EOF`;
    const { redacted } = r.redactText(SurfaceLogger, "msg", input);
    expect(redacted).not.toContain("sk-abcdef");
  });

  test("multiple secrets in one input", () => {
    const r = F();
    const input = "k1=sk-abcdef0123456789ABCDEF0123456789 k2=AKIAIOSFODNN7EXAMPLE k3=ghp_abcdefghijklmnopqrstuvwxyz0123456789";
    const { redacted, events } = r.redactText(SurfaceLogger, "msg", input);
    expect(redacted).not.toContain("sk-abcdef");
    expect(redacted).not.toContain("AKIAIOSF");
    expect(redacted).not.toContain("ghp_abc");
    expect(events.length).toBeGreaterThanOrEqual(3);
  });

  test("event carries surface + context + bytes + count", () => {
    const r = F();
    const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c";
    const { events } = r.redactText(SurfaceLogger, "msg", jwt);
    const e = events.find((ev) => ev.patternId === "bearer-hmac");
    expect(e).toBeDefined();
    expect(e!.redactor).toBe(SurfaceLogger);
    expect(e!.context).toBe("msg");
    expect(e!.bytesRedacted).toBe(jwt.length);
    expect(e!.count).toBe(1);
  });

  test("empty input → empty events", () => {
    const r = F();
    const { redacted, events } = r.redactText(SurfaceLogger, "msg", "");
    expect(redacted).toBe("");
    expect(events).toEqual([]);
  });

  test("patternIds is stable + complete (drift detector input)", () => {
    const r = F();
    const ids = r.patternIds();
    expect(ids).toEqual([
      "http-header-authorization",
      "http-header-cookie",
      "http-header-set-cookie",
      "private-key-block",
      "bearer-hmac",
      "bearer-token",
      "provider-key-anthropic",
      "provider-key-sk",
      "provider-key-google",
      "provider-key-aws",
      "provider-key-github",
      "provider-key-slack",
      "pairing-token",
      "ssh-passphrase",
      "browser-cookie-chatgpt",
      "browser-cookie-claude",
      "browser-cookie-oai",
      "telegram-bot-token",
      "email-address",
      "ssh-key-path",
      "shadow-file-path",
      "macos-private-db-path",
      "oracle-profile-path",
      "user-home-path",
      "vps-project-path",
    ]);
  });
});

describe("Redactor.redactValue", () => {
  test("walks nested maps + arrays + propagates context", () => {
    const r = F();
    const input = {
      outer: {
        key: "sk-abcdef0123456789ABCDEF0123456789",
        arr: ["AKIAIOSFODNN7EXAMPLE", "no secret"],
      },
    };
    const { redacted, events } = r.redactValue(SurfaceLogger, "data", input as never);
    const out = redacted as { outer: { key: string; arr: string[] } };
    expect(out.outer.key).not.toContain("sk-abcdef");
    expect(out.outer.arr[0]).not.toContain("AKIAIOSF");
    // Context paths should reflect the walk.
    expect(events.some((e: TraceEvent) => e.context === "data.outer.key")).toBe(true);
    expect(events.some((e: TraceEvent) => e.context === "data.outer.arr[0]")).toBe(true);
  });
});

describe("Redactor.snapshotStats", () => {
  test("stats accumulate across calls", () => {
    const r = F();
    // Plain `key=...` (no Bearer prefix) so provider-key-sk matches
    // directly. With "Bearer sk-..." the bearer-token regex would absorb
    // the token first.
    r.redactText(SurfaceLogger, "msg", "key=sk-abcdef0123456789ABCDEF0123456789");
    r.redactText(SurfaceAudit, "audit", "ghp_abcdefghijklmnopqrstuvwxyz0123456789");
    const snap = r.snapshotStats();
    expect(snap.schemaVersion).toBe(1);
    expect(snap.patterns.length).toBeGreaterThanOrEqual(2);
    const names = snap.patterns.map((p) => p.patternId);
    expect(names).toContain("provider-key-sk");
    expect(names).toContain("provider-key-github");
  });

  test("resetStats clears the accumulator", () => {
    const r = F();
    r.redactText(SurfaceLogger, "h", "Bearer sk-abcdef0123456789ABCDEF0123456789");
    expect(r.snapshotStats().patterns.length).toBeGreaterThan(0);
    r.resetStats();
    expect(r.snapshotStats().patterns).toEqual([]);
  });
});
