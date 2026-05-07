// hp-ilt — Phase 4 renderer data layer for project lifecycle.
//
// The renderer drives Import / Create / Clone flows through the daemon-RPC
// bridge exposed by the preload (`window.hoopoe.daemon.request(method, body)`).
// The four lifecycle methods are listed in
// `apps/desktop/src/shared/ipc-contract.ts` (and authoritatively in
// `packages/schemas/preload-api.yaml`):
//
//   - "projects.create"   — new project from scratch
//   - "projects.import"   — import an existing checkout at a VPS path
//   - "projects.clone"    — clone a remote URL onto the VPS
//   - "projects.readiness"— evaluate the §4.2 'imported' gate (read-only)
//
// Daemon-side persistence (SQLite registry, POST /v1/projects, idempotency)
// is the daemon-pane's hp-ilt half. The renderer is bridge-agnostic — when
// the daemon RPC isn't wired (no bridge), the hooks surface a typed error
// rather than fall back silently. Mock-fixture flows happen one layer up
// in the test harness.
//
// Cross-references:
//   - apps/desktop/electron/projects/lifecycle.ts — main/daemon-side helpers.
//   - packages/schemas/preload-api.yaml — IPC method shapes.
//   - bead hp-ilt — Phase 4 project entry.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@hoopoe/schemas";

// ── Wire-shape inputs/outputs ─────────────────────────────────────────────
//
// hp-da2: previously these were hand-mirrored interfaces that could silently
// drift from preload-api.yaml. They are now type aliases over the generated
// OpenAPI components — preload-api.yaml `$ref`s the same components and the
// schemas codegen drift gate (validate-preload-codegen) refuses divergence.
// The renderer's lifecycle calls are end-to-end typed against the YAML.

export type ProjectCreateInput = components["schemas"]["ProjectsCreateInput"];
export type ProjectCreateOutput = components["schemas"]["ProjectsCreateOutput"];
export type ProjectImportInput = components["schemas"]["ProjectsImportInput"];
export type ProjectImportOutput = components["schemas"]["ProjectsImportOutput"];
export type ProjectCloneInput = components["schemas"]["ProjectsCloneInput"];
export type ProjectCloneOutput = components["schemas"]["ProjectsCloneOutput"];
export type ReadinessRequirement = components["schemas"]["ProjectsReadinessRequirement"];
export type ReadinessInput = components["schemas"]["ProjectsReadinessInput"];
export type ReadinessOutput = components["schemas"]["ProjectsReadinessOutput"];

// ── Daemon RPC bridge resolution ──────────────────────────────────────────

interface RendererDaemonBridge {
  readonly daemon?: {
    readonly request?: (method: string, body: unknown) => Promise<unknown>;
  };
}

export class ProjectsBridgeUnavailableError extends Error {
  override readonly name = "ProjectsBridgeUnavailableError";
  constructor() {
    super(
      "Hoopoe daemon RPC bridge is not available — pair a VPS daemon before importing projects.",
    );
  }
}

/** Resolve the renderer-side daemon-request function. Returns null when the
 *  preload bridge is not present (e.g., SSR, jsdom). Test harnesses can
 *  inject `window.hoopoe.daemon.request` to drive the hooks. */
export function resolveDaemonRequest(): ((method: string, body: unknown) => Promise<unknown>) | null {
  if (typeof window === "undefined") return null;
  const hoopoe = (window as Window & { readonly hoopoe?: RendererDaemonBridge }).hoopoe;
  const request = hoopoe?.daemon?.request;
  return typeof request === "function" ? request : null;
}

async function callDaemon<I, O>(method: string, body: I): Promise<O> {
  const request = resolveDaemonRequest();
  if (!request) throw new ProjectsBridgeUnavailableError();
  return (await request(method, body)) as O;
}

// ── Validation helpers (UI-side, mirrors §1.1 + lifecycle.ts errors) ──────

export interface CreateValidation {
  readonly name?: string;
  readonly originRemote?: string;
}

export function validateCreateInput(input: ProjectCreateInput): CreateValidation {
  const errors: { name?: string; originRemote?: string } = {};
  if (input.name.trim().length === 0) {
    errors.name = "name is required";
  }
  if (input.originRemote.trim().length === 0) {
    errors.originRemote = "origin remote is required (plan.md §1.1 — v1 has no remoteless mode)";
  } else if (!isPlausibleRemote(input.originRemote.trim())) {
    errors.originRemote = "expected a git remote URL or scp-style host:path";
  }
  return errors;
}

export interface ImportValidation {
  readonly rootPath?: string;
}

export function validateImportInput(input: ProjectImportInput): ImportValidation {
  const errors: { rootPath?: string } = {};
  if (input.rootPath.trim().length === 0) {
    errors.rootPath = "absolute VPS path is required";
  } else if (!isAbsolutePath(input.rootPath.trim())) {
    errors.rootPath = "path must be absolute (start with `/`)";
  }
  return errors;
}

export interface CloneValidation {
  readonly remoteUrl?: string;
}

export function validateCloneInput(input: ProjectCloneInput): CloneValidation {
  const errors: { remoteUrl?: string } = {};
  if (input.remoteUrl.trim().length === 0) {
    errors.remoteUrl = "remote URL is required";
  } else if (!isPlausibleRemote(input.remoteUrl.trim())) {
    errors.remoteUrl = "expected a git remote URL or scp-style host:path";
  }
  return errors;
}

function isAbsolutePath(value: string): boolean {
  return value.startsWith("/");
}

function isPlausibleRemote(value: string): boolean {
  // Accepts: https://host/repo[.git], git://host/repo, ssh://user@host/repo,
  // user@host:org/repo (scp-style). Refuses obvious garbage like spaces.
  if (/\s/.test(value)) return false;
  if (/^(https?|git|ssh):\/\//.test(value)) return true;
  if (/^[^:@\/]+@[^:]+:.+/.test(value)) return true;
  return false;
}

// ── Mutations ─────────────────────────────────────────────────────────────

export function useCreateProjectMutation() {
  const qc = useQueryClient();
  return useMutation<ProjectCreateOutput, Error, ProjectCreateInput>({
    mutationFn: (input) => callDaemon("projects.create", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["projects", "list"] });
    },
  });
}

export function useImportProjectMutation() {
  const qc = useQueryClient();
  return useMutation<ProjectImportOutput, Error, ProjectImportInput>({
    mutationFn: (input) => callDaemon("projects.import", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["projects", "list"] });
    },
  });
}

export function useCloneProjectMutation() {
  const qc = useQueryClient();
  return useMutation<ProjectCloneOutput, Error, ProjectCloneInput>({
    mutationFn: (input) => callDaemon("projects.clone", input),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["projects", "list"] });
    },
  });
}

/** Read-only readiness probe. Disabled when `rootPath` is empty so the form
 *  doesn't ping the daemon on every keystroke before the user has typed
 *  anything plausible. */
export function useReadinessQuery(input: ReadinessInput, enabled: boolean) {
  return useQuery<ReadinessOutput, Error>({
    queryKey: ["projects", "readiness", input.rootPath, input.allowNoLanguageManifest ?? false],
    queryFn: () => callDaemon("projects.readiness", input),
    enabled: enabled && input.rootPath.trim().length > 0,
    staleTime: 0,
  });
}
