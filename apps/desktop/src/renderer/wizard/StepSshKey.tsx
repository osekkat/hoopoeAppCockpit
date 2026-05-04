// hp-pl8h — SSH key wizard step.
//
// Replaces the StepStub for stepId="ssh_key". The component:
//   - Lists existing keys via window.hoopoe.ssh.listKeys() and lets the
//     user pick one to use for the VPS pairing.
//   - Generates a fresh ed25519 key via window.hoopoe.ssh.generateKey()
//     when the user clicks "Generate". The key file is named
//     `hoopoe-vps-<runId>` so the user can correlate it with this run.
//   - Reports success via onComplete(selection); the caller persists
//     the selection (label + fingerprint + path) as the step checkpoint
//     and advances the wizard.
//   - Reports a failure code via onFailed(code, message) so the
//     resume banner can render a stable Resume CTA.

import { Check, KeyRound, RefreshCcw, Sparkles } from "lucide-react";
import { useCallback, useEffect, useState } from "react";

export interface SshKeySelection {
  readonly label: string;
  readonly path: string;
  readonly fingerprint: string;
  readonly algorithm: string;
}

export interface ListedSshKeyView {
  readonly name: string;
  readonly path: string;
  readonly algorithm: string;
  readonly fingerprint: string;
  readonly comment: string;
  readonly bits: number | null;
  readonly hasPrivateKey: boolean;
}

interface SshBridge {
  readonly listKeys: () => Promise<{ readonly keys: readonly ListedSshKeyView[] }>;
  readonly generateKey: (input: {
    readonly runId: string;
    readonly comment?: string;
  }) => Promise<ListedSshKeyView & { readonly privatePath: string }>;
}

interface StepSshKeyProps {
  readonly runId: string;
  readonly onComplete: (selection: SshKeySelection) => void;
  readonly onFailed: (failure: { readonly code: string; readonly message: string }) => void;
  readonly bridge?: SshBridge;
  /** Optional initial selection (resume from prior checkpoint). */
  readonly initialSelection?: SshKeySelection | undefined;
}

type LoadState =
  | { readonly status: "idle" }
  | { readonly status: "loading" }
  | { readonly status: "ready"; readonly keys: readonly ListedSshKeyView[] }
  | { readonly status: "error"; readonly message: string };

type ActionState =
  | { readonly kind: "idle" }
  | { readonly kind: "generating" }
  | { readonly kind: "saved"; readonly selection: SshKeySelection };

function getDefaultBridge(): SshBridge | null {
  if (typeof window === "undefined") return null;
  const ssh = (window as { readonly hoopoe?: { readonly ssh?: unknown } }).hoopoe?.ssh;
  if (!ssh || typeof ssh !== "object") return null;
  return ssh as unknown as SshBridge;
}

