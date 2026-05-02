// Hoopoe desktop renderer-isolation rules (hp-rflj).
//
// **Note on tooling:** Hoopoe lints with `oxlint` (root `.oxlintrc.json`).
// This `.eslintrc.cjs` exists to document the rules the bead requires and
// provide the canonical eslint-shape config for editors / CI plugins that
// only understand eslint. The actual enforcement runs as a custom check
// at `scripts/rendererlint/check-renderer-isolation.ts` (parallel to the
// hp-ara provider-SDK lint), which is more accurate than oxlint's
// pattern-based no-restricted-imports for the patterns we need.
//
// The two enforcement paths share this list of banned identifiers /
// imports / globals so a future eslint adoption matches the custom check.

/** @type {import('eslint').Linter.Config} */
module.exports = {
  root: false,
  // Renderer-only files. Main-process and preload code is intentionally
  // excluded; they need fs / net / child_process etc. to do their job.
  overrides: [
    {
      files: ["src/renderer/**/*.{ts,tsx}"],
      rules: {
        // No Node built-ins in renderer code. The renderer reaches the
        // main process only through `window.hoopoe` (typed preload).
        "no-restricted-imports": [
          "error",
          {
            paths: [
              { name: "fs", message: "Renderer cannot read the filesystem; use window.hoopoe.files." },
              { name: "node:fs", message: "Renderer cannot read the filesystem; use window.hoopoe.files." },
              { name: "fs/promises", message: "Renderer cannot read the filesystem; use window.hoopoe.files." },
              { name: "node:fs/promises", message: "Renderer cannot read the filesystem; use window.hoopoe.files." },
              { name: "net", message: "Renderer cannot reach the network; use window.hoopoe.daemon." },
              { name: "node:net", message: "Renderer cannot reach the network; use window.hoopoe.daemon." },
              { name: "tls", message: "Renderer cannot reach TLS; use window.hoopoe.daemon." },
              { name: "node:tls", message: "Renderer cannot reach TLS; use window.hoopoe.daemon." },
              { name: "child_process", message: "Renderer cannot spawn processes; route through main + IpcRegistry." },
              { name: "node:child_process", message: "Renderer cannot spawn processes; route through main + IpcRegistry." },
              { name: "http", message: "Renderer must use fetch + window.hoopoe; no raw http." },
              { name: "node:http", message: "Renderer must use fetch + window.hoopoe; no raw http." },
              { name: "https", message: "Renderer must use fetch + window.hoopoe; no raw https." },
              { name: "node:https", message: "Renderer must use fetch + window.hoopoe; no raw https." },
              { name: "electron", message: "Renderer must not import electron directly; use window.hoopoe (preload bridge)." },
            ],
            patterns: [
              {
                group: ["electron/*"],
                message: "Renderer must not import electron internals; use window.hoopoe (preload bridge).",
              },
            ],
          },
        ],
        // No window.require / window.process / new Function / eval in
        // renderer. These are caught by `scripts/rendererlint/check-renderer-isolation.ts`
        // because oxlint's no-restricted-globals is plugin-dependent.
        "no-eval": "error",
        "no-new-func": "error",
        "no-restricted-globals": [
          "error",
          { name: "require", message: "Renderer has no `require`; route through window.hoopoe." },
          { name: "process", message: "Renderer has no `process`; route through window.hoopoe." },
        ],
      },
    },
  ],
};
