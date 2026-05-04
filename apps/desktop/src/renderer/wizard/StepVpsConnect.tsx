// hp-o7rn — Wizard step 4: VPS connect.
//
// Replaces the StepStub for stepId="vps_connect". The user supplies the
// SSH connection details + picks the key from the prior ssh_key step;
// the wizard:
//   1. Validates the form via makeProfile (from hp-e7k profile.ts).
//   2. Persists the profile to disk (main process; renderer fires the
//      hoopoe.tunnel.testConnection bridge call).
//   3. Surfaces the live tunnel status (probing → opening → authenticating
//      → ready) by polling the FSM snapshot bridge.
//   4. Reports success via onComplete(profileId) so the wizard advances.
//   5. Reports failure via onFailed so the resume banner renders the
//      stable "Resume" CTA.
//
// The bridge interface mirrors what hp-fkov (ssh2 + PowerMonitor +
// heartbeat orchestrator) will expose at preload. Until that lands,
// the default bridge returns a typed "tunnel orchestrator not yet
// wired" error so the form stays exercisable end-to-end without
// blocking on the daemon.

import { useEffect, useState } from "react";
import { AlertCircle, CheckCircle2, Loader2, Server, ShieldCheck } from "lucide-react";

// Minimal duplication of the tunnel types from
// `apps/desktop/electron/tunnel/types.ts` (hp-e7k). The renderer's
// tsconfig only includes `src/`, so importing across the boundary
// fails at typecheck time. When packages/schemas/ provides a TS
// codegen target for the tunnel surface (hp-r3i extension), this
// duplication goes away. Until then, the canonical declarations live
// in electron/tunnel/types.ts and the FSM tests guarantee parity.
const TUNNEL_STATES = [
  "unconfigured",
  "ssh_probing",
  "bootstrapping",
  "tunnel_connecting",
  "authenticating",
  "ready",
  "degraded",
  "reconnecting",
  "disconnected",
] as const;
type TunnelState = (typeof TUNNEL_STATES)[number];

interface ConnectionFault {
  readonly code: string;
  readonly message: string;
  readonly capturedAt: string;
}

interface TunnelSnapshot {
  readonly state: TunnelState;
  readonly activeProfileId: string | null;
  readonly localPort: number | null;
  readonly lastFault: ConnectionFault | null;
  readonly reconnectAttempts: number;
  readonly nextRetryAt: string | null;
}

export interface VpsConnectSelection {
  /** Profile id created by the test-connection call. */
  readonly profileId: string;
  /** Local TCP port the tunnel forwarded to. */
  readonly localPort: number;
  /** Tunnel state snapshot at the moment of success (always "ready"). */
  readonly snapshot: TunnelSnapshot;
  /** Sanitized SSH profile submitted by the user. StepVpsConnect attaches
   *  this before the wizard persists its checkpoint. */
  readonly profile?: VpsConnectFormInput;
}

export interface VpsConnectFormInput {
  readonly label: string;
  readonly host: string;
  readonly port: number;
  readonly username: string;
  readonly privateKeyPath: string;
}

export interface VpsConnectFingerprintPrompt {
  readonly host: string;
  readonly port: number;
  readonly fingerprint: string;
}

export interface VpsConnectBridge {
  /** Run an SSH probe + open tunnel + authenticate. Returns the profileId
   *  + local port + final snapshot on success. Throws a typed Error on
   *  failure. */
  readonly testConnection: (input: VpsConnectFormInput) => Promise<VpsConnectSelection>;
  /** Subscribe to FSM snapshot updates while the test-connection call is
   *  in flight. Returns an unsubscribe handle. Optional — when absent,
   *  the form just shows the final result. */
  readonly subscribeSnapshot?: (listener: (snapshot: TunnelSnapshot) => void) => () => void;
  /** Approve a TOFU known-hosts prompt. The orchestrator pauses the
   *  connect attempt until this resolves. */
  readonly acceptFingerprint?: (input: VpsConnectFingerprintPrompt) => Promise<void>;
}

export interface StepVpsConnectProps {
  /** Default private-key path surfaced from the ssh_key step. The user
   *  can still override the path. */
  readonly defaultPrivateKeyPath?: string;
  readonly onComplete: (selection: VpsConnectSelection) => void;
  readonly onFailed: (failure: { readonly code: string; readonly message: string }) => void;
  readonly bridge?: VpsConnectBridge;
}

interface FormState {
  readonly label: string;
  readonly host: string;
  readonly port: string;
  readonly username: string;
  readonly privateKeyPath: string;
}

const DEFAULT_FORM: FormState = {
  label: "",
  host: "",
  port: "22",
  username: "",
  privateKeyPath: "",
};

