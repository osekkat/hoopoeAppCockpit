#!/usr/bin/env bun
// Originally from github.com/pingdotgg/t3code (MIT), commit 460d9c3.
// Adapted for Hoopoe's local electron-updater smoke tests.

import { createReadStream } from "node:fs";
import { access, realpath, stat } from "node:fs/promises";
import * as http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

type MockUpdateServerConfig = {
  port: number;
  rootRealPath: string;
};

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(scriptDir, "..");

function parsePort(value: string | undefined, fallback: number) {
  if (value === undefined || value.trim().length === 0) return fallback;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid mock update server port: ${value}`);
  }
  return port;
}

async function resolveConfig(): Promise<MockUpdateServerConfig> {
  const port = parsePort(process.env.HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_PORT, 3000);
  const root = process.env.HOOPOE_DESKTOP_MOCK_UPDATE_SERVER_ROOT ?? "release-mock";
  const rootPath = path.resolve(repoRoot, root);
  await access(rootPath);

  return {
    port,
    rootRealPath: await realpath(rootPath),
  };
}

function isOutsideRoot(rootRealPath: string, filePath: string) {
  const relativePath = path.relative(rootRealPath, filePath);
  return (
    relativePath === ".." ||
    relativePath.startsWith("../") ||
    relativePath.startsWith("..\\") ||
    path.isAbsolute(relativePath)
  );
}

function resolveRequestedFilePath(rootRealPath: string, requestUrl: string | undefined) {
  const rawPath = (requestUrl ?? "/").split("?", 1)[0] ?? "/";
  let decodedPath: string;

  try {
    decodedPath = decodeURIComponent(rawPath);
  } catch {
    return undefined;
  }

  if (!decodedPath || decodedPath.includes("\0")) return undefined;

  const filePath = path.resolve(
    rootRealPath,
    `.${decodedPath.startsWith("/") ? decodedPath : `/${decodedPath}`}`,
  );
  return isOutsideRoot(rootRealPath, filePath) ? undefined : filePath;
}

async function isServableFile(rootRealPath: string, filePath: string) {
  try {
    const resolvedPath = await realpath(filePath);
    if (isOutsideRoot(rootRealPath, resolvedPath)) return false;
    return (await stat(resolvedPath)).isFile();
  } catch {
    return false;
  }
}

async function serveFile(response: http.ServerResponse, filePath: string) {
  response.writeHead(200);
  await new Promise<void>((resolve, reject) => {
    const stream = createReadStream(filePath);
    stream.on("error", reject);
    stream.on("end", resolve);
    stream.pipe(response);
  });
}

async function main() {
  const config = await resolveConfig();
  const server = http.createServer((request, response) => {
    void (async () => {
      const filePath = resolveRequestedFilePath(config.rootRealPath, request.url);
      if (!filePath || !(await isServableFile(config.rootRealPath, filePath))) {
        response.writeHead(404, { "content-type": "text/plain; charset=utf-8" });
        response.end("Not Found");
        return;
      }

      await serveFile(response, filePath);
    })().catch((error: unknown) => {
      console.error(error instanceof Error ? error.message : error);
      response.writeHead(500, { "content-type": "text/plain; charset=utf-8" });
      response.end("Internal Server Error");
    });
  });

  server.listen(config.port, "localhost", () => {
    console.log(`Hoopoe mock update server listening on http://localhost:${config.port}`);
    console.log(`Serving ${config.rootRealPath}`);
  });
}

if (import.meta.main) {
  main().catch((error: unknown) => {
    console.error(error instanceof Error ? error.message : error);
    process.exit(1);
  });
}
