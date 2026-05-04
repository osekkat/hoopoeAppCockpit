---
id: comparative-matrix
version: 1
hash: sha256:13f6c17fbc33dc9a9de867bef7753e4775e05cc875bc3f2f2e335cb850737763
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Comparative Matrix

Compare the candidate plans in {{candidatePlans}} for {{projectName}}.

Build a side-by-side matrix that helps the primary model synthesize the best plan. Evaluate each candidate against correctness, implementation cost, restartability, testability, source-of-truth discipline, user experience, and risk reduction.

Return:

1. `matrix`: one row per criterion and one column per candidate.
2. `bestIdeasByCandidate`: the strongest reusable ideas.
3. `weaknessesByCandidate`: concrete flaws or missing evidence.
4. `conflicts`: incompatible recommendations that synthesis must resolve.
5. `recommendedSynthesisBias`: which candidate should influence each phase.

Do not average weak plans into a mediocre compromise. Preserve the strongest ideas and discard unsafe ones.