export interface VpsConnectCheckpointData extends Record<string, unknown> {
  readonly profileId: string;
  readonly localPort: number;
  readonly state: TunnelState;
  readonly profile: VpsConnectFormInput | null;
}

export class VpsConnectFingerprintRequiredError extends Error {
  override readonly name = "VpsConnectFingerprintRequiredError";
  readonly code = "tofu_required";
  readonly prompt: VpsConnectFingerprintPrompt;

  constructor(prompt: VpsConnectFingerprintPrompt) {
    super(`Trust the SSH host key for ${prompt.host}:${prompt.port} to continue.`);
    this.prompt = prompt;
  }
}

export function validateVpsConnectForm(
  input: FormState,
): { ok: true; profile: VpsConnectFormInput } | {
  ok: false;
  errors: Partial<Record<keyof FormState, string>>;
} {
  const found: Partial<Record<keyof FormState, string>> = {};
  if (!input.label.trim()) found.label = "Label is required";
  if (!input.host.trim()) found.host = "Host is required";
  const portRaw = input.port.trim();
  const portNum = Number.parseInt(portRaw, 10);
  if (!/^\d+$/.test(portRaw) || !Number.isFinite(portNum) || portNum < 1 || portNum > 65535) {
    found.port = "Port must be 1..65535";
  }
  if (!input.username.trim()) found.username = "Username is required";
  if (!input.privateKeyPath.trim()) {
    found.privateKeyPath = "SSH private key path is required (pick one in the SSH key step or browse here)";
  }
  if (Object.keys(found).length > 0) {
    return { ok: false, errors: found };
  }
  return {
    ok: true,
    profile: {
      label: input.label.trim(),
      host: input.host.trim(),
      port: portNum,
      username: input.username.trim(),
      privateKeyPath: input.privateKeyPath.trim(),
    },
  };
}

export function buildVpsConnectCheckpointData(
  selection: VpsConnectSelection,
): VpsConnectCheckpointData {
  return {
    profileId: selection.profileId,
    localPort: selection.localPort,
    state: selection.snapshot.state,
    profile: selection.profile ?? null,
  };
}

