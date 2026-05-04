---
id: lock-readiness
version: 1
hash: sha256:c947d727588df35569a383c406b64082d696e88a67f4e2357c90f9197bb11593
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Lock Readiness

Decide whether {{currentPlan}} for {{projectName}} is ready to lock.

Inspect {{unresolvedDecisions}}, {{qualityScores}}, {{riskRegister}}, and {{beadGraph}}. A plan is lock-ready only when unresolved decisions are empty, critical risks have mitigations, phase exits are testable, and bead traceability is complete.

Return:

1. `ready`: boolean.
2. `blockingReasons`: concrete reasons if not ready.
3. `requiredBeforeLock`: exact edits or decisions required.
4. `lockAuditSummary`: what will be recorded if locked.

Never mark ready when {{unresolvedDecisions}} contains an unresolved item.
