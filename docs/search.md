# Search

Hoopoe search has two backends:

- Desktop primary: ripgrep over the read-only local clone. This is the fast
  path for committed code mirrored from origin.
- Daemon fallback: `GET /v1/projects/{projectId}/grep`, which runs ripgrep
  against the VPS clone so users can include uncommitted or unpushed work.

The daemon fallback accepts:

| Query parameter | Meaning |
| --- | --- |
| `query` / `q` | Search text or regex. Required. |
| `literal` | When `true`, passes `--fixed-strings`. |
| `maxResults` | Stored result cap; clamped to 5,000. |
| `path` | Optional repeated repo-relative scope. |

The fallback uses explicit argv only, never a shell. It runs `rg --json`,
honors `.gitignore`, searches hidden files, excludes `.git`, and adds
`.hoopoeignore` as an ignore file when present. Search paths are resolved
inside the project clone before ripgrep starts, so renderer-supplied paths
cannot escape the repo root.
