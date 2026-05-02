// Hoopoe desktop renderer-isolation rules (hp-rflj) +
// Codex-shape scrub identifier ban (hp-4nrd).
//
// **Note on tooling:** Hoopoe lints with `oxlint` (root `.oxlintrc.json`).
// This `.eslintrc.cjs` exists to document the rules the bead requires and
// provide the canonical eslint-shape config for editors / CI plugins that
// only understand eslint. The actual enforcement runs as custom checks
// alongside oxlint:
//
//   - hp-rflj renderer isolation:
//       scripts/rendererlint/check-renderer-isolation.ts
//   - hp-ara provider-SDK ban:
//       scripts/providerlint/check-provider-sdks.ts
//   - hp-4nrd Codex-shape scrub:
//       scripts/codex-shape-scrub/check-codex-shape-scrub.ts
//
// All three are wired into the root `lint` script and gate CI. They are
// more accurate than oxlint's pattern-based rules for the patterns we
// need (whole-word identifier matching with comment / string-literal
// suppression for the Codex-shape scrub; cross-pattern banned-globals for
// renderer isolation).
//
// The list of banned identifiers / imports / globals here mirrors the
// custom checks so a future eslint adoption matches.

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
    {
      // hp-4nrd Codex-shape scrub. §14 of plan.md names "Lifted code
      // carries Codex-shaped assumptions" as a real risk; this rule
      // refuses chat-thread / model-provider / message-list identifier
      // shapes anywhere outside `apps/desktop/src/vendored/t3code/**`,
      // so scrubbed adapter code in `apps/desktop/src/main/**` and
      // every Hoopoe surface beyond it stays in Hoopoe's domain
      // language (bead, swarm, agent, plan, activity).
      //
      // Canonical enforcement runs in
      // `scripts/codex-shape-scrub/check-codex-shape-scrub.ts`; this
      // eslint stanza documents the equivalent policy.
      files: [
        "src/**/*.{ts,tsx}",
        "!src/vendored/t3code/**",
      ],
      rules: {
        "id-denylist": [
          "error",
          "Thread",
          "Chat",
          "Provider",
          "MessageList",
          "Conversation",
          "ConversationItem",
          "ChatTurn",
          "MAX_THREAD_MESSAGES",
          "messageList",
          "threadList",
        ],
      },
    },
  ],
};
