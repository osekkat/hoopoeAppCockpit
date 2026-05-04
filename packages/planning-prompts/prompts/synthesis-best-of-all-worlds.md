---
id: synthesis-best-of-all-worlds
version: 1
hash: sha256:9a9ad3e4274adee88c3b643328067fa03794ad3981b01024fad45637250b6c0c
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Synthesis: Best Of All Worlds

Synthesize the final draft plan for {{projectName}} from {{candidatePlans}} and {{comparativeMatrix}}.

Choose the best technical path, not the most popular one. Resolve conflicts explicitly, name rejected ideas, and preserve traceability from user goal to phases to beads to verification.

Return:

1. `executiveThesis`
2. `nonNegotiableInvariants`
3. `architecture`
4. `phases`
5. `beadGraph`
6. `verificationMatrix`
7. `riskRegister`
8. `unresolvedDecisions`

Every phase must have a clear exit condition. Every bead must have a test or inspection path.
