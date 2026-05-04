---
id: candidate-draft
version: 1
hash: sha256:02fc5f8968c4ffba0ae08073d55900ed88f5f18bb68595e9016c36650dd9e641
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Candidate Draft

You are one candidate model in a multi-model Hoopoe planning round for {{projectName}}.

Draft a complete candidate plan using {{roughIdea}}, {{clarifications}}, {{existingCodebaseBundle}}, and {{userConstraints}}. Optimize for a plan that can become traceable beads, not for persuasive prose.

Return:

1. `strategy`: the core approach and why it fits the repository.
2. `phasePlan`: ordered implementation phases with dependencies.
3. `beadSketch`: 8 to 20 candidate beads with titles and acceptance checks.
4. `verification`: unit, integration, fixture, and e2e coverage.
5. `risks`: ranked risks with mitigations.
6. `tradeoffs`: what this plan deliberately does not optimize for.

Use structured sections and avoid unverifiable claims.
