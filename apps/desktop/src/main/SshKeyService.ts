// hp-pl8h — SSH key wizard step backing service.
//
// Two operations, both invoked by the renderer over the typed preload
// bridge:
//
//   1. listKeys() — read entries under `~/.ssh/`, parse each `*.pub`
//      file with `ssh-keygen -l -f <path>`, and surface
//      `{name, path, fingerprint, algorithm, comment}`.
//   2. generateKey({ runId, comment }) — generate a fresh ed25519 key
//      via `ssh-keygen -t ed25519 -f <derived path> -N "" -C <comment>`.
//      The file path is DERIVED from the runId (a UUID) — the renderer
//      never supplies the path. The comment is sanitized to a strict
//      allowlist before reaching the child process.
//
// Security model (Guardrail 2):
//   - argv is always explicit — no shell, no user-string interpolation.
//   - paths are derived inside the service from a validated runId.
//   - the only directory accessed is `~/.ssh/`; operations refuse to
//     read or write anywhere else.
//   - failures throw `SshKeyServiceError` with stable codes so the
//     renderer can render the matching `Resume` CTA.

import { spawn } from "node:child_process";
import { constants as fsConstants, promises as fs } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, isAbsolute, join, resolve, sep } from "node:path";

export const SSH_KEY_ALGORITHMS = ["ed25519", "rsa", "ecdsa", "dsa"] as const;
export type SshKeyAlgorithm = (typeof SSH_KEY_ALGORITHMS)[number];

export interface ListedSshKey {
  /** Filename of the public key, e.g. `id_ed25519.pub`. */
  readonly name: string;
  /** Absolute path to the public key inside `~/.ssh/`. */
  readonly path: string;
  /** Algorithm parsed from the key file. */
  readonly algorithm: SshKeyAlgorithm | "unknown";
  /** SSH key fingerprint as reported by `ssh-keygen -l`. */
  readonly fingerprint: string;
  /** Comment field from the public key (often the user@host). */
  readonly comment: string;
  /** Bit length when reported by `ssh-keygen -l`. */
  readonly bits: number | null;
  /** True iff a matching private-key sibling exists in `~/.ssh/`. */
  readonly hasPrivateKey: boolean;
}

export interface GeneratedSshKey extends ListedSshKey {
  /** Absolute path to the new private key (e.g. `~/.ssh/hoopoe-vps-...`). */
  readonly privatePath: string;
}

export interface GenerateKeyInput {
  /** UUID v4 — used to derive the new key filename. Validated below. */
  readonly runId: string;
  /** Optional, free-form comment. Sanitized before reaching ssh-keygen. */
  readonly comment?: string;
}

export class SshKeyServiceError extends Error {
  override readonly name = "SshKeyServiceError";
  readonly code: SshKeyServiceErrorCode;
  readonly details: Readonly<Record<string, string>>;

  constructor(code: SshKeyServiceErrorCode, message: string, details: Readonly<Record<string, string>> = {}) {
    super(message);
    this.code = code;
    this.details = details;
  }
}

export type SshKeyServiceErrorCode =
  | "ssh.runId-invalid"
  | "ssh.comment-invalid"
  | "ssh.dir-not-readable"
  | "ssh.path-escape"
  | "ssh.key-already-exists"
  | "ssh-keygen-not-installed"
  | "ssh-keygen-exit"
  | "ssh-keygen-no-fingerprint"
  | "ssh-fingerprint-failed";

export interface SshKeyServiceOptions {
  readonly homeDir?: string;
  readonly sshKeygenBin?: string;
  readonly run?: ChildProcessRunner;
  /** hp-ig3r: optional audit sink. Production wiring routes events
   *  into the structured logger; tests inject a spy. The sink MUST
   *  NOT throw — the service swallows sink errors so a bad logger
   *  cannot block key generation. */
  readonly audit?: SshKeyAuditSink;
  /** hp-ig3r: clock for the `at` timestamp on audit events.
   *  Defaults to `() => new Date()`. */
  readonly now?: () => Date;
}

/** hp-ig3r: events emitted by SshKeyService.generateKey so the audit log
 *  can show when a wizard run created (or refused, or failed to create)
 *  an SSH key. Private key bytes and passphrases never reach the sink —
 *  generateKey runs ssh-keygen with `-N ""` (no passphrase) and the only
 *  sensitive material is the on-disk private key file, which is not
 *  read back into the audit payload. The success event carries the
 *  PUBLIC fingerprint (already a SHA256:base64 hash). */