export function StepVpsConnect({
  bridge,
  defaultPrivateKeyPath,
  onComplete,
  onFailed,
}: StepVpsConnectProps) {
  const resolvedBridge = bridge ?? getDefaultBridge();
  const [form, setForm] = useState<FormState>({
    ...DEFAULT_FORM,
    privateKeyPath: defaultPrivateKeyPath ?? "",
  });
  const [errors, setErrors] = useState<Partial<Record<keyof FormState, string>>>({});
  const [submitting, setSubmitting] = useState(false);
  const [snapshot, setSnapshot] = useState<TunnelSnapshot | null>(null);
  const [bannerError, setBannerError] = useState<string | null>(null);
  const [fingerprintPrompt, setFingerprintPrompt] = useState<VpsConnectFingerprintPrompt | null>(null);

  // Subscribe to FSM snapshot updates while submitting (so the user sees
  // ssh_probing → tunnel_connecting → authenticating → ready).
  useEffect(() => {
    if (!submitting || !resolvedBridge?.subscribeSnapshot) return;
    const unsub = resolvedBridge.subscribeSnapshot((next) => setSnapshot(next));
    return () => unsub();
  }, [submitting, resolvedBridge]);

  function update<K extends keyof FormState>(key: K, value: string): void {
    setForm((prev) => ({ ...prev, [key]: value }));
    if (errors[key]) {
      setErrors((prev) => ({ ...prev, [key]: undefined }));
    }
  }

  async function handleSubmit(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setBannerError(null);
    const validation = validateVpsConnectForm(form);
    if (!validation.ok) {
      setErrors(validation.errors);
      return;
    }
    if (!resolvedBridge) {
      const message = "Hoopoe tunnel orchestrator not yet wired in this build (hp-fkov follow-up).";
      setBannerError(message);
      onFailed({ code: "tunnel_bridge_unavailable", message });
      return;
    }
    setSubmitting(true);
    setSnapshot(null);
    try {
      const selection = await resolvedBridge.testConnection(validation.profile);
      onComplete({ ...selection, profile: validation.profile });
    } catch (err) {
      const prompt = fingerprintPromptFromError(err);
      if (prompt !== null) {
        setFingerprintPrompt(prompt);
        onFailed({ code: "tofu_required", message: (err as Error).message });
        return;
      }
      const message = (err as Error).message;
      setBannerError(message);
      // Map well-known orchestrator errors to stable codes; the FSM's
      // FaultCode enum is the canonical source.
      const code = guessCode(message);
      onFailed({ code, message });
    } finally {
      setSubmitting(false);
    }
  }

  async function handleAcceptFingerprint(prompt: VpsConnectFingerprintPrompt): Promise<void> {
    if (!resolvedBridge?.acceptFingerprint) return;
    try {
      await resolvedBridge.acceptFingerprint(prompt);
      setFingerprintPrompt(null);
    } catch (err) {
      setBannerError((err as Error).message);
      setFingerprintPrompt(null);
    }
  }

  return (
    <section
      aria-labelledby="hh-step-vps-connect-title"
      className="hh-wizard-step"
      data-testid="wizard-step-vps_connect"
    >
      <header className="hh-wizard-step-header">
        <span className="hh-stage-kicker">STEP 04</span>
        <h2 id="hh-step-vps-connect-title">Connect to your VPS</h2>
        <p>
          Hoopoe opens an SSH tunnel from your Mac to the daemon on the VPS. We
          never bind a public daemon port; everything stays on{" "}
          <code>127.0.0.1</code> at both ends.
        </p>
      </header>
      <form
        className="hh-wizard-form hh-wizard-form-vps"
        data-testid="wizard-vps-connect-form"
        onSubmit={(event) => void handleSubmit(event)}
      >
        <ConnectField
          autoFocus
          error={errors.label}
          id="vps-label"
          label="Profile label"
          onChange={(value) => update("label", value)}
          placeholder="Production VPS"
          value={form.label}
        />
        <div className="hh-wizard-row">
          <ConnectField
            error={errors.host}
            id="vps-host"
            label="SSH host"
            onChange={(value) => update("host", value)}
            placeholder="vps.example.com"
            value={form.host}
            wide
          />
          <ConnectField
            error={errors.port}
            id="vps-port"
            label="Port"
            onChange={(value) => update("port", value)}
            placeholder="22"
            value={form.port}
          />
        </div>
        <ConnectField
          error={errors.username}
          id="vps-user"
          label="Username"
          onChange={(value) => update("username", value)}
          placeholder="ubuntu"
          value={form.username}
        />
        <ConnectField
          error={errors.privateKeyPath}
          hint={
            defaultPrivateKeyPath
              ? "Pre-filled from the SSH key step. Override only if you want a different key for this VPS."
              : "Path to the private key on your Mac (e.g. ~/.ssh/id_ed25519)."
          }
          id="vps-key"
          label="SSH private key path"
          onChange={(value) => update("privateKeyPath", value)}
          placeholder="~/.ssh/id_ed25519"
          value={form.privateKeyPath}
        />
        <button
          className="hh-wizard-primary"
          data-testid="wizard-vps-connect-submit"
          disabled={submitting}
          type="submit"
        >
          {submitting ? (
            <Loader2 className="hh-spin" size={14} strokeWidth={2.1} />
          ) : (
            <Server size={14} strokeWidth={2.1} />
          )}
          {submitting ? "Connecting..." : "Test connection"}
        </button>
      </form>

      {snapshot ? (
        <SnapshotPanel snapshot={snapshot} />
      ) : null}

      {fingerprintPrompt ? (
        <FingerprintPanel
          onAccept={() => void handleAcceptFingerprint(fingerprintPrompt)}
          onReject={() => setFingerprintPrompt(null)}
          prompt={fingerprintPrompt}
        />
      ) : null}

      {bannerError ? (
        <div
          className="hh-wizard-resume-banner hh-wizard-resume-banner-inline"
          data-testid="wizard-vps-connect-error"
          role="alert"
        >
          <AlertCircle size={16} strokeWidth={2.1} />
          <div>
            <strong>Connection failed</strong>
            <p>{bannerError}</p>
          </div>
        </div>
      ) : null}
    </section>
  );
}

interface ConnectFieldProps {
  readonly id: string;
  readonly label: string;
  readonly value: string;
  readonly placeholder: string;
  readonly onChange: (next: string) => void;
  readonly error?: string | undefined;
  readonly hint?: string;
  readonly autoFocus?: boolean;
  readonly wide?: boolean;
}

function ConnectField({
  autoFocus,
  error,
  hint,
  id,
  label,
  onChange,
  placeholder,
  value,
  wide,
}: ConnectFieldProps) {
  return (
    <div
      className="hh-wizard-field"
      data-error={error !== undefined}
      data-testid={`wizard-vps-field-${id}`}
      data-wide={wide ?? false}
    >
      <label htmlFor={id}>{label}</label>
      <input
        autoComplete="off"
        autoFocus={autoFocus}
        id={id}
        name={id}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        type="text"
        value={value}
      />
      {error ? (
        <p className="hh-wizard-field-error" data-testid={`wizard-vps-field-${id}-error`} role="alert">
          {error}
        </p>
      ) : hint ? (
        <p className="hh-wizard-field-hint">{hint}</p>
      ) : null}
    </div>
  );
}

