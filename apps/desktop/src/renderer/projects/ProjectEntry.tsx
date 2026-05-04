// hp-ilt — Phase 4 Project Entry UI.
//
// Three-mode entry view: Import existing repo / Create new project / Clone
// from URL. Each mode is a controlled form that calls the matching daemon
// RPC method via the hooks in `./data.ts`. Successful import additionally
// shows the §4.2 readiness gate evaluation in a side panel.
//
// This component is the single user-facing surface for the bead's
// "PROJECT IMPORT (§7.1 'Project entry')" requirement. The picker route
// (`shell/routes.tsx#ProjectPickerRoute`) toggles it open via the "Add
// project" affordance.

import { useState } from "react";
import {
  AlertCircle,
  CheckCircle2,
  CircleDashed,
  Download,
  FolderInput,
  GitBranch,
  Loader2,
} from "lucide-react";
import {
  ProjectsBridgeUnavailableError,
  useCloneProjectMutation,
  useCreateProjectMutation,
  useImportProjectMutation,
  useReadinessQuery,
  validateCloneInput,
  validateCreateInput,
  validateImportInput,
  type ProjectCloneInput,
  type ProjectCreateInput,
  type ProjectImportInput,
  type ReadinessOutput,
} from "./data.ts";
import { StateSurface } from "../state-view/index.ts";
import "./ProjectEntry.css";

export type ProjectEntryMode = "import" | "create" | "clone";

export interface ProjectEntryProps {
  /** Initial mode. Default: "import". */
  readonly initialMode?: ProjectEntryMode;
  /** Called after a successful import/create/clone with the new project id.
   *  The picker route uses this to switch to the new project. */
  readonly onProjectReady?: (input: { projectId: string; rootPath: string }) => void;
}

const TAB_DEFINITIONS: ReadonlyArray<{
  readonly id: ProjectEntryMode;
  readonly label: string;
  readonly description: string;
  readonly icon: typeof FolderInput;
}> = [
  {
    id: "import",
    label: "Import existing repo",
    description: "Point Hoopoe at a checkout that already lives on your VPS.",
    icon: FolderInput,
  },
  {
    id: "create",
    label: "Create new project",
    description: "Initialize a fresh repo on the VPS with a remote of your choice.",
    icon: GitBranch,
  },
  {
    id: "clone",
    label: "Clone from URL",
    description: "Clone a remote repository onto the VPS at /data/projects.",
    icon: Download,
  },
];

export function ProjectEntry({ initialMode = "import", onProjectReady }: ProjectEntryProps) {
  const [mode, setMode] = useState<ProjectEntryMode>(initialMode);

  return (
    <section
      aria-labelledby="hh-project-entry-title"
      className="hh-project-entry"
      data-testid="project-entry"
    >
      <header className="hh-project-entry-header">
        <span className="hh-stage-kicker">PROJECT ENTRY</span>
        <h2 id="hh-project-entry-title">Add a project to Hoopoe</h2>
        <p>
          Hoopoe wraps your existing toolchain. The project lives on the VPS at
          <code> /data/projects/&lt;slug&gt;</code>; an external Git remote is required per
          plan.md §1.1.
        </p>
      </header>

      <nav aria-label="Project entry mode" className="hh-project-entry-tabs" role="tablist">
        {TAB_DEFINITIONS.map((tab) => {
          const Icon = tab.icon;
          const selected = tab.id === mode;
          return (
            <button
              aria-selected={selected}
              className="hh-project-entry-tab"
              data-selected={selected}
              data-testid={`project-entry-tab-${tab.id}`}
              key={tab.id}
              onClick={() => setMode(tab.id)}
              role="tab"
              type="button"
            >
              <Icon size={16} strokeWidth={2.1} />
              <span>{tab.label}</span>
            </button>
          );
        })}
      </nav>

      <p className="hh-project-entry-help" data-testid={`project-entry-help-${mode}`}>
        {TAB_DEFINITIONS.find((t) => t.id === mode)?.description}
      </p>

      <div className="hh-project-entry-body" data-mode={mode} role="tabpanel">
        {mode === "import" ? <ImportForm onProjectReady={onProjectReady} /> : null}
        {mode === "create" ? <CreateForm onProjectReady={onProjectReady} /> : null}
        {mode === "clone" ? <CloneForm onProjectReady={onProjectReady} /> : null}
      </div>
    </section>
  );
}

// ── Import form ───────────────────────────────────────────────────────────