export type SshKeyAuditEvent =
  | {
      readonly kind: "ssh.key_generation_succeeded";
      readonly at: string;
      readonly runId: string;
      readonly keyName: string;
      readonly algorithm: SshKeyAlgorithm | "unknown";
      readonly fingerprint: string;
    }
  | {
      readonly kind: "ssh.key_generation_refused";
      readonly at: string;
      /** The raw runId from the renderer — validation may have rejected
       *  it as malformed, but it's not secret material. */
      readonly runId: string;
      readonly errorCode: SshKeyServiceErrorCode;
      readonly errorMessage: string;
    }
  | {
      readonly kind: "ssh.key_generation_failed";
      readonly at: string;
      readonly runId: string;
      readonly errorCode: SshKeyServiceErrorCode;
      readonly errorMessage: string;
    };

export type SshKeyAuditSink = (event: SshKeyAuditEvent) => void;

/** Classify an SshKeyServiceErrorCode into the refused vs failed audit
 *  bucket. Refused = validation/precondition rejection driven by input
 *  shape or filesystem state; failed = external tool / system error.
 *  Used by generateKey's catch handler to pick the right event kind. */
const REFUSED_CODES: ReadonlySet<SshKeyServiceErrorCode> = new Set([
  "ssh.runId-invalid",
  "ssh.comment-invalid",
  "ssh.path-escape",
  "ssh.key-already-exists",
]);

export interface ChildProcessRunner {
  (
    command: string,
    args: readonly string[],
    options?: { readonly cwd?: string; readonly env?: Readonly<Record<string, string>> },
  ): Promise<{ readonly code: number; readonly stdout: string; readonly stderr: string }>;
}

const RUN_ID_RE = /^[A-Za-z0-9-]{8,64}$/;
const COMMENT_RE = /^[A-Za-z0-9 _.@:+/-]{0,80}$/;
const KEY_NAME_RE = /^hoopoe-vps-[A-Za-z0-9-]{8,64}$/;

const DEFAULT_HOME = homedir();
const DEFAULT_BIN = "ssh-keygen";

/** Default child-process runner — `spawn` with explicit argv, no shell. */
export const defaultRunner: ChildProcessRunner = (command, args, options) =>
  new Promise((resolvePromise, rejectPromise) => {
    const child = spawn(command, args.slice(), {
      cwd: options?.cwd,
      env: options?.env ? { ...options.env } : process.env,
      stdio: ["ignore", "pipe", "pipe"],
      shell: false,
    });
    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (chunk: Buffer | string) => {
      stdout += chunk.toString("utf8");
    });
    child.stderr?.on("data", (chunk: Buffer | string) => {
      stderr += chunk.toString("utf8");
    });
    child.on("error", (err) => {
      rejectPromise(err);
    });
    child.on("close", (code) => {
      resolvePromise({ code: code ?? 0, stdout, stderr });
    });
  });

export class SshKeyService {
  readonly #home: string;
  readonly #sshDir: string;
  readonly #bin: string;
  readonly #run: ChildProcessRunner;
  readonly #audit: SshKeyAuditSink | undefined;
  readonly #now: () => Date;

  constructor(options: SshKeyServiceOptions = {}) {
    this.#home = options.homeDir ?? DEFAULT_HOME;
    this.#sshDir = join(this.#home, ".ssh");
    this.#bin = options.sshKeygenBin ?? DEFAULT_BIN;
    this.#run = options.run ?? defaultRunner;
    this.#audit = options.audit;
    this.#now = options.now ?? (() => new Date());
  }

  /** Resolved `~/.ssh/` directory, exposed for tests + diagnostics. */
  get sshDir(): string {
    return this.#sshDir;
  }