function SnapshotPanel({ snapshot }: { readonly snapshot: TunnelSnapshot }) {
  const stateLabel = SNAPSHOT_LABELS[snapshot.state] ?? snapshot.state;
  return (
    <aside
      aria-label="Tunnel status"
      className="hh-wizard-tunnel-status"
      data-testid="wizard-vps-connect-status"
      data-state={snapshot.state}
    >
      {snapshot.state === "ready" ? (
        <CheckCircle2 size={16} strokeWidth={2.1} />
      ) : snapshot.state === "reconnecting" || snapshot.state === "ssh_probing" ? (
        <Loader2 className="hh-spin" size={16} strokeWidth={2.1} />
      ) : (
        <Server size={16} strokeWidth={2.1} />
      )}
      <div>
        <strong>{stateLabel}</strong>
        {snapshot.lastFault ? (
          <p data-testid="wizard-vps-connect-status-fault">
            {snapshot.lastFault.code}: {snapshot.lastFault.message}
          </p>
        ) : snapshot.localPort !== null ? (
          <p>Tunnel forwarded to localhost:{snapshot.localPort}</p>
        ) : null}
      </div>
    </aside>
  );
}

function FingerprintPanel({
  onAccept,
  onReject,
  prompt,
}: {
  readonly prompt: VpsConnectFingerprintPrompt;
  readonly onAccept: () => void;
  readonly onReject: () => void;
}) {
  return (
    <aside
      aria-labelledby="hh-vps-connect-tofu-title"
      className="hh-wizard-tunnel-tofu"
      data-testid="wizard-vps-connect-tofu"
    >
      <ShieldCheck size={20} strokeWidth={2.1} />
      <div>
        <strong id="hh-vps-connect-tofu-title">Trust on first use</strong>
        <p>
          The host <code>{prompt.host}:{prompt.port}</code> presented a key with
          fingerprint:
        </p>
        <code className="hh-wizard-tunnel-tofu-fingerprint">{prompt.fingerprint}</code>
        <p>If you recognize this fingerprint, trust it now and we'll save it.</p>
        <div className="hh-wizard-tunnel-tofu-actions">
          <button
            className="hh-wizard-secondary"
            data-testid="wizard-vps-connect-tofu-reject"
            onClick={onReject}
            type="button"
          >
            Cancel
          </button>
          <button
            className="hh-wizard-primary"
            data-testid="wizard-vps-connect-tofu-accept"
            onClick={onAccept}
            type="button"
          >
            Trust this host
          </button>
        </div>
      </div>
    </aside>
  );
}

const SNAPSHOT_LABELS: Partial<Record<TunnelState, string>> = {
  ssh_probing: "Probing SSH...",
  bootstrapping: "Installing daemon...",
  tunnel_connecting: "Opening tunnel...",
  authenticating: "Authenticating...",
  ready: "Connected",
  degraded: "Connected (degraded)",
  reconnecting: "Reconnecting...",
  disconnected: "Disconnected",
  unconfigured: "Idle",
};

function guessCode(message: string): string {
  const lower = message.toLowerCase();
  if (lower.includes("permission denied") || lower.includes("authentication failed")) {
    return "auth_rejected";
  }
  if (lower.includes("network is unreachable") || lower.includes("could not resolve host")) {
    return "ssh_unreachable";
  }
  if (lower.includes("not yet wired")) return "tunnel_bridge_unavailable";
  return "unknown";
}

function fingerprintPromptFromError(error: unknown): VpsConnectFingerprintPrompt | null {
  if (error instanceof VpsConnectFingerprintRequiredError) {
    return error.prompt;
  }
  if (typeof error !== "object" || error === null) return null;
  const prompt = (error as { readonly prompt?: unknown }).prompt;
  if (typeof prompt !== "object" || prompt === null) return null;
  const candidate = prompt as Partial<VpsConnectFingerprintPrompt>;
  if (
    typeof candidate.host === "string" &&
    typeof candidate.port === "number" &&
    typeof candidate.fingerprint === "string"
  ) {
    return {
      host: candidate.host,
      port: candidate.port,
      fingerprint: candidate.fingerprint,
    };
  }
  return null;
}

interface BridgeShape {
  readonly tunnel?: VpsConnectBridge;
}

function getDefaultBridge(): VpsConnectBridge | null {
  if (typeof window === "undefined") return null;
  const tunnel = (window as Window & { readonly hoopoe?: BridgeShape }).hoopoe?.tunnel;
  if (!tunnel || typeof tunnel !== "object") return null;
  return tunnel as unknown as VpsConnectBridge;
}

/** Defensive utility — exported for tests so they can assert the
 *  state-label mapping covers every valid TunnelState. */
export function _stateLabelMapHasAllStates(): boolean {
  return TUNNEL_STATES.every((state) => SNAPSHOT_LABELS[state] !== undefined);
}
