# @hoopoe/planning-prompts

Versioned prompt registry for the Phase 5 planning pipeline.

The package keeps each pipeline prompt as a Markdown artifact with frontmatter, verifies body hashes against `manifest.json`, and checks deterministic regression fixtures under `packages/fixtures/planning-prompt-regression/`.

Run:

```bash
bun run --cwd packages/planning-prompts typecheck
bun run --cwd packages/planning-prompts test
```