  async listKeys(): Promise<readonly ListedSshKey[]> {
    let entries: readonly string[];
    try {
      entries = await fs.readdir(this.#sshDir);
    } catch (err) {
      if (isErrnoCode(err, "ENOENT")) return [];
      throw new SshKeyServiceError(
        "ssh.dir-not-readable",
        `Cannot read ${this.#sshDir}: ${(err as Error).message}`,
        { sshDir: this.#sshDir },
      );
    }
    const pubFiles = entries
      .filter((entry) => entry.endsWith(".pub"))
      .toSorted();

    const results: ListedSshKey[] = [];
    for (const entry of pubFiles) {
      const fullPath = join(this.#sshDir, entry);
      const containment = ensureContainedIn(fullPath, this.#sshDir);
      if (!containment.contained) continue;
      try {
        const info = await this.#fingerprintFile(fullPath);
        const privatePath = fullPath.slice(0, -".pub".length);
        const hasPrivateKey = await fileExists(privatePath);
        results.push({
          name: entry,
          path: fullPath,
          algorithm: info.algorithm,
          fingerprint: info.fingerprint,
          comment: info.comment,
          bits: info.bits,
          hasPrivateKey,
        });
      } catch (err) {
        if (err instanceof SshKeyServiceError) {
          // Surface the bad entry but keep going; a corrupted file
          // shouldn't blank the list. Wizard renders unparseable keys
          // separately later.
          results.push({
            name: entry,
            path: fullPath,
            algorithm: "unknown",
            fingerprint: "",
            comment: err.message,
            bits: null,
            hasPrivateKey: false,
          });
          continue;
        }
        throw err;
      }
    }
    return results;
  }

  async generateKey(input: GenerateKeyInput): Promise<GeneratedSshKey> {
    const rawRunId = String(input.runId ?? "").trim();
    try {
      const generated = await this.#generateKeyImpl(rawRunId, input);
      this.#emitAudit({
        kind: "ssh.key_generation_succeeded",
        at: this.#now().toISOString(),
        runId: rawRunId,
        keyName: basename(generated.privatePath),
        algorithm: generated.algorithm,
        fingerprint: generated.fingerprint,
      });
      return generated;
    } catch (err) {
      if (err instanceof SshKeyServiceError) {
        this.#emitAudit({
          kind: REFUSED_CODES.has(err.code)
            ? "ssh.key_generation_refused"
            : "ssh.key_generation_failed",
          at: this.#now().toISOString(),
          runId: rawRunId,
          errorCode: err.code,
          errorMessage: err.message,
        });
      }
      throw err;
    }
  }

  /** Inner generateKey body — separated so the public method can wrap a
   *  single try/catch around the success and error audit emissions. */
  async #generateKeyImpl(runId: string, input: GenerateKeyInput): Promise<GeneratedSshKey> {
    if (!RUN_ID_RE.test(runId)) {
      throw new SshKeyServiceError(
        "ssh.runId-invalid",
        "runId must match /^[A-Za-z0-9-]{8,64}$/.",
        { runId },
      );
    }
    const comment =
      input.comment === undefined ? `hoopoe-vps-${runId}` : sanitizeComment(input.comment);
    if (comment === null) {
      throw new SshKeyServiceError(
        "ssh.comment-invalid",
        "comment must match /^[A-Za-z0-9 _.@:+/-]{0,80}$/.",
        { comment: String(input.comment) },
      );
    }

