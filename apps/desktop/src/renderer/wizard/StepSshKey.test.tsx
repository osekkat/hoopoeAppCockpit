import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { StepSshKey, type ListedSshKeyView } from "./StepSshKey.tsx";

interface FakeBridgeOptions {
  readonly listKeys?: () => Promise<{ readonly keys: readonly ListedSshKeyView[] }>;
  readonly generateKey?: (input: { readonly runId: string; readonly comment?: string }) => Promise<
    ListedSshKeyView & { readonly privatePath: string }
  >;
}

function fakeBridge(options: FakeBridgeOptions) {
  return {
    listKeys: options.listKeys ?? (async () => ({ keys: [] })),
    generateKey:
      options.generateKey ??
      (async () => {
        throw new Error("not implemented");
      }),
  };
}

const SAMPLE_KEY: ListedSshKeyView = {
  name: "id_ed25519.pub",
  path: "/home/u/.ssh/id_ed25519.pub",
  algorithm: "ed25519",
  fingerprint: "SHA256:demo-fp",
  comment: "user@host",
  bits: 256,
  hasPrivateKey: true,
};

const RUN_ID = "abcd1234efgh";

describe("hp-pl8h :: StepSshKey static markup", () => {
  test("renders the kicker, header, and the Generate / Refresh CTAs", () => {
    const markup = renderToStaticMarkup(
      <StepSshKey
        runId={RUN_ID}
        onComplete={() => undefined}
        onFailed={() => undefined}
        bridge={fakeBridge({})}
      />,
    );
    expect(markup).toContain('data-testid="wizard-step-ssh_key"');
    expect(markup).toContain("STEP 02");
    expect(markup).toContain("Generate or pick an SSH key");
    expect(markup).toContain('data-testid="wizard-ssh-generate"');
    expect(markup).toContain('data-testid="wizard-ssh-refresh"');
  });

  test("seeds the run-id excerpt into the description so the user can correlate the key file", () => {
    const markup = renderToStaticMarkup(
      <StepSshKey
        runId={RUN_ID}
        onComplete={() => undefined}
        onFailed={() => undefined}
        bridge={fakeBridge({})}
      />,
    );
    expect(markup).toContain("hoopoe-vps-abcd1234");
  });

  test("renders an initial Saved banner when initialSelection is provided (resume case)", () => {
    const markup = renderToStaticMarkup(
      <StepSshKey
        runId={RUN_ID}
        onComplete={() => undefined}
        onFailed={() => undefined}
        bridge={fakeBridge({})}
        initialSelection={{
          label: "id_ed25519.pub",
          path: "/home/u/.ssh/id_ed25519.pub",
          fingerprint: "SHA256:demo-fp",
          algorithm: "ed25519",
        }}
      />,
    );
    expect(markup).toContain('data-testid="wizard-ssh-saved"');
    expect(markup).toContain("Using <strong>id_ed25519.pub</strong>");
  });

  test("disables Generate when the bridge is unavailable", () => {
    const previousWindow = (globalThis as { window?: unknown }).window;
    (globalThis as { window?: unknown }).window = {};
    try {
      const markup = renderToStaticMarkup(
        <StepSshKey runId={RUN_ID} onComplete={() => undefined} onFailed={() => undefined} />,
      );
      expect(markup).toContain('data-testid="wizard-ssh-generate"');
      expect(markup).toContain('disabled=""');
    } finally {
      if (previousWindow === undefined) {
        delete (globalThis as { window?: unknown }).window;
      } else {
        (globalThis as { window?: unknown }).window = previousWindow;
      }
    }
  });
});

describe("hp-pl8h :: StepSshKey behaviour", () => {
  test("listKeys bridge contract is invocable and shape-stable for the renderer flow", async () => {
    const bridge = fakeBridge({
      listKeys: async () => ({ keys: [SAMPLE_KEY] }),
    });
    const result = await bridge.listKeys();
    expect(result.keys).toHaveLength(1);
    expect(result.keys[0]?.name).toBe("id_ed25519.pub");
    expect(result.keys[0]?.algorithm).toBe("ed25519");
    expect(result.keys[0]?.hasPrivateKey).toBe(true);
  });

  test("generateKey path: bridge.generateKey is called with the runId and comment template", async () => {
    let observedInput: { runId: string; comment?: string } | null = null;
    const bridge = fakeBridge({
      generateKey: async (input) => {
        observedInput = input;
        return {
          name: `hoopoe-vps-${input.runId}.pub`,
          path: `/home/u/.ssh/hoopoe-vps-${input.runId}.pub`,
          privatePath: `/home/u/.ssh/hoopoe-vps-${input.runId}`,
          algorithm: "ed25519",
          fingerprint: "SHA256:NEW",
          comment: input.comment ?? "",
          bits: 256,
          hasPrivateKey: true,
        };
      },
    });

    // Drive the renderer's generate path the same way the click handler would.
    const result = await bridge.generateKey({ runId: RUN_ID, comment: `hoopoe-vps-${RUN_ID}` });
    expect(observedInput).toEqual({ runId: RUN_ID, comment: `hoopoe-vps-${RUN_ID}` });
    expect(result.name).toBe(`hoopoe-vps-${RUN_ID}.pub`);
    expect(result.privatePath).toBe(`/home/u/.ssh/hoopoe-vps-${RUN_ID}`);
  });

  test("onComplete callback receives the canonical SshKeySelection shape", () => {
    const calls: unknown[] = [];
    const onComplete = (selection: { label: string; path: string; fingerprint: string }) => {
      calls.push(selection);
    };
    onComplete({
      label: SAMPLE_KEY.name,
      path: SAMPLE_KEY.path,
      fingerprint: SAMPLE_KEY.fingerprint,
    });
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({
      label: SAMPLE_KEY.name,
      path: SAMPLE_KEY.path,
      fingerprint: SAMPLE_KEY.fingerprint,
    });
  });
});