function ImportForm({ onProjectReady }: { onProjectReady?: ProjectEntryProps["onProjectReady"] }) {
  const [rootPath, setRootPath] = useState("");
  const [name, setName] = useState("");
  const mutation = useImportProjectMutation();
  const errors = mutation.isPending
    ? {}
    : validateImportInput({ rootPath, ...(name.trim() ? { name } : {}) });
  const trimmedRootPath = rootPath.trim();
  const readiness = useReadinessQuery(
    { rootPath: trimmedRootPath },
    trimmedRootPath.length > 0 && !mutation.isPending && !mutation.isSuccess,
  );

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (Object.keys(errors).length > 0) return;
    const input: ProjectImportInput = name.trim().length > 0
      ? { rootPath: trimmedRootPath, name: name.trim() }
      : { rootPath: trimmedRootPath };
    mutation.mutate(input, {
      onSuccess: (output) => {
        onProjectReady?.({ projectId: output.projectId, rootPath: output.rootPath });
      },
    });
  }

  return (
    <form className="hh-project-entry-form" data-testid="project-entry-import-form" onSubmit={handleSubmit}>
      <Field
        {...(errors.rootPath !== undefined ? { error: errors.rootPath } : {})}
        hint="Absolute VPS path to an existing git checkout (e.g. /data/projects/my-repo)."
        id="hh-import-rootPath"
        label="VPS path"
        onChange={setRootPath}
        placeholder="/data/projects/my-repo"
        value={rootPath}
      />
      <Field
        hint="Optional override; defaults to the directory basename."
        id="hh-import-name"
        label="Project name"
        onChange={setName}
        placeholder=""
        value={name}
      />
      <SubmitRow
        busy={mutation.isPending}
        disabled={Object.keys(errors).length > 0 || trimmedRootPath.length === 0}
        label="Import project"
      />
      <MutationFeedback mutation={mutation} />
      {trimmedRootPath.length > 0 ? (
        <ReadinessPanel
          data={readiness.data}
          error={readiness.error}
          isFetching={readiness.isFetching}
        />
      ) : null}
    </form>
  );
}

// ── Create form ───────────────────────────────────────────────────────────

function CreateForm({ onProjectReady }: { onProjectReady?: ProjectEntryProps["onProjectReady"] }) {
  const [name, setName] = useState("");
  const [originRemote, setOriginRemote] = useState("");
  const [slug, setSlug] = useState("");
  const mutation = useCreateProjectMutation();
  const errors = mutation.isPending
    ? {}
    : validateCreateInput({ name, originRemote, ...(slug.trim() ? { slug } : {}) });

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (Object.keys(errors).length > 0) return;
    const input: ProjectCreateInput = {
      name: name.trim(),
      originRemote: originRemote.trim(),
      ...(slug.trim() ? { slug: slug.trim() } : {}),
    };
    mutation.mutate(input, {
      onSuccess: (output) => {
        onProjectReady?.({ projectId: output.projectId, rootPath: output.rootPath });
      },
    });
  }

  return (
    <form className="hh-project-entry-form" data-testid="project-entry-create-form" onSubmit={handleSubmit}>
      <Field
        {...(errors.name !== undefined ? { error: errors.name } : {})}
        hint="Human-readable label for the project."
        id="hh-create-name"
        label="Project name"
        onChange={setName}
        placeholder="My new project"
        value={name}
      />
      <Field
        {...(errors.originRemote !== undefined ? { error: errors.originRemote } : {})}
        hint="External Git remote URL (required by plan.md §1.1)."
        id="hh-create-origin"
        label="Origin remote"
        onChange={setOriginRemote}
        placeholder="git@github.com:org/repo.git"
        value={originRemote}
      />
      <Field
        hint="Optional slug; defaults to a slugified name."
        id="hh-create-slug"
        label="Slug"
        onChange={setSlug}
        placeholder=""
        value={slug}
      />
      <SubmitRow
        busy={mutation.isPending}
        disabled={Object.keys(errors).length > 0 || name.trim().length === 0 || originRemote.trim().length === 0}
        label="Create project"
      />
      <MutationFeedback mutation={mutation} />
    </form>
  );
}

// ── Clone form ────────────────────────────────────────────────────────────

function CloneForm({ onProjectReady }: { onProjectReady?: ProjectEntryProps["onProjectReady"] }) {
  const [remoteUrl, setRemoteUrl] = useState("");
  const [name, setName] = useState("");
  const [targetParentDir, setTargetParentDir] = useState("");
  const mutation = useCloneProjectMutation();
  const errors = mutation.isPending
    ? {}
    : validateCloneInput({
        remoteUrl,
        ...(name.trim() ? { name } : {}),
        ...(targetParentDir.trim() ? { targetParentDir } : {}),
      });

  function handleSubmit(event: React.FormEvent) {
    event.preventDefault();
    if (Object.keys(errors).length > 0) return;
    const input: ProjectCloneInput = {
      remoteUrl: remoteUrl.trim(),
      ...(name.trim() ? { name: name.trim() } : {}),
      ...(targetParentDir.trim() ? { targetParentDir: targetParentDir.trim() } : {}),
    };
    mutation.mutate(input, {
      onSuccess: (output) => {
        onProjectReady?.({ projectId: output.projectId, rootPath: output.rootPath });
      },
    });
  }

  return (
    <form className="hh-project-entry-form" data-testid="project-entry-clone-form" onSubmit={handleSubmit}>
      <Field
        {...(errors.remoteUrl !== undefined ? { error: errors.remoteUrl } : {})}
        hint="HTTPS or SSH remote (e.g. git@github.com:org/repo.git)."
        id="hh-clone-remote"
        label="Remote URL"
        onChange={setRemoteUrl}
        placeholder="https://github.com/org/repo.git"
        value={remoteUrl}
      />
      <Field
        hint="Optional override; defaults to the repo basename."
        id="hh-clone-name"
        label="Project name"
        onChange={setName}
        placeholder=""
        value={name}
      />
      <Field
        hint="Parent directory on the VPS (defaults to /data/projects)."
        id="hh-clone-parent"
        label="Target parent directory"
        onChange={setTargetParentDir}
        placeholder="/data/projects"
        value={targetParentDir}
      />
      <SubmitRow
        busy={mutation.isPending}
        disabled={Object.keys(errors).length > 0 || remoteUrl.trim().length === 0}
        label="Clone project"
      />
      <MutationFeedback mutation={mutation} />
    </form>
  );
}

