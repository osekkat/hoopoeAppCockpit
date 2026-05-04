---
id: fresh-eyes-critique
version: 1
hash: sha256:013bea53d370969a35bf357995c947df978052d0450d2eec44cd0f12782be723
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Fresh Eyes Critique

Review {{synthesizedPlan}} for {{projectName}} as a brand-new planning session.

Look for hidden coupling, optimistic assumptions, missing tests, unsafe source-of-truth moves, unclear user experience, and phase ordering mistakes. Treat {{existingCodebaseBundle}} and {{userConstraints}} as the evidence base.

Return:

1. `blockingIssues`: issues that must be fixed before lock.
2. `importantIssues`: risks that should become beads or acceptance checks.
3. `questionsForAuthor`: only questions that change the plan.
4. `suggestedEdits`: concrete changes by section.
5. `confidence`: high, medium, or low with rationale.

Do not rewrite the whole plan. Produce actionable critique.
