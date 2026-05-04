---
id: refinement-round-N
version: 1
hash: sha256:2769efd0ec722e4b2b7b7b4c8a05cff48c6f192b92e9698ecd4ff5b358d3defe
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Refinement Round N

Refine {{currentPlan}} for {{projectName}} in round {{roundNumber}}.

Use {{freshEyesCritique}}, {{previousRoundNotes}}, and {{unresolvedDecisions}}. Apply only changes that improve correctness, sequencing, testability, or user value. Keep the plan internally consistent after edits.

Return:

1. `updatedPlan`
2. `changeLog`: section-by-section edits and reasons.
3. `resolvedDecisions`
4. `remainingUnresolvedDecisions`
5. `newBeadAdjustments`
6. `regressionRisks`

If a critique item is rejected, explain why and what evidence supports the rejection.
