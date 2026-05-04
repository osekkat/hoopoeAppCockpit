---
id: take-first-shot
version: 1
hash: sha256:a8efa7c6dd9835af3c61ad11122b01cf40d68888df5ddc46ba25fc73afee7204
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Take A First Shot

Draft an initial plan for {{projectName}} from {{roughIdea}} without asking follow-up questions.

Use {{existingCodebaseBundle}} and {{userConstraints}} as evidence, not decoration. State assumptions explicitly, keep them conservative, and design the first slice so a real implementation agent can start without inventing missing architecture.

Return:

1. `workingTitle`
2. `executiveThesis`
3. `phases`
4. `risksAndMitigations`
5. `openQuestions`
6. `acceptanceTests`

Do not propose direct provider API calls, arbitrary shell execution from the renderer, or a second source of truth for beads, Git, Agent Mail, NTM, or tool state.
