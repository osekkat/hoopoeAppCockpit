---
id: clarifying-questions
version: 1
hash: sha256:10354830f5ae38b3fcdb04f18a308c5b3b610d47c2563d60b086420121ddfe35
owner: planning-pipeline
last-edited: 2026-05-04
applies-to-pipeline-versions:
  - phase5.v1
---

# Clarifying Questions

You are preparing a Hoopoe planning run for {{projectName}}.

Ask the smallest useful set of clarifying questions before any candidate plan is drafted. Prefer questions that change architecture, risk, scope, release sequencing, or acceptance tests. Do not ask about details already present in {{roughIdea}}, {{existingCodebaseBundle}}, or {{userConstraints}}.

Return:

1. `questions`: 3 to 7 concrete questions.
2. `assumptionsIfUnanswered`: conservative assumptions to use if the user skips the interview.
3. `blockedUntilAnswered`: true only when drafting would be irresponsible without the answers.

Keep the questions neutral and answerable in one or two sentences each.
