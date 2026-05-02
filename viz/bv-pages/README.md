# Project Dashboard

## 📊 Executive Summary

**207** total issues | **0%** complete | **8** ready to work | **199** blocked

⚠️ **Health Warning:** More issues are blocked than actionable. Focus on clearing blockers.

## 🎯 Top Priorities

The graph analysis identified these as the highest-impact items to work on:

### 1. Cross-cutting: packages/schemas — OpenAPI + TS client + Go types
**ID:** `hp-r3i` | **Impact Score:** 0.60 | **Unblocks:** 8 issues

**Why this matters:**
- 🎯 Completing this unblocks 8 downstream issues (hp-14zt, hp-1ry, +6 more)
- 📊 High centrality in dependency graph (PageRank: 100%)
- ⚡ Low effort, high impact - good starting point
- ✅ Currently unclaimed - available for work
- 🚨 High priority (P1) - prioritize this work

### 2. Phase 1: Clone & pin t3code, initialize monorepo (Turbo + Bun + workspaces)
**ID:** `hp-xru` | **Impact Score:** 0.41 | **Unblocks:** 6 issues

**Why this matters:**
- 🎯 Completing this unblocks 6 downstream issues (hp-15s, hp-191, +4 more)
- 📊 High centrality in dependency graph (PageRank: 58%)
- ⚡ Low effort, high impact - good starting point
- ✅ Currently unclaimed - available for work
- 🚨 High priority (P0) - prioritize this work

### 3. Cross-cutting: capability registry + degraded-mode contract
**ID:** `hp-r33` | **Impact Score:** 0.39 | **Unblocks:** 4 issues

**Why this matters:**
- 🎯 Completing this unblocks 4 downstream issues (hp-6bj, hp-6e2, +2 more)
- 📊 High centrality in dependency graph (PageRank: 46%)
- ⚡ Low effort, high impact - good starting point
- ✅ Currently unclaimed - available for work
- 🚨 High priority (P1) - prioritize this work

## 🚧 Critical Bottlenecks

These issues are blocking the most downstream work. Clearing them has outsized impact:

| Issue | Title | Unblocks | Status |
|-------|-------|----------|--------|
| `hp-r3i` | Cross-cutting: packages/schemas — Ope... | **8** issues | Ready |
| `hp-xru` | Phase 1: Clone & pin t3code, initiali... | **6** issues | Ready |
| `hp-r33` | Cross-cutting: capability registry + ... | **4** issues | Ready |
| `hp-38d` | Phase 2: Daemon HTTP/WS scaffolding (... | **3** issues | Blocked by 1 |
| `hp-zir` | Phase 1: Decompose t3code main.ts int... | **3** issues | Blocked by 1 |

## 📈 Graph Analysis

- **Dependency Density:** 0.022 (🟢 Healthy) — Issues are well-isolated and can be parallelized
- **Graph Size:** 207 issues with 929 dependencies
- **Cycles:** None detected ✓

## 🏃 Quick Wins

Low-effort items that clear the path forward:

- **hp-xru**: Phase 1: Clone & pin t3code, initialize monorepo (Turbo + Bun + workspaces) (unblocks 6)
  - *Unblocks 6 items, high priority*
- **hp-r3i**: Cross-cutting: packages/schemas — OpenAPI + TS client + Go types (unblocks 8)
  - *Unblocks 8 items, high priority*
- **hp-38d**: Phase 2: Daemon HTTP/WS scaffolding (chi/echo + gorilla/nhooyr WS, /health, /v1/version, /v1/jobs) (unblocks 3)
  - *Unblocks 3 items, high priority*
- **hp-5s4**: Phase 10: Tending scheduler infrastructure (cron + interval + event + on-demand; lease-based; durable; restartable) (unblocks 2)
  - *Unblocks 2 items, high priority*
- **hp-r7i**: Phase 0: Provision research-spike VPS with ACFS (unblocks 2)
  - *Unblocks 2 items, high priority*

## 📋 Status Summary

**By Priority:** P0: 77 | P1: 105 | P2: 21 | P3: 4

**By Type:** epic: 24 | task: 183

---

*Generated May 2, 2026 at 12:47 PM +01 by [bv](https://github.com/Dicklesworthstone/beads_viewer)*

