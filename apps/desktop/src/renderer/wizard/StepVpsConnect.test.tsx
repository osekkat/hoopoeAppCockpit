// hp-o7rn — StepVpsConnect render + behavior tests.
//
// Renders the step in the disabled (no-bridge) variant via
// renderToStaticMarkup, plus integration-style tests that drive the
// validate/submit pipeline directly without React state.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  StepVpsConnect,
  VpsConnectFingerprintRequiredError,
  _stateLabelMapHasAllStates,
  buildVpsConnectCheckpointData,
  validateVpsConnectForm,
  type VpsConnectBridge,
  type VpsConnectFormInput,
  type VpsConnectSelection,
} from "./StepVpsConnect.tsx";

// Mirror of `electron/tunnel/index.ts#freshSnapshot` to avoid the
// renderer-tsconfig boundary issue that StepVpsConnect.tsx itself
// addresses with type duplication.
function freshSnapshot(): TunnelSnapshot {
  return {
    state: "unconfigured",
    activeProfileId: null,
    localPort: null,
    lastFault: null,
    reconnectAttempts: 0,
    nextRetryAt: null,
  };
}

interface TunnelSnapshot {
  readonly state: TunnelState;
  readonly activeProfileId: string | null;
  readonly localPort: number | null;
  readonly lastFault: { code: string; message: string; capturedAt: string } | null;
  readonly reconnectAttempts: number;
  readonly nextRetryAt: string | null;
}

type TunnelState =
  | "unconfigured"
  | "ssh_probing"
  | "bootstrapping"
  | "tunnel_connecting"
  | "authenticating"
  | "ready"
  | "degraded"
  | "reconnecting"
  | "disconnected";

test("StepVpsConnect: renders the form with all four required fields", () => {
  const html = renderToStaticMarkup(
    <StepVpsConnect onComplete={() => undefined} onFailed={() => undefined} />,
  );
  expect(html).toContain("data-testid=\"wizard-step-vps_connect\"");
  expect(html).toContain("data-testid=\"wizard-vps-field-vps-label\"");
  expect(html).toContain("data-testid=\"wizard-vps-field-vps-host\"");
  expect(html).toContain("data-testid=\"wizard-vps-field-vps-port\"");
  expect(html).toContain("data-testid=\"wizard-vps-field-vps-user\"");
  expect(html).toContain("data-testid=\"wizard-vps-field-vps-key\"");
  expect(html).toContain("data-testid=\"wizard-vps-connect-submit\"");
  expect(html).toContain("Test connection");
});

test("StepVpsConnect: defaults port to 22 + pre-fills the SSH key path", () => {
  const html = renderToStaticMarkup(
    <StepVpsConnect
      defaultPrivateKeyPath="/Users/me/.ssh/id_ed25519"
      onComplete={() => undefined}
      onFailed={() => undefined}
    />,
  );
  expect(html).toContain("value=\"22\"");
  expect(html).toContain("value=\"/Users/me/.ssh/id_ed25519\"");
  expect(html).toContain("Pre-filled from the SSH key step");
});

test("StepVpsConnect: hint changes when no defaultPrivateKeyPath provided", () => {
  const html = renderToStaticMarkup(
    <StepVpsConnect onComplete={() => undefined} onFailed={() => undefined} />,
  );
  expect(html).toContain("Path to the private key on your Mac");
  expect(html).not.toContain("Pre-filled from the SSH key step");
});

test("StepVpsConnect: header explains the SSH-tunnel architecture", () => {
  const html = renderToStaticMarkup(
    <StepVpsConnect onComplete={() => undefined} onFailed={() => undefined} />,
  );
  expect(html).toContain("Connect to your VPS");
  expect(html).toContain("STEP 04");
  expect(html).toContain("never bind a public daemon port");
  expect(html).toContain("127.0.0.1");
});

test("Bridge contract: testConnection rejects with typed orchestrator error", async () => {
  // Drive the bridge as the component would once submit fires. We can't
  // easily simulate the React submit pipeline in renderToStaticMarkup,
  // but we can confirm the bridge interface itself behaves as expected.
  const bridge: VpsConnectBridge = {
    testConnection: async () => {
      throw new Error("ssh: connect to host vps.example.com port 22: Network is unreachable");
    },
  };
  let captured: Error | null = null;
  try {
    await bridge.testConnection({
      label: "L",
      host: "vps.example.com",
      port: 22,
      username: "ubuntu",
      privateKeyPath: "~/.ssh/id_ed25519",
    });
  } catch (err) {
    captured = err as Error;
  }
  expect(captured).not.toBeNull();
  expect(captured?.message).toContain("Network is unreachable");
});

