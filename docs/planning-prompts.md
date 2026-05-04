# Planning Prompts

`packages/planning-prompts/` is the canonical home for Phase 5 planning-pipeline prompts from plan.md §7.1.

Each prompt is a Markdown file with frontmatter:

- `id`: stable prompt identifier used in planning-job audit logs.
- `version`: integer version for deliberate prompt edits.
- `hash`: SHA-256 of the prompt body, excluding frontmatter.
- `owner`: package or subsystem responsible for the prompt.
- `last-edited`: review date.
- `applies-to-pipeline-versions`: pipeline versions that may use it.

`manifest.json` repeats the id, version, path, owner, pipeline versions, and body hash for every prompt. The package tests load the manifest, recompute each prompt hash, and fail on drift.

Regression fixtures live under `packages/fixtures/planning-prompt-regression/`. They do not call live models. Instead, they pin the input variables, expected output schema, and acceptable output examples for deterministic mock-model tests. Real-model regression can run later as a gated nightly suite, but normal CI remains subscription-free.

Consumers should load prompts through `@hoopoe/planning-prompts` and record the prompt hash in every planning job audit entry. If two planning jobs use different hashes for the same step, Diagnostics can surface the drift without parsing raw model output.
