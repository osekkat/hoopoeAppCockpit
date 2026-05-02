// Originally Hoopoe-owned shim — NOT vendored from t3code.
//
// This file exists to make t3code's lifted lifecycle source files compile in
// the Hoopoe tree without dragging in the Effect framework or t3code's
// `@t3tools/contracts` / `@t3tools/shared` packages (Appendix B "Files we
// explicitly do NOT lift"). Each lifted file gets its `@t3tools/...` import
// paths mechanically rewritten to `./_shims.ts`; the symbols below mirror what
// the lifted code reads from those packages.
//
// Adaptations (real validation, real net-bind probing, real login-shell env
// scraping) live in `apps/desktop/src/main/*` and replace the stubs here.
// Anything implemented as a `throw` here is a deliberate "wire up real impl
// in hp-zir" marker.

import * as Net from "node:net";

// ── Type shims (mirror @t3tools/contracts symbols actually used) ────────────

export type DesktopRuntimeArch = "arm64" | "x64" | "other";
export type DesktopUpdateChannel = "latest" | "nightly";
export type DesktopServerExposureMode = "local-only" | "network-accessible";
export type DesktopAppStageLabel = "Alpha" | "Dev" | "Nightly";
export type DesktopUpdateStatus =
  | "disabled"
  | "idle"
  | "checking"
  | "up-to-date"
  | "available"
  | "downloading"
  | "downloaded"
  | "error";

export interface DesktopAppBranding {
  baseName: string;
  stageLabel: DesktopAppStageLabel;
  displayName: string;
}

export interface DesktopRuntimeInfo {
  hostArch: DesktopRuntimeArch;
  appArch: DesktopRuntimeArch;
  runningUnderArm64Translation: boolean;
}

export interface DesktopUpdateState {
  enabled: boolean;
  status: DesktopUpdateStatus;
  channel: DesktopUpdateChannel;
  currentVersion: string;
  hostArch: DesktopRuntimeArch;
  appArch: DesktopRuntimeArch;
  runningUnderArm64Translation: boolean;
  availableVersion: string | null;
  downloadedVersion: string | null;
  downloadPercent: number | null;
  checkedAt: string | null;
  message: string | null;
  errorContext: "check" | "download" | "install" | null;
  canRetry: boolean;
}

export type EnvironmentId = string;

export interface PersistedSavedEnvironmentRecord {
  readonly environmentId: EnvironmentId;
  readonly label: string;
  readonly wsBaseUrl: string;
  readonly httpBaseUrl: string;
  readonly createdAt: string;
  readonly lastConnectedAt: string | null;
}

// ClientSettings is a structural placeholder. The full t3code shape is an
// Effect-`Schema.Struct` with ~30 fields driving t3code's chat UI; Hoopoe's
// renderer is purpose-built and will declare its own client settings shape in
// `@hoopoe/schemas` (Phase 2.5). For now this is permissive so the lifted
// `clientPersistence.ts` round-trips arbitrary JSON without losing fields.
export interface ClientSettings {
  readonly [key: string]: unknown;
}

// Single-method "schema" interface that mirrors the shape of an Effect
// `Schema.Struct` for the lifted code path that calls `Schema.decodeUnknownSync`.
export interface VendoredSchema<T> {
  readonly decodeUnknownSync: (value: unknown) => T;
}

// Phase 2.5 / hp-r3i replaces this with a Zod / OpenAPI-derived validator.
// For now we accept any object as ClientSettings (the t3code Effect schema
// enforced defaults; Hoopoe will enforce them at the Settings schema layer).
export const ClientSettingsSchema: VendoredSchema<ClientSettings> = {
  decodeUnknownSync(value: unknown): ClientSettings {
    if (value === null || typeof value !== "object") {
      throw new Error("Invalid client settings: expected object.");
    }
    return value as ClientSettings;
  },
};

// ── Effect Predicate.isObject / Schema.decodeUnknownSync replacements ───────

export const Predicate = {
  isObject(value: unknown): value is Record<string, unknown> {
    return typeof value === "object" && value !== null && !Array.isArray(value);
  },
};

export const Schema = {
  decodeUnknownSync<T>(schema: VendoredSchema<T>): (value: unknown) => T {
    return (value: unknown): T => schema.decodeUnknownSync(value);
  },
};

// ── NetService.canListenOnHost replacement (uses Node's net module) ─────────

export async function canListenOnHost(port: number, host: string): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    const server = Net.createServer();
    let resolved = false;
    const finish = (ok: boolean) => {
      if (resolved) return;
      resolved = true;
      server.close(() => {
        resolve(ok);
      });
    };
    server.once("error", () => {
      finish(false);
    });
    server.once("listening", () => {
      finish(true);
    });
    try {
      server.listen({ port, host, exclusive: true });
    } catch {
      finish(false);
    }
  });
}

// ── @t3tools/shared/shell stubs ─────────────────────────────────────────────
//
// Real shell-env scraping logic ports to `apps/desktop/src/main/SettingsBridge.ts`
// or a dedicated `apps/desktop/src/main/shellEnv.ts` in hp-zir. The stubs here
// exist so `syncShellEnvironment.ts` can be lifted and typecheck without
// dragging in t3code's `packages/shared/`.

export interface CommandAvailabilityOptions {
  readonly env?: NodeJS.ProcessEnv;
}

export type ShellEnvironmentReader = (
  shell: string,
  names: readonly string[],
) => Partial<Record<string, string>>;

export type WindowsShellEnvironmentReader = (
  options?: { readonly env?: NodeJS.ProcessEnv },
) => Partial<Record<string, string>>;

export function listLoginShellCandidates(
  _platform: NodeJS.Platform,
  _envShell?: string,
  _userShell?: string,
): readonly string[] {
  // hp-zir wires up the real candidate list (zsh/bash/fish + platform shims).
  return [];
}

export function mergePathEntries(
  shellPath: string | undefined,
  envPath: string | undefined,
  _platform: NodeJS.Platform,
): string | undefined {
  if (!shellPath) return envPath;
  if (!envPath) return shellPath;
  return shellPath === envPath ? envPath : `${shellPath}:${envPath}`;
}

export function readPathFromLaunchctl(): string | undefined {
  return undefined;
}

export function readEnvironmentFromLoginShell(
  _shell: string,
  _names: readonly string[],
): Partial<Record<string, string>> {
  return {};
}

export function resolveWindowsEnvironment(
  env: NodeJS.ProcessEnv,
  _options?: {
    readonly readEnvironment?: WindowsShellEnvironmentReader;
    readonly commandAvailable?: (
      command: string,
      options?: CommandAvailabilityOptions,
    ) => boolean;
  },
): NodeJS.ProcessEnv {
  return env;
}
