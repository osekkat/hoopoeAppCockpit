import { describe, expect, test } from "bun:test";
import { createHash, verify } from "node:crypto";
import { spawn } from "node:child_process";
import * as FS from "node:fs";
import * as OS from "node:os";
import * as Path from "node:path";
import { GenericProvider } from "electron-updater/out/providers/GenericProvider.js";

const repoRoot = Path.resolve(import.meta.dir, "../../..", "..");

describe("hp-2ae3 release acceptance", () => {
  test("mock DMG build produces update metadata served through electron-updater generic provider", async () => {
    const releaseRoot = FS.mkdtempSync(Path.join(OS.tmpdir(), "hoopoe-release-acceptance-"));
    const version = "0.0.0-hp-2ae3.1";
    const port = 33_000 + Math.floor(Math.random() * 1_000);

    const build = await runBun([
      "scripts/build-desktop-artifact.ts",
      "--platform",
      "mac",
      "--target",
      "dmg",
      "--arch",
      "arm64",
      "--build-version",
      version,
      "--output-dir",
      releaseRoot,
      "--mock-updates",
      "--mock-artifact",
    ]);
    if (build.exitCode !== 0) {
      console.error(build.stderr);
    }
    expect(build.exitCode).toBe(0);

    const artifactName = `Hoopoe-${version}-arm64.dmg`;
    const artifactPath = Path.join(releaseRoot, artifactName);
    expect(FS.existsSync(artifactPath)).toBe(true);
    expect(FS.existsSync(Path.join(releaseRoot, "latest-mac.yml"))).toBe(true);
    expect(FS.existsSync(Path.join(releaseRoot, "update.json"))).toBe(true);

    const server = spawn("bun", ["scripts/mock-update-server.ts"], {
      cwd: repoRoot,
      env: {
        ...process.env,
        HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_PORT: String(port),
        HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_ROOT: releaseRoot,
      },
      stdio: ["ignore", "pipe", "pipe"],
    });

    try {
      await waitForOutput(server, "Hoopoe mock update server listening");

      const updateResponse = await fetchWithTimeout(`http://localhost:${port}/update.json`);
      expect(updateResponse.status).toBe(200);
      expect(updateResponse.headers.get("content-type")).toContain("application/json");
      const updateJson = (await updateResponse.json()) as {
        artifact: { file: string; sha512: string; size: number; url: string };
        signature: { algorithm: string; publicKeyPem: string; signedPayload: string; value: string };
        notarization: { mode: string; status: string };
        version: string;
      };

      const artifactResponse = await fetchWithTimeout(`http://localhost:${port}/${artifactName}`);
      expect(artifactResponse.status).toBe(200);
      const artifactBytes = Buffer.from(await artifactResponse.arrayBuffer());
      expect(updateJson.version).toBe(version);
      expect(updateJson.artifact.file).toBe(artifactName);
      expect(updateJson.artifact.size).toBe(artifactBytes.byteLength);
      expect(updateJson.artifact.sha512).toBe(sha512(artifactBytes));
      expect(updateJson.notarization).toEqual({ mode: "stub", status: "accepted", authority: "hp-2ae3 mock acceptance" });
      expect(
        verify(
          null,
          Buffer.from(updateJson.signature.signedPayload),
          updateJson.signature.publicKeyPem,
          Buffer.from(updateJson.signature.value, "base64"),
        ),
      ).toBe(true);

      const provider = new GenericProvider(
        { provider: "generic", url: `http://localhost:${port}` },
        { channel: "latest", isAddNoCacheQuery: false },
        {
          platform: "darwin",
          executor: {
            request: async (options: unknown) => {
              const response = await fetchWithTimeout(requestUrl(options));
              expect(response.status).toBe(200);
              return response.text();
            },
          },
          isUseMultipleRangeRequest: false,
        },
      );
      const latest = await provider.getLatestVersion();
      const files = provider.resolveFiles(latest);
      expect(latest.version).toBe(version);
      expect(files[0]?.info.sha512).toBe(updateJson.artifact.sha512);
      expect(files[0]?.url.pathname.endsWith(`/${artifactName}`)).toBe(true);
    } finally {
      server.kill();
    }
  });
});

function sha512(bytes: Buffer) {
  return createHash("sha512").update(bytes).digest("base64");
}

async function runBun(args: readonly string[]) {
  const child = spawn("bun", [...args], {
    cwd: repoRoot,
    stdio: ["ignore", "pipe", "pipe"],
  });
  const [stdout, stderr, exitCode] = await Promise.all([
    collectStream(child.stdout),
    collectStream(child.stderr),
    new Promise<number>((resolve) => {
      child.on("close", (code) => resolve(code ?? 1));
    }),
  ]);

  return { stdout, stderr, exitCode };
}

function fetchWithTimeout(url: string): Promise<Response> {
  return globalThis.fetch(url, { signal: AbortSignal.timeout(4_000) });
}

function requestUrl(options: unknown) {
  if (options instanceof URL) return options.href;
  if (typeof options === "string") return options;
  const requestOptions = options as {
    protocol?: string;
    hostname?: string;
    host?: string;
    port?: string | number;
    path?: string;
    pathname?: string;
  };
  const protocol = requestOptions.protocol ?? "http:";
  const hostname = requestOptions.hostname ?? requestOptions.host ?? "localhost";
  const port = requestOptions.port ? `:${requestOptions.port}` : "";
  const requestPath = requestOptions.path ?? requestOptions.pathname ?? "/";
  return `${protocol}//${hostname}${port}${requestPath}`;
}

function waitForOutput(child: ReturnType<typeof spawn>, text: string) {
  return new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`Timed out waiting for: ${text}`)), 4_000);
    const onData = (chunk: Buffer) => {
      const output = chunk.toString("utf8");
      if (output.includes(text)) {
        clearTimeout(timeout);
        resolve();
      }
    };
    child.stdout?.on("data", onData);
    child.stderr?.on("data", (chunk: Buffer) => {
      const output = chunk.toString("utf8");
      if (output.includes("EADDRINUSE")) {
        clearTimeout(timeout);
        reject(new Error(output));
      }
    });
    child.on("exit", (code) => {
      clearTimeout(timeout);
      reject(new Error(`mock update server exited early with code ${code ?? "unknown"}`));
    });
  });
}

async function collectStream(stream: NodeJS.ReadableStream | null) {
  if (!stream) return "";
  let output = "";
  for await (const chunk of stream) {
    output += Buffer.from(chunk as Buffer).toString("utf8");
  }
  return output;
}
