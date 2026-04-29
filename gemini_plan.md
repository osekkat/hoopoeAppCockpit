# Hoopoe: Agentic Coding Flywheel Implementation Plan

This is an incredibly ambitious and well-architected vision. By offloading the heavy compute, token generation, file-system I/O, and LLM orchestration to a remote VPS—while keeping the Electron desktop app as a sleek, reactive control plane—you perfectly solve the UX friction of the Agentic Coding Flywheel.

Based on your workflow requirements, the provided CLI tools (`ntm`, `br`, `bv`, Agent Mail), and your high-fidelity UI mockups, here is a comprehensive, end-to-end implementation plan to build **Hoopoe**.

---

### 1. System Architecture & Tech Stack

To ensure Hoopoe remains ultra-fast and doesn't lag while tracking 12+ agents, we will use a **Client-Tunnel-Daemon** architecture.

*   **Frontend (Electron Renderer):** React 18+ (TypeScript), Tailwind CSS, `shadcn/ui` (for the native macOS dark/light aesthetic), `reactflow` (for the Bead DAG), `@hello-pangea/dnd` (for Kanban), and `recharts` (for Swarm CPU/RAM sparklines).
*   **Bridge (Electron Main):** Uses the Node.js `ssh2` library to establish a persistent, multiplexed SSH connection. It manages local API key storage securely via `keytar` (macOS Keychain).
*   **The VPS Daemon (Hoopoe-d):** Instead of making thousands of raw SSH bash calls per minute, Hoopoe will silently deploy a lightweight Node.js/Go daemon to the VPS during setup. This daemon runs locally on the VPS, watches the `br` SQLite DB, reads `ntm`/`tmux` panes, calculates CPU metrics via `ps`, and streams structured JSON state back to the Electron app via WebSockets over an SSH-forwarded port.

---

### Phase 1: Infrastructure & VPS Onboarding (Weeks 1-2)
**Goal:** Abstract away the CLI bootstrapping so the user goes from "download" to a fully armed AI software factory in minutes.

1.  **Connection Wizard:** A sleek UI to input the VPS IP, Username (root/ubuntu), and SSH key pair.
2.  **Automated Provisioning:** 
    *   Hoopoe establishes the SSH connection and executes the flywheel setup: `curl -fsSL https://raw.githubusercontent.com/Dicklesworthstone/agentic_coding_flywheel_setup/main/setup.sh | bash`
    *   The UI features an embedded `xterm.js` terminal tailing the installation logs, accompanied by a visual checklist (verifying `ntm`, `br`, `bv`, bun, and agent CLIs are installed).
3.  **Project Workspace Manager:** UI to `git clone` an existing repository or `git init` a new one. The app prompts the user for their Anthropic/OpenAI/Gemini API keys and injects them securely into the remote VPS `.env` file.

---

### Phase 2: Stage 1 — Planning Engine (Weeks 3-4)
**Goal:** Implement the multi-LLM "Hybrid Best of All Worlds" planning workflow (Mockup 2).

1.  **State Management:** Markdown files in the `plans/` directory are tracked via YAML frontmatter (`status: DRAFT | PLANNING | CONVERTED`).
2.  **The Multi-Agent Synthesis Pipeline:**
    *   User inputs a rough idea into Hoopoe.
    *   The Electron Main process makes concurrent API calls to Claude 3.5 Sonnet, GPT-4o, and Gemini 1.5 Pro.
    *   The three disparate architectural outputs are fed into a Frontier model (e.g., Claude 3.7 Sonnet / o3-mini) with a synthesis prompt to extract the best choices and generate the **Master Plan**.
3.  **Refinement UI:** A split-pane Monaco Editor allows the user to read the `plan.md`, with an adjacent chat window to ask the Deep Think model to iterate on specific sections before finalizing.

---

### Phase 3: Stage 2 — Bead Curation & Graphing (Weeks 5-6)
**Goal:** Convert plans to trackable work units and visualize the dependency critical path (Mockup 3).

