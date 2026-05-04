// hp-2qn — Disk-pressure chaos primitive.
//
// Writes padding files inside an allow-listed directory to simulate
// "disk full" / "near-disk-full" conditions. Strictly bounded:
// refuses to write outside the supplied directory, refuses sizes
// above a hard ceiling, and exposes a `release()` to delete every
// file the call created. Tests that forget to release leak the
// allocation only inside the temp dir — never in shared filesystem
// space.

import { mkdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { randomUUID } from "node:crypto";

export class ChaosDiskPressureError extends Error {
  override readonly name = "ChaosDiskPressureError";
  constructor(message: string) {
    super(message);
  }
}

export interface DiskPressureOptions {
  /** Directory to fill. MUST be a writable temp directory the caller
   *  owns; the primitive will refuse paths above the user's home or
   *  outside `/tmp` / `/var/tmp` to prevent accidents. */
  dir: string;
  /** Total bytes to write across N padding files. Hard ceiling: 1 GiB
   *  per call (override with `allowAboveCeiling`). */
  totalBytes: number;
  /** Per-file size; the primitive splits totalBytes across `ceil(total/perFile)` files. Default: 16 MiB. */
  perFileBytes?: number;
  /** Override the safety ceiling. Default: 1 GiB. Use cautiously. */
  allowAboveCeiling?: boolean;
}

export interface DiskPressureHandle {
  /** Absolute paths of the padding files written. */
  paths: readonly string[];
  /** Total bytes written. */
  bytesWritten: number;
  /** Delete every file written. Idempotent. */
  release: () => void;
}

const DEFAULT_PER_FILE = 16 * 1024 * 1024; // 16 MiB
const HARD_CEILING_BYTES = 1024 * 1024 * 1024; // 1 GiB

const ALLOWED_PREFIXES = ["/tmp/", "/var/tmp/", "/private/tmp/"];

function allowed(dir: string): boolean {
  const r = resolve(dir);
  return ALLOWED_PREFIXES.some((prefix) => r.startsWith(prefix));
}

/** Write `totalBytes` of padding into `dir`. Returns a handle that
 *  records the files written + a `release()` that deletes them. */
export function fillDisk(options: DiskPressureOptions): DiskPressureHandle {
  const dir = resolve(options.dir);
  if (!allowed(dir)) {
    throw new ChaosDiskPressureError(
      `fillDisk: refusing to write into ${dir}; only ${ALLOWED_PREFIXES.join(", ")} are allow-listed`,
    );
  }
  if (options.totalBytes <= 0) {
    throw new ChaosDiskPressureError(`fillDisk: totalBytes must be > 0, got ${options.totalBytes}`);
  }
  if (options.totalBytes > HARD_CEILING_BYTES && options.allowAboveCeiling !== true) {
    throw new ChaosDiskPressureError(
      `fillDisk: refusing to write ${options.totalBytes} bytes (>${HARD_CEILING_BYTES}); pass allowAboveCeiling: true to override`,
    );
  }
  mkdirSync(dir, { recursive: true });
  try {
    const stat = statSync(dir);
    if (!stat.isDirectory()) {
      throw new ChaosDiskPressureError(`fillDisk: ${dir} is not a directory`);
    }
  } catch (err) {
    throw new ChaosDiskPressureError(
      `fillDisk: cannot stat ${dir}: ${(err as Error).message}`,
    );
  }
  const perFile = options.perFileBytes ?? DEFAULT_PER_FILE;
  const fileCount = Math.ceil(options.totalBytes / perFile);
  const paths: string[] = [];
  let written = 0;
  // Use a stable padding pattern (zero bytes) so the writes are fast +
  // compressible — chaos tests care about size on disk, not entropy.
  const buffer = Buffer.alloc(Math.min(perFile, 1024 * 1024), 0);
  try {
    for (let i = 0; i < fileCount; i++) {
      const remaining = options.totalBytes - written;
      const thisFile = Math.min(perFile, remaining);
      if (thisFile <= 0) break;
      const path = resolve(dir, `chaos-pad-${randomUUID()}.bin`);
      // Write in chunks of `buffer.length` to avoid allocating a multi-GB Buffer.
      let bytesLeft = thisFile;
      const chunks: Buffer[] = [];
      while (bytesLeft > 0) {
        const slice = bytesLeft >= buffer.length ? buffer : buffer.subarray(0, bytesLeft);
        chunks.push(slice);
        bytesLeft -= slice.length;
      }
      writeFileSync(path, Buffer.concat(chunks), { mode: 0o644 });
      paths.push(path);
      written += thisFile;
    }
  } catch (err) {
    // Best-effort cleanup on partial-write failure.
    for (const p of paths) {
      try {
        rmSync(p, { force: true });
      } catch {
        // ignore
      }
    }
    throw new ChaosDiskPressureError(
      `fillDisk: failed to write padding (wrote ${written}/${options.totalBytes} bytes): ${(err as Error).message}`,
    );
  }
  let released = false;
  return {
    paths,
    bytesWritten: written,
    release: () => {
      if (released) return;
      released = true;
      for (const p of paths) {
        try {
          rmSync(p, { force: true });
        } catch {
          // best effort
        }
      }
    },
  };
}