    await fs.mkdir(this.#sshDir, { recursive: true, mode: 0o700 });
    const keyName = `hoopoe-vps-${runId}`;
    if (!KEY_NAME_RE.test(keyName)) {
      // Defense-in-depth — runId already passed RUN_ID_RE.
      throw new SshKeyServiceError("ssh.runId-invalid", "derived keyName failed validation", {
        keyName,
      });
    }
    const privatePath = join(this.#sshDir, keyName);
    const publicPath = `${privatePath}.pub`;
    if (await fileExists(privatePath)) {
      throw new SshKeyServiceError(
        "ssh.key-already-exists",
        `A key for runId ${runId} already exists at ${privatePath}.`,
        { privatePath, runId },
      );
    }
    const containment = ensureContainedIn(privatePath, this.#sshDir);
    if (!containment.contained) {
      throw new SshKeyServiceError(
        "ssh.path-escape",
        `Derived key path ${privatePath} would escape ${this.#sshDir}.`,
        { privatePath, sshDir: this.#sshDir },
      );
    }

    const argv = [
      "-t",
      "ed25519",
      "-f",
      privatePath,
      "-N",
      "",
      "-C",
      comment,
    ] as const;

    let result: { code: number; stdout: string; stderr: string };
    try {
      result = await this.#run(this.#bin, argv);
    } catch (err) {
      throw new SshKeyServiceError(
        "ssh-keygen-not-installed",
        `Failed to run ${this.#bin}: ${(err as Error).message}`,
        { command: this.#bin },
      );
    }
    if (result.code !== 0) {
      throw new SshKeyServiceError(
        "ssh-keygen-exit",
        `ssh-keygen exited with code ${result.code}: ${result.stderr.trim() || result.stdout.trim()}`,
        { code: String(result.code), stderr: result.stderr.trim() },
      );
    }
    const fingerprint = await this.#fingerprintFile(publicPath);
    return {
      name: basename(publicPath),
      path: publicPath,
      privatePath,
      algorithm: fingerprint.algorithm,
      fingerprint: fingerprint.fingerprint,
      comment: fingerprint.comment,
      bits: fingerprint.bits,
      hasPrivateKey: true,
    };
  }

  #emitAudit(event: SshKeyAuditEvent): void {
    if (!this.#audit) return;
    try {
      this.#audit(event);
    } catch {
      // Defensive: a sink that throws cannot block key generation.
      // Production wiring uses a logger that doesn't throw.
    }
  }

  async #fingerprintFile(publicPath: string): Promise<{
    readonly algorithm: SshKeyAlgorithm | "unknown";
    readonly fingerprint: string;
    readonly comment: string;
    readonly bits: number | null;
  }> {
    const containment = ensureContainedIn(publicPath, this.#sshDir);
    if (!containment.contained) {
      throw new SshKeyServiceError("ssh.path-escape", "fingerprint requested for path outside ~/.ssh/", {
        publicPath,
      });
    }
    let result: { code: number; stdout: string; stderr: string };
    try {
      result = await this.#run(this.#bin, ["-l", "-f", publicPath]);
    } catch (err) {
      throw new SshKeyServiceError(
        "ssh-keygen-not-installed",
        `Failed to run ${this.#bin}: ${(err as Error).message}`,
        { command: this.#bin },
      );
    }
    if (result.code !== 0) {
      throw new SshKeyServiceError(
        "ssh-fingerprint-failed",
        `ssh-keygen -l failed for ${publicPath} (exit ${result.code}): ${result.stderr.trim()}`,
        { publicPath, code: String(result.code) },
      );
    }
    return parseSshKeygenLine(result.stdout);
  }
}

const SSH_KEYGEN_LINE_RE =
  /^(?<bits>\d+)\s+(?<fingerprint>SHA256:[^\s]+|MD5:[^\s]+)\s+(?<comment>.*?)\s+\((?<algorithm>[A-Z0-9-]+)\)\s*$/m;

export function parseSshKeygenLine(stdout: string): {
  readonly algorithm: SshKeyAlgorithm | "unknown";
  readonly fingerprint: string;
  readonly comment: string;
  readonly bits: number | null;
} {
  const trimmed = stdout.trim();
  if (!trimmed) {
    throw new SshKeyServiceError("ssh-keygen-no-fingerprint", "ssh-keygen returned no output");
  }
  const match = SSH_KEYGEN_LINE_RE.exec(trimmed);
  const groups = match?.groups;
  if (!match || !groups) {
    return {
      algorithm: "unknown",
      fingerprint: trimmed.split(/\s+/)[1] ?? trimmed,
      comment: trimmed,
      bits: null,
    };
  }
  const algorithmRaw = (groups.algorithm ?? "").toLowerCase();
  const algorithm: SshKeyAlgorithm | "unknown" = (SSH_KEY_ALGORITHMS as readonly string[]).includes(
    algorithmRaw,
  )
    ? (algorithmRaw as SshKeyAlgorithm)
    : "unknown";
  const bitsParsed = Number.parseInt(groups.bits ?? "", 10);
  return {
    algorithm,
    fingerprint: groups.fingerprint ?? trimmed,
    comment: (groups.comment ?? "").trim(),
    bits: Number.isFinite(bitsParsed) ? bitsParsed : null,
  };
}

function ensureContainedIn(target: string, parent: string): { readonly contained: boolean } {
  if (!isAbsolute(target) || !isAbsolute(parent)) return { contained: false };
  const targetDir = resolve(dirname(target));
  const resolvedParent = resolve(parent);
  const contained =
    targetDir === resolvedParent ||
    targetDir.startsWith(`${resolvedParent}${sep}`);
  return { contained };
}

async function fileExists(path: string): Promise<boolean> {
  try {
    await fs.access(path, fsConstants.F_OK);
    return true;
  } catch {
    return false;
  }
}

function sanitizeComment(input: string): string | null {
  const trimmed = input.trim();
  if (!COMMENT_RE.test(trimmed)) return null;
  return trimmed;
}

function isErrnoCode(err: unknown, code: string): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "code" in err &&
    (err as { readonly code: string }).code === code
  );
}