1.  **Conversion:** A "Convert to Beads" button triggers the VPS Orchestrator to run the `/beads-workflow` skill and `br create`, parsing the `plan.md` into the `.beads` SQLite database.
2.  **Graph Extraction (`bv` Integration):** Hoopoe runs `bv --robot-triage` and `bv --robot-graph` on the VPS. This outputs deterministic JSON containing PageRank, Betweenness Centrality (bottlenecks), and exact dependency edges.
3.  **Visualization Dashboard:**
    *   **Kanban + DAG:** Feed the nodes and edges into `React Flow`. Map the nodes into column swimlanes based on their status (Backlog, Ready, In Progress, Review, Closed). 
    *   **State Sync:** Dragging a node to a new column fires an API call to the VPS daemon to execute `br update <id> --status <new>`.
4.  **Curation Tracker:** Add a background evaluation loop where an LLM rates the generated beads on a 1-10 scale for testability and clarity, flagging ambiguous beads for user review.

---

### Phase 4: Stages 3 & 4 — Swarm Orchestration & Activity (Weeks 7-8)
**Goal:** Launch the multi-agent swarm and render the mission-control dashboard (Mockups 4 & 5).

1.  **Swarm Launch & The Orchestrator:**
    *   The user clicks "Launch Swarm."
    *   Hoopoe injects your exact **Universal Starting Prompt** (instructing the use of `/ntm`, `/vibing-with-ntm`, `rch`, and the `looper` tool) into a master `claude` orchestrator running in a detached `tmux` pane.
2.  **Activity Feed (Agent Mail):**
    *   The VPS daemon uses `chokidar` (or SQLite watchers) to monitor the MCP Agent Mail database.
    *   Hoopoe streams this to the UI, formatting file lock claims (`Reserving src/search/embed.ts`), urgent broadcasts, and code-review handoffs into the chronological timeline.
3.  **Swarm Telemetry Grid:**
    *   **Process Mapping:** The VPS daemon queries `ntm status` and `tmux ls`, maps session PIDs to system processes, and streams CPU/RAM usage to power the Recharts sparklines.
    *   **Terminal Tails:** The daemon runs `tmux capture-pane -p` to grab the last 5 lines of each agent's active thought process, rendering it in the UI's black terminal boxes.
    *   **Token Velocity:** The daemon parses the local LLM logs/cache to aggregate token usage and calculate the dynamic dollar spend.

---

### Phase 5: Stage 5 — Code Health & The Review Loop (Weeks 9-10)
**Goal:** Automate the "rounds and rounds until reviews come back clean" testing cycle (Mockup 6).

1.  **Automated Tooling Execution:**
    *   As the orchestrator uses `/vibing-with-ntm` and triggers tests via `/rch`, Hoopoe parses the resulting `coverage-summary.json` (or `lcov.info`).
    *   The VPS daemon periodically runs an AST parser (like `scc` or `radon`) to calculate the Cyclomatic Complexity (CX) per file. Git churn is extracted via `git log`.
2.  **Code Health Dashboard:**
    *   Render the table matching your mockup, sorting files by LOC, CX, Coverage %, and Churn.
3.  **The "Fresh Eyes" Auto-Trigger:**
    *   Establish visual "Hot Spots" (e.g., Complexity > 20 and Coverage < 60%).
    *   Monitor the **Saturation Point**: Hoopoe plots bugs found vs. tokens spent. When the curve flattens, Hoopoe highlights a UI recommendation to transition to deep-skills (`/security-audit`, `/mock-code-finder`, `/deadlock-finder-and-fixer`), which auto-generates new beads and feeds them back into Stage 2.

---

### Critical Technical Considerations

*   **SSH Resilience:** Laptops go to sleep. Since all agents are safely compartmentalized in `tmux` panes by NTM on the remote VPS, an SSH drop is harmless. Hoopoe's SSH manager must simply auto-reconnect, fetch the latest state from the VPS daemon, and instantly re-hydrate the UI without interrupting the swarm.
*   **Handling Raw Tmux ANSI:** Reading interactive LLM CLIs is messy due to colors and progress bars. The VPS daemon must run the `tmux` output through an ANSI stripper (like the `strip-ansi` npm package) before sending it to the React UI to ensure clean, readable terminal tails.
*   **Rate Limit Circuit Breakers:** The swarm will rapidly hit API limits. The daemon should parse agent `stderr` for `429 Too Many Requests`. When detected, Hoopoe turns the agent's card orange ("Rate limited"). If a bead is marked "In Progress" but hasn't been modified in 15 minutes, Hoopoe visually flags it as "Stalled" to prompt the orchestrator's triage loop.