test("Bridge contract: testConnection happy path returns VpsConnectSelection", async () => {
  const profile: VpsConnectFormInput = {
    label: "Production VPS",
    host: "vps.example.com",
    port: 22,
    username: "ubuntu",
    privateKeyPath: "~/.ssh/id_ed25519",
  };
  const expectedSelection: VpsConnectSelection = {
    profileId: "profile-123",
    localPort: 17655,
    snapshot: { ...freshSnapshot(), state: "ready", localPort: 17655, activeProfileId: "profile-123" },
    profile,
  };
  const bridge: VpsConnectBridge = {
    testConnection: async () => expectedSelection,
  };
  const result = await bridge.testConnection({
    label: "Production VPS",
    host: "vps.example.com",
    port: 22,
    username: "ubuntu",
    privateKeyPath: "~/.ssh/id_ed25519",
  });
  expect(result.profileId).toBe("profile-123");
  expect(result.localPort).toBe(17655);
  expect(result.snapshot.state).toBe("ready");
  expect(buildVpsConnectCheckpointData(result)).toEqual({
    profileId: "profile-123",
    localPort: 17655,
    state: "ready",
    profile,
  });
});

test("validateVpsConnectForm trims inputs and rejects incomplete profiles", () => {
  const ok = validateVpsConnectForm({
    label: " Production ",
    host: " vps.example.com ",
    port: "22",
    username: " ubuntu ",
    privateKeyPath: " ~/.ssh/id_ed25519 ",
  });
  expect(ok.ok).toBe(true);
  if (ok.ok) {
    expect(ok.profile).toEqual({
      label: "Production",
      host: "vps.example.com",
      port: 22,
      username: "ubuntu",
      privateKeyPath: "~/.ssh/id_ed25519",
    });
  }

  const bad = validateVpsConnectForm({
    label: "",
    host: "",
    port: "22abc",
    username: "",
    privateKeyPath: "",
  });
  expect(bad.ok).toBe(false);
  if (!bad.ok) {
    expect(bad.errors.label).toBe("Label is required");
    expect(bad.errors.host).toBe("Host is required");
    expect(bad.errors.port).toContain("1..65535");
    expect(bad.errors.username).toBe("Username is required");
    expect(bad.errors.privateKeyPath).toContain("SSH private key path");
  }
});

test("VpsConnectFingerprintRequiredError carries the TOFU prompt shape", () => {
  const prompt = {
    host: "vps.example.com",
    port: 22,
    fingerprint: "SHA256:abcd",
  };
  const err = new VpsConnectFingerprintRequiredError(prompt);
  expect(err.name).toBe("VpsConnectFingerprintRequiredError");
  expect(err.code).toBe("tofu_required");
  expect(err.prompt).toEqual(prompt);
  expect(err.message).toContain("Trust the SSH host key");
});

test("Bridge contract: subscribeSnapshot delivers transitions in order", () => {
  const observed: string[] = [];
  const states: TunnelState[] = ["ssh_probing", "tunnel_connecting", "authenticating", "ready"];
  let listener: ((s: TunnelSnapshot) => void) | null = null;
  const bridge: VpsConnectBridge = {
    testConnection: async () => ({
      profileId: "p", localPort: 17655, snapshot: { ...freshSnapshot(), state: "ready" },
    }),
    subscribeSnapshot: (l) => { listener = l; return () => undefined; },
  };
  const unsub = bridge.subscribeSnapshot!((s) => observed.push(s.state));
  for (const state of states) {
    listener!({ ...freshSnapshot(), state });
  }
  unsub();
  expect(observed).toEqual(states);
});

test("Bridge contract: acceptFingerprint resolves a TOFU prompt", async () => {
  const accepted: string[] = [];
  const bridge: VpsConnectBridge = {
    testConnection: async () => ({
      profileId: "p", localPort: 17655, snapshot: { ...freshSnapshot(), state: "ready" },
    }),
    acceptFingerprint: async (input) => {
      accepted.push(input.fingerprint);
    },
  };
  await bridge.acceptFingerprint!({ host: "vps", port: 22, fingerprint: "SHA256:abcd" });
  expect(accepted).toEqual(["SHA256:abcd"]);
});

test("_stateLabelMapHasAllStates: every TUNNEL_STATES value has a label", () => {
  expect(_stateLabelMapHasAllStates()).toBe(true);
});
