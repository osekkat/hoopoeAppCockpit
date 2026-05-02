// Originally from github.com/pingdotgg/t3code (MIT License)
// Copyright (c) 2026 T3 Tools Inc.
// Adapted for Hoopoe.
//
// Full MIT license text: vendored/t3code/LICENSE
//
// Lifted from t3code `apps/server/src/atomicWrite.ts`. The Effect / scoped
// fiber harness is removed; the durable-write semantics (write to a
// tempfile in the SAME directory, fsync the file descriptor, then rename
// over the canonical path) are unchanged.
//
// Per plan.md §3 "lifted patterns": tempfile + fsync + rename gives
// crash-safe atomic writes — a process death between the write and the
// rename leaves either the previous canonical file (or no canonical file
// if this is the first write), never a half-written file.

import * as FS from "node:fs";
import * as Path from "node:path";

export interface WriteFileStringAtomicallyInput {
  readonly filePath: string;
  readonly contents: string;
}

export function writeFileStringAtomically(input: WriteFileStringAtomicallyInput): void {
  const directory = Path.dirname(input.filePath);
  FS.mkdirSync(directory, { recursive: true });
  // Tempfile in the SAME directory ensures rename is on the same filesystem
  // (and therefore atomic). Including pid + timestamp avoids collisions
  // when multiple writers race.
  const tempPath = Path.join(
    directory,
    `${Path.basename(input.filePath)}.${process.pid}.${Date.now()}.tmp`,
  );

  const fd = FS.openSync(tempPath, "w");
  try {
    FS.writeSync(fd, input.contents);
    FS.fsyncSync(fd);
  } finally {
    FS.closeSync(fd);
  }
  FS.renameSync(tempPath, input.filePath);

  // Fsync the directory's directory entry so the rename is durable across
  // a power loss. Best-effort — Windows doesn't support directory fsync;
  // fall through silently.
  try {
    const dirFd = FS.openSync(directory, "r");
    try {
      FS.fsyncSync(dirFd);
    } finally {
      FS.closeSync(dirFd);
    }
  } catch {
    // Ignore — see above.
  }
}