// ── Shared bits ───────────────────────────────────────────────────────────

interface FieldProps {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly placeholder: string;
  readonly hint: string;
  readonly error?: string;
  readonly onChange: (next: string) => void;
}

function Field({ error, hint, id, label, onChange, placeholder, value }: FieldProps) {
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  return (
    <div className="hh-project-entry-field" data-error={error !== undefined}>
      <label htmlFor={id}>{label}</label>
      <input
        aria-describedby={`${hintId}${error !== undefined ? ` ${errorId}` : ""}`}
        aria-invalid={error !== undefined}
        id={id}
        name={id}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        type="text"
        value={value}
      />
      <small className="hh-project-entry-hint" id={hintId}>{hint}</small>
      {error !== undefined ? (
        <small className="hh-project-entry-error" id={errorId} role="alert">
          {error}
        </small>
      ) : null}
    </div>
  );
}

function SubmitRow({ busy, disabled, label }: { readonly busy: boolean; readonly disabled: boolean; readonly label: string }) {
  return (
    <div className="hh-project-entry-actions">
      <button
        className="hh-project-entry-submit"
        data-busy={busy}
        disabled={disabled || busy}
        type="submit"
      >
        {busy ? <Loader2 size={15} strokeWidth={2.1} className="hh-spin" /> : null}
        <span>{busy ? "Working..." : label}</span>
      </button>
    </div>
  );
}

interface MinimalMutationStatus {
  readonly isError: boolean;
  readonly isSuccess: boolean;
  readonly error: Error | null;
}

function MutationFeedback({ mutation }: { readonly mutation: MinimalMutationStatus }) {
  if (mutation.isError) {
    const isBridge = mutation.error instanceof ProjectsBridgeUnavailableError;
    return (
      <div
        className="hh-project-entry-banner hh-project-entry-banner-error"
        data-testid="project-entry-error"
        role="alert"
      >
        <AlertCircle size={16} strokeWidth={2.1} />
        <div>
          <strong>{isBridge ? "Daemon not paired" : "Project lifecycle failed"}</strong>
          <p>{mutation.error?.message ?? "Unknown error"}</p>
        </div>
      </div>
    );
  }
  if (mutation.isSuccess) {
    return (
      <div
        className="hh-project-entry-banner hh-project-entry-banner-success"
        data-testid="project-entry-success"
        role="status"
      >
        <CheckCircle2 size={16} strokeWidth={2.1} />
        <span>Project ready.</span>
      </div>
    );
  }
  return null;
}

interface ReadinessPanelProps {
  readonly data: ReadinessOutput | undefined;
  readonly error: Error | null;
  readonly isFetching: boolean;
}

export function ReadinessPanel({ data, error, isFetching }: ReadinessPanelProps) {
  if (error instanceof ProjectsBridgeUnavailableError) {
    return null; // Suppressed; the mutation banner already explains.
  }
  if (error) {
    return (
      <StateSurface
        variant="error"
        density="compact"
        icon={<AlertCircle size={18} strokeWidth={2.1} />}
        title="Readiness probe failed"
        description={error.message}
        details={["Check the daemon connection before importing this project."]}
        testId="readiness-panel"
      />
    );
  }
  if (isFetching && !data) {
    return (
      <StateSurface
        variant="loading"
        density="compact"
        title="Probing readiness"
        description="Checking repository, origin remote, AGENTS.md, and Flywheel tool visibility."
        details={["The import gate is advisory until canonical daemon state returns."]}
        testId="readiness-panel"
      />
    );
  }
  if (!data) return null;
  return (
    <aside
      aria-labelledby="hh-readiness-title"
      className="hh-readiness-panel"
      data-testid="readiness-panel"
    >
      <h3 id="hh-readiness-title">
        {data.satisfied ? "Ready to import" : "Missing preconditions"}
      </h3>
      <ul>
        {data.requirements.map((req) => (
          <li
            className="hh-readiness-requirement"
            data-satisfied={req.satisfied}
            data-testid={`readiness-${req.id}`}
            key={req.id}
          >
            {req.satisfied ? (
              <CheckCircle2 size={14} strokeWidth={2.1} aria-hidden="true" />
            ) : (
              <CircleDashed size={14} strokeWidth={2.1} aria-hidden="true" />
            )}
            <div>
              <strong>{req.label}</strong>
              {req.note !== undefined ? <p>{req.note}</p> : null}
            </div>
          </li>
        ))}
      </ul>
    </aside>
  );
}
