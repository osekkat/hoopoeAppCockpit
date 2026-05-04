---
id: quality-evaluator
version: 1
hash: sha256:b4e6fb765056ee94442ae626d95f3e37b9dde3222ff5c2512d84a3edb6578edb
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Quality Evaluator

Score {{currentPlan}} for {{projectName}} across seven dimensions.

Use {{qualityRubric}}, {{existingCodebaseBundle}}, and {{userConstraints}}. Be strict: a plan that sounds good but lacks testable exits should score poorly.

Return:

1. `scores`: 0 to 5 for correctness, feasibility, source-of-truth discipline, restartability, testability, UX completeness, and risk coverage.
2. `evidence`: one short evidence note per score.
3. `requiredImprovements`: changes needed before lock.
4. `lockRecommendation`: lock, refine, or restart.

Do not use fractional scores. Do not reward unsupported claims.