export function StepSshKey({ runId, onComplete, onFailed, bridge, initialSelection }: StepSshKeyProps) {
  const resolvedBridge = bridge ?? getDefaultBridge();
  const [load, setLoad] = useState<LoadState>({ status: "idle" });
  const [action, setAction] = useState<ActionState>(
    initialSelection ? { kind: "saved", selection: initialSelection } : { kind: "idle" },
  );
  const [selectedPath, setSelectedPath] = useState<string | null>(initialSelection?.path ?? null);

  const refresh = useCallback(async () => {
    if (!resolvedBridge) {
      setLoad({ status: "error", message: "Hoopoe bridge unavailable. Reload the app." });
      return;
    }
    setLoad({ status: "loading" });
    try {
      const response = await resolvedBridge.listKeys();
      const keys = response?.keys ?? [];
      setLoad({ status: "ready", keys });
      if (selectedPath && !keys.some((key) => key.path === selectedPath)) {
        setSelectedPath(null);
      }
    } catch (err) {
      setLoad({ status: "error", message: (err as Error).message });
    }
  }, [resolvedBridge, selectedPath]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleGenerate = async () => {
    if (!resolvedBridge) return;
    setAction({ kind: "generating" });
    try {
      const generated = await resolvedBridge.generateKey({ runId, comment: `hoopoe-vps-${runId}` });
      const selection: SshKeySelection = {
        label: generated.name,
        path: generated.path,
        fingerprint: generated.fingerprint,
        algorithm: generated.algorithm,
      };
      setAction({ kind: "saved", selection });
      setSelectedPath(generated.path);
      await refresh();
      onComplete(selection);
    } catch (err) {
      const code = (err as { readonly code?: string }).code ?? "ssh.generate-failed";
      const message = (err as Error).message ?? "Key generation failed.";
      setAction({ kind: "idle" });
      onFailed({ code, message });
    }
  };

  const handleConfirmExisting = (key: ListedSshKeyView) => {
    if (!key.hasPrivateKey) {
      onFailed({
        code: "ssh.public-only",
        message: `${key.name} has no matching private key. Generate a fresh key or import the matching private key first.`,
      });
      return;
    }
    const selection: SshKeySelection = {
      label: key.name,
      path: key.path,
      fingerprint: key.fingerprint,
      algorithm: key.algorithm,
    };
    setAction({ kind: "saved", selection });
    setSelectedPath(key.path);
    onComplete(selection);
  };

  return (
    <section
      aria-labelledby="hh-wizard-ssh-key-title"
      className="hh-wizard-step hh-wizard-step-ssh-key"
      data-testid="wizard-step-ssh_key"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 02</span>
        <h2 id="hh-wizard-ssh-key-title">Generate or pick an SSH key</h2>
        <p>
          Hoopoe uses your SSH key to open the tunnel to the VPS. Either pick a key Hoopoe found in
          <code> ~/.ssh/ </code>or generate a fresh one named <code>hoopoe-vps-{runId.slice(0, 8)}…</code>.
        </p>
      </header>

      <div className="hh-wizard-ssh-key-actions">
        <button
          type="button"
          className="hh-wizard-ssh-key-generate"
          onClick={() => void handleGenerate()}
          disabled={!resolvedBridge || action.kind === "generating"}
          data-testid="wizard-ssh-generate"
        >
          <Sparkles size={13} strokeWidth={2.1} />
          <span>{action.kind === "generating" ? "Generating…" : "Generate fresh ed25519 key"}</span>
        </button>
        <button
          type="button"
          className="hh-wizard-ssh-key-refresh"
          onClick={() => void refresh()}
          disabled={load.status === "loading"}
          data-testid="wizard-ssh-refresh"
        >
          <RefreshCcw size={12} strokeWidth={2.1} />
          <span>Refresh</span>
        </button>
      </div>

      {load.status === "loading" ? (
        <p className="hh-wizard-ssh-key-status" role="status">
          Reading <code>~/.ssh/</code>…
        </p>
      ) : null}
      {load.status === "error" ? (
        <p className="hh-wizard-ssh-key-error" role="alert" data-testid="wizard-ssh-error">
          Could not read your SSH directory: {load.message}
        </p>
      ) : null}

      {load.status === "ready" ? (
        <div className="hh-wizard-ssh-key-list">
          {load.keys.length === 0 ? (
            <p className="hh-wizard-ssh-key-empty" data-testid="wizard-ssh-empty">
              No keys found in <code>~/.ssh/</code>. Click <strong>Generate</strong> to create one.
            </p>
          ) : (
            <ul>
              {load.keys.map((key) => {
                const selected = selectedPath === key.path;
                return (
                  <li key={key.path}>
                    <button
                      type="button"
                      className={`hh-wizard-ssh-key-row${selected ? " hh-wizard-ssh-key-row-selected" : ""}`}
                      onClick={() => handleConfirmExisting(key)}
                      data-testid={`wizard-ssh-row-${key.name}`}
                      aria-current={selected ? "true" : undefined}
                    >
                      <span className="hh-wizard-ssh-key-row-icon" aria-hidden="true">
                        <KeyRound size={14} strokeWidth={2.1} />
                      </span>
                      <span className="hh-wizard-ssh-key-row-main">
                        <strong>{key.name}</strong>
                        <span>
                          {key.algorithm}
                          {key.bits ? ` · ${key.bits}` : ""}
                          {key.comment ? ` · ${key.comment}` : ""}
                        </span>
                      </span>
                      <span className="hh-wizard-ssh-key-row-meta">
                        {key.fingerprint || "no fingerprint"}
                      </span>
                      {selected ? (
                        <span className="hh-wizard-ssh-key-row-saved" aria-label="Selected">
                          <Check size={12} strokeWidth={2.1} />
                        </span>
                      ) : null}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      ) : null}

      {action.kind === "saved" ? (
        <p className="hh-wizard-ssh-key-saved" role="status" data-testid="wizard-ssh-saved">
          <Check size={12} strokeWidth={2.1} />
          <span>
            Using <strong>{action.selection.label}</strong> ({action.selection.fingerprint || "no fingerprint"}).
          </span>
        </p>
      ) : null}
    </section>
  );
}
