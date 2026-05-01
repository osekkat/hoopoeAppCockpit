// Beads: DAG view, Kanban view, refinement rounds with the br-tool prompt

const BEAD_FONT = '-apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, sans-serif';
const BEAD_MONO = '"SF Mono", "JetBrains Mono", ui-monospace, Menlo, monospace';

// Authors (planner models that drafted each bead)
const BEAD_AUTHORS = {
  'gpt-pro': { name: 'Chat GPT Pro', dot: '#10A37F' },
  'opus':    { name: 'Opus 4.7', dot: '#C25A2E' },
  'gemini':  { name: 'Gemini 3.1 Pro', dot: '#4285F4' },
  'grok':    { name: 'Grok Heavy', dot: '#1D9BF0' },
};

// Status palette
const BEAD_STATUS = {
  queued:  { label: 'Queued',   dot: '#8E8E93' },
  running: { label: 'Running',  dot: '#0A84FF' },
  review:  { label: 'In review', dot: '#E9963F' },
  done:    { label: 'Done',     dot: '#30B66B' },
  blocked: { label: 'Blocked',  dot: '#E5484D' },
};

// 14 beads forming a clean DAG (kept tight for screen real-estate; deps form layers)
const BEADS = [
  { id: 'b01', title: 'Define plan goals & scope',           goal: 'Crisp goals, non-goals, success metrics.',       author: 'gpt-pro', status: 'done',    deps: [] },
  { id: 'b02', title: 'Gather & analyze requirements',       goal: 'Capture user workflows + constraints.',          author: 'opus',    status: 'done',    deps: ['b01'] },
  { id: 'b03', title: 'Research & evaluate technologies',    goal: 'Compare Electron / Tauri / native shells.',      author: 'gemini',  status: 'done',    deps: ['b01'] },
  { id: 'b04', title: 'Design system architecture',          goal: 'Modules, IPC contracts, data flow.',             author: 'opus',    status: 'running', deps: ['b02', 'b03'] },
  { id: 'b05', title: 'Design database schema',              goal: 'SQLite event log + snapshots.',                  author: 'opus',    status: 'running', deps: ['b04'] },
  { id: 'b06', title: 'Design API interfaces',               goal: 'IPC + sidecar protobufs.',                       author: 'gpt-pro', status: 'running', deps: ['b04'] },
  { id: 'b07', title: 'Design UI / UX prototype',            goal: 'macOS-native window, sidebar, beads view.',      author: 'gemini',  status: 'running', deps: ['b04'] },
  { id: 'b08', title: 'Implement database',                  goal: 'Migrations, queries, snapshot logic.',           author: 'opus',    status: 'review',  deps: ['b05'] },
  { id: 'b09', title: 'Implement backend services',          goal: 'Plan store, bead graph, scheduler.',             author: 'gpt-pro', status: 'review',  deps: ['b06', 'b08'] },
  { id: 'b10', title: 'Implement frontend',                  goal: 'React renderer, vibrancy, animations.',          author: 'gemini',  status: 'review',  deps: ['b07', 'b09'] },
  { id: 'b11', title: 'Write & run tests',                   goal: 'Unit + integration; logged thoroughly.',         author: 'opus',    status: 'queued',  deps: ['b08'] },
  { id: 'b12', title: 'Integrate system',                    goal: 'Wire frontend to backend end-to-end.',           author: 'gpt-pro', status: 'queued',  deps: ['b09', 'b10', 'b11'] },
  { id: 'b13', title: 'Perform code review',                 goal: 'Multi-model critique; gate on health metrics.',  author: 'grok',    status: 'queued',  deps: ['b10'] },
  { id: 'b14', title: 'Deploy to production',                goal: 'Notarized DMG + auto-update channel.',           author: 'gpt-pro', status: 'blocked', deps: ['b12', 'b13'] },
];

// Refinement rounds — each round runs the br-tool prompt and revises beads
const BR_PROMPT = `Reread AGENTS.md so it's still fresh in your mind. Check over each bead super carefully — are you sure it makes sense? Is it optimal? Could we change anything to make the system work better for users? If so, revise the beads. It's a lot easier and faster to operate in "plan space" before we start implementing these things!

DO NOT OVERSIMPLIFY THINGS! DO NOT LOSE ANY FEATURES OR FUNCTIONALITY!

Also, make sure that as part of these beads, we include comprehensive unit tests and e2e test scripts with great, detailed logging so we can be sure that everything is working perfectly after implementation.

Remember to ONLY use the \`br\` tool to create and modify the beads and to add the dependencies to beads. Use ultrathink.`;

const BEAD_ROUNDS = [
  {
    n: 1, model: 'gpt-pro', status: 'done', durationMs: 184000,
    summary: 'Initial pass after extraction.',
    actions: [
      { kind: 'create', count: 4, note: 'split monolithic "Worker pool" bead into spawner / reservation / token accounting' },
      { kind: 'modify', count: 9, note: 'tightened acceptance criteria across scheduling beads' },
      { kind: 'add-dep', count: 7, note: 'wired bead graph deps where lane crossings were missing' },
    ],
  },
  {
    n: 2, model: 'opus', status: 'done', durationMs: 211000,
    summary: 'Architectural critique; recovered missed test coverage.',
    actions: [
      { kind: 'create', count: 2, note: 'added "Crash + resume harness" and "Structured logging across IPC"' },
      { kind: 'modify', count: 12, note: 'reframed bead goals as user-observable outcomes, not internals' },
      { kind: 'add-dep', count: 4, note: 'made e2e bead block on logging + reservation' },
    ],
  },
  {
    n: 3, model: 'gemini', status: 'done', durationMs: 167000,
    summary: 'Edge-case sweep; clarified blocked beads.',
    actions: [
      { kind: 'create', count: 1, note: '"Cycle detection + diagnostics" bead' },
      { kind: 'modify', count: 6, note: 'split UI beads into DAG and Kanban as separate beads' },
      { kind: 'remove', count: 1, note: 'dropped redundant "polling-mode" bead in favor of event subscription' },
    ],
  },
  {
    n: 4, model: 'grok', status: 'active', durationMs: 0,
    summary: 'Stress-testing assumptions — currently auditing reservation TTLs and crash semantics.',
    actions: [
      { kind: 'create', count: 0, note: '' },
      { kind: 'modify', count: 3, note: 'in flight — tightening crash/resume bead' },
      { kind: 'add-dep', count: 1, note: 'in flight — adding dep on logging' },
    ],
  },
  {
    n: 5, model: 'gpt-pro', status: 'pending', durationMs: 0,
    summary: 'Final convergence pass — looking for incremental-only suggestions.',
    actions: [],
  },
];

// ─────────────────────────────────────────────────────────────
// BEADS PAGE — top-level view
// ─────────────────────────────────────────────────────────────
function BeadsView({ t, setView, onLaunchSwarm }) {
  const [layout, setLayout] = useState('dag'); // 'dag' | 'kanban'
  const [selected, setSelected] = useState(null);
  const [showRounds, setShowRounds] = useState(false);

  const counts = BEADS.reduce((acc, b) => {
    acc[b.status] = (acc[b.status] || 0) + 1;
    return acc;
  }, {});

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      {/* Header */}
      <div style={{
        padding: '20px 28px 14px',
        display: 'flex', alignItems: 'flex-end', gap: 16,
        borderBottom: `0.5px solid ${t.borderSoft}`,
        flexShrink: 0,
      }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 11, color: t.textDim, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Beads · plan.hybrid.md</div>
          <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, marginTop: 2 }}>
            {BEADS.length} beads <span style={{ color: t.textDim, fontWeight: 500, fontSize: 14, marginLeft: 6 }}>· 3 rounds of refinement complete · 1 in progress</span>
          </div>
          <div style={{ display: 'flex', gap: 14, marginTop: 8 }}>
            {['done', 'running', 'review', 'queued', 'blocked'].map((s) => (
              <div key={s} style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5, color: t.textDim }}>
                <span style={{ width: 6, height: 6, borderRadius: '50%', background: BEAD_STATUS[s].dot }} />
                <span style={{ fontWeight: 500 }}>{counts[s] || 0}</span>
                <span>{BEAD_STATUS[s].label.toLowerCase()}</span>
              </div>
            ))}
          </div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 8, alignItems: 'flex-end', flexShrink: 0 }}>
          <button
            onClick={() => onLaunchSwarm?.()}
            style={{ ...beadPillBtn(t, 'primary'), whiteSpace: 'nowrap' }}
          >
            <span>{Icon.spark(11, '#fff')}</span><span>Launch swarm</span>
          </button>

          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <button
              onClick={() => setShowRounds(true)}
              style={{ ...beadPillBtn(t, 'ghost'), whiteSpace: 'nowrap' }}
            >
              <span>{Icon.spark(11, t.crest)}</span><span>Rounds</span>
            </button>

            {/* Segmented layout switch */}
            <div style={{
              display: 'flex',
              background: t.panelAlt,
              border: `0.5px solid ${t.borderSoft}`,
              borderRadius: 7, padding: 2, height: 28,
            }}>
              {[
                { id: 'dag', label: 'DAG' },
                { id: 'kanban', label: 'Kanban' },
              ].map((o) => (
                <button key={o.id} onClick={() => setLayout(o.id)} style={{
                  padding: '0 12px',
                  borderRadius: 5, border: 'none',
                  background: layout === o.id ? t.panel : 'transparent',
                  color: layout === o.id ? t.text : t.textDim,
                  fontFamily: BEAD_FONT, fontSize: 11.5, fontWeight: 600,
                  cursor: 'pointer',
                  boxShadow: layout === o.id ? '0 0.5px 1.5px rgba(0,0,0,0.08)' : 'none',
                }}>{o.label}</button>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Body */}
      <div style={{ flex: 1, overflow: 'auto', position: 'relative' }}>
        {layout === 'dag'
          ? <DAGView t={t} beads={BEADS} selected={selected} onSelect={setSelected} />
          : <KanbanView t={t} beads={BEADS} selected={selected} onSelect={setSelected} />}
      </div>

      {/* Selected bead detail drawer (bottom) */}
      {selected && <BeadDetail t={t} bead={BEADS.find(b => b.id === selected)} onClose={() => setSelected(null)} />}

      {/* Refinement rounds modal */}
      {showRounds && <RoundsModal t={t} onClose={() => setShowRounds(false)} />}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// DAG VIEW — layered (topological) layout, top-down
// ─────────────────────────────────────────────────────────────
function DAGView({ t, beads, selected, onSelect }) {
  const NODE_W = 168, NODE_H = 60, GAP_X = 28, GAP_Y = 44;
  const PAD_X = 32, PAD_Y = 32;

  const beadById = React.useMemo(() => Object.fromEntries(beads.map(b => [b.id, b])), [beads]);

  // Compute layer (longest path from a root). Nodes with no deps → layer 0.
  const layers = React.useMemo(() => {
    const memo = {};
    const layerOf = (id) => {
      if (memo[id] != null) return memo[id];
      const b = beadById[id];
      if (!b || b.deps.length === 0) return memo[id] = 0;
      memo[id] = 1 + Math.max(...b.deps.map(layerOf));
      return memo[id];
    };
    beads.forEach(b => layerOf(b.id));
    return memo;
  }, [beads, beadById]);

  // Group beads by layer, ordered by parent x for stability
  const byLayer = React.useMemo(() => {
    const groups = {};
    beads.forEach(b => {
      const l = layers[b.id];
      (groups[l] = groups[l] || []).push(b);
    });
    return groups;
  }, [beads, layers]);

  const layerKeys = Object.keys(byLayer).map(Number).sort((a, b) => a - b);

  // Position: center each layer's row horizontally; track parent x to order siblings sensibly.
  const positions = React.useMemo(() => {
    const pos = {};
    // First pass: place layer 0 in source order
    layerKeys.forEach((l) => {
      let row = byLayer[l];
      if (l > 0) {
        // Order by average parent x for nicer layouts
        row = [...row].sort((a, b) => {
          const ax = a.deps.length ? a.deps.reduce((s, d) => s + (pos[d]?.x || 0), 0) / a.deps.length : 0;
          const bx = b.deps.length ? b.deps.reduce((s, d) => s + (pos[d]?.x || 0), 0) / b.deps.length : 0;
          return ax - bx;
        });
      }
      const totalW = row.length * NODE_W + (row.length - 1) * GAP_X;
      const startX = -totalW / 2; // we'll shift everything later
      row.forEach((b, i) => {
        pos[b.id] = {
          x: startX + i * (NODE_W + GAP_X),
          y: l * (NODE_H + GAP_Y),
        };
      });
    });
    // Shift so min x is PAD_X
    let minX = Infinity, maxX = -Infinity, maxY = 0;
    Object.values(pos).forEach(p => {
      minX = Math.min(minX, p.x);
      maxX = Math.max(maxX, p.x + NODE_W);
      maxY = Math.max(maxY, p.y + NODE_H);
    });
    const shift = PAD_X - minX;
    Object.values(pos).forEach(p => { p.x += shift; });
    const W = (maxX - minX) + PAD_X * 2;
    const H = maxY + PAD_Y * 2;
    return { pos, W, H };
  }, [byLayer, layerKeys]);

  const { pos, W, H } = positions;

  return (
    <div style={{ padding: PAD_Y, minWidth: W + PAD_X, display: 'flex', justifyContent: 'center' }}>
      <div style={{ position: 'relative', width: W, height: H }}>
        {/* Edges */}
        <svg width={W} height={H} style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
          <defs>
            <marker id="dag-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto">
              <path d="M 0 0 L 9 5 L 0 10 z" fill={t.textMute} />
            </marker>
            <marker id="dag-arrow-sel" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5" markerHeight="5" orient="auto">
              <path d="M 0 0 L 9 5 L 0 10 z" fill={t.crest} />
            </marker>
          </defs>
          {beads.map((b) =>
            b.deps.map((depId) => {
              const dep = beadById[depId];
              if (!dep) return null;
              const a = pos[dep.id], c = pos[b.id];
              const x1 = a.x + NODE_W / 2;
              const y1 = a.y + NODE_H;
              const x2 = c.x + NODE_W / 2;
              const y2 = c.y - 2;
              // Orthogonal-ish: vertical out, vertical in; if x differs, jog at midpoint Y
              const midY = y1 + (y2 - y1) / 2;
              const path = x1 === x2
                ? `M ${x1} ${y1} L ${x2} ${y2}`
                : `M ${x1} ${y1} L ${x1} ${midY} L ${x2} ${midY} L ${x2} ${y2}`;
              const isSel = selected && (selected === b.id || selected === dep.id);
              return (
                <path
                  key={`${depId}-${b.id}`}
                  d={path}
                  fill="none"
                  stroke={isSel ? t.crest : t.borderSoft}
                  strokeWidth={isSel ? 1.4 : 1}
                  markerEnd={`url(#${isSel ? 'dag-arrow-sel' : 'dag-arrow'})`}
                />
              );
            })
          )}
        </svg>

        {/* Nodes */}
        {beads.map((b) => {
          const p = pos[b.id];
          const isSel = selected === b.id;
          const author = BEAD_AUTHORS[b.author];
          return (
            <div
              key={b.id}
              onClick={() => onSelect(b.id)}
              style={{
                position: 'absolute', left: p.x, top: p.y,
                width: NODE_W, height: NODE_H,
                background: t.panel,
                border: `1px solid ${isSel ? t.crest : t.borderSoft}`,
                borderRadius: 8,
                padding: '8px 10px 8px 10px',
                cursor: 'pointer',
                boxShadow: isSel ? `0 0 0 3px ${t.crestSoft}, 0 1px 3px rgba(0,0,0,0.06)` : '0 1px 2px rgba(0,0,0,0.05)',
                display: 'flex', flexDirection: 'column', gap: 4,
                transition: 'box-shadow 120ms ease, border-color 120ms ease',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
                <span style={{ width: 7, height: 7, borderRadius: '50%', background: BEAD_STATUS[b.status].dot, flexShrink: 0 }} />
                <span style={{ fontFamily: BEAD_MONO, fontSize: 10, color: t.textMute, fontWeight: 600 }}>{b.id}</span>
                <span style={{ flex: 1 }} />
                <span title={`Author: ${author.name}`} style={{
                  width: 6, height: 6, borderRadius: '50%',
                  background: author.dot,
                }} />
                <span style={{
                  marginLeft: 2, color: t.textMute, fontSize: 13, lineHeight: 0.6,
                  cursor: 'pointer', userSelect: 'none',
                }}>⋮</span>
              </div>
              <div style={{
                fontSize: 11.5, fontWeight: 600, color: t.text,
                lineHeight: 1.3,
                display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical',
                overflow: 'hidden',
              }}>{b.title}</div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// KANBAN VIEW
// ─────────────────────────────────────────────────────────────
function KanbanView({ t, beads, selected, onSelect }) {
  const cols = ['queued', 'running', 'review', 'done', 'blocked'];
  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: `repeat(${cols.length}, minmax(180px, 1fr))`,
      gap: 12, padding: 24, alignItems: 'start',
    }}>
      {cols.map((c) => {
        const list = beads.filter(b => b.status === c);
        return (
          <div key={c} style={{
            background: t.panelAlt, borderRadius: 10,
            border: `0.5px solid ${t.borderSoft}`,
            display: 'flex', flexDirection: 'column',
            minHeight: 200,
          }}>
            <div style={{
              padding: '10px 12px',
              display: 'flex', alignItems: 'center', gap: 8,
              borderBottom: `0.5px solid ${t.borderSoft}`,
            }}>
              <span style={{ width: 7, height: 7, borderRadius: '50%', background: BEAD_STATUS[c].dot }} />
              <span style={{ fontSize: 11.5, fontWeight: 700, color: t.text }}>{BEAD_STATUS[c].label}</span>
              <span style={{ flex: 1 }} />
              <span style={{ fontSize: 10.5, color: t.textMute, fontVariantNumeric: 'tabular-nums' }}>{list.length}</span>
            </div>
            <div style={{ padding: 8, display: 'flex', flexDirection: 'column', gap: 6 }}>
              {list.map((b) => (
                <div
                  key={b.id}
                  onClick={() => onSelect(b.id)}
                  style={{
                    background: t.panel,
                    border: `1px solid ${selected === b.id ? t.crest : t.borderSoft}`,
                    borderRadius: 7,
                    padding: '8px 9px',
                    cursor: 'pointer',
                    boxShadow: selected === b.id ? `0 0 0 3px ${t.crestSoft}` : 'none',
                  }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 5, marginBottom: 3 }}>
                    <span style={{ fontFamily: BEAD_MONO, fontSize: 9.5, color: t.textMute, fontWeight: 600 }}>{b.id}</span>
                    <span style={{ flex: 1 }} />
                    <span title={BEAD_AUTHORS[b.author].name} style={{
                      width: 5, height: 5, borderRadius: '50%',
                      background: BEAD_AUTHORS[b.author].dot,
                    }} />
                  </div>
                  <div style={{ fontSize: 11.5, fontWeight: 600, color: t.text, lineHeight: 1.3 }}>{b.title}</div>
                  {b.deps.length > 0 && (
                    <div style={{ marginTop: 5, fontSize: 10, color: t.textMute, fontFamily: BEAD_MONO }}>
                      ← {b.deps.join(', ')}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </div>
        );
      })}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// BEAD DETAIL DRAWER
// ─────────────────────────────────────────────────────────────
function BeadDetail({ t, bead, onClose }) {
  if (!bead) return null;
  const author = BEAD_AUTHORS[bead.author];
  const status = BEAD_STATUS[bead.status];
  const dependents = BEADS.filter(b => b.deps.includes(bead.id));

  return (
    <div style={{
      position: 'absolute', left: 0, right: 0, bottom: 0,
      background: t.panel,
      borderTop: `0.5px solid ${t.border}`,
      boxShadow: '0 -8px 24px rgba(0,0,0,0.10)',
      padding: '14px 24px',
      display: 'flex', gap: 18, alignItems: 'flex-start',
    }}>
      <div style={{ flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
          <span style={{ fontFamily: BEAD_MONO, fontSize: 11, color: t.textMute, fontWeight: 600 }}>{bead.id}</span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '2px 7px', borderRadius: 4, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}` }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: status.dot }} />
            <span style={{ fontSize: 10.5, fontWeight: 600, color: t.textDim }}>{status.label}</span>
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '2px 7px', borderRadius: 4, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}` }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', background: author.dot }} />
            <span style={{ fontSize: 10.5, fontWeight: 600, color: t.textDim }}>authored by {author.name}</span>
          </span>
        </div>
        <div style={{ fontSize: 15, fontWeight: 700, letterSpacing: -0.2 }}>{bead.title}</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 3 }}>{bead.goal}</div>
        <div style={{ display: 'flex', gap: 24, marginTop: 10 }}>
          <div>
            <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Depends on</div>
            <div style={{ display: 'flex', gap: 5, marginTop: 4, flexWrap: 'wrap' }}>
              {bead.deps.length === 0 && <span style={{ fontSize: 11.5, color: t.textMute, fontStyle: 'italic' }}>nothing</span>}
              {bead.deps.map(d => (
                <span key={d} style={{ fontFamily: BEAD_MONO, fontSize: 10.5, padding: '2px 6px', borderRadius: 4, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`, color: t.textDim }}>{d}</span>
              ))}
            </div>
          </div>
          <div>
            <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Unblocks</div>
            <div style={{ display: 'flex', gap: 5, marginTop: 4, flexWrap: 'wrap' }}>
              {dependents.length === 0 && <span style={{ fontSize: 11.5, color: t.textMute, fontStyle: 'italic' }}>nothing</span>}
              {dependents.map(d => (
                <span key={d.id} style={{ fontFamily: BEAD_MONO, fontSize: 10.5, padding: '2px 6px', borderRadius: 4, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`, color: t.textDim }}>{d.id}</span>
              ))}
            </div>
          </div>
        </div>
      </div>
      <button onClick={onClose} style={{
        background: 'transparent', border: 'none', color: t.textDim, cursor: 'pointer',
        fontSize: 16, padding: 4,
      }}>✕</button>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// REFINEMENT ROUNDS MODAL
// ─────────────────────────────────────────────────────────────
function RoundsModal({ t, onClose }) {
  const [activeRound, setActiveRound] = useState(4);
  const round = BEAD_ROUNDS.find(r => r.n === activeRound);

  return (
    <div style={{
      position: 'absolute', inset: 0,
      background: 'rgba(0,0,0,0.32)',
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      zIndex: 100, padding: 32,
    }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{
        background: t.panel,
        borderRadius: 12,
        boxShadow: '0 30px 80px rgba(0,0,0,0.4)',
        width: 880, maxHeight: '88%',
        display: 'flex', flexDirection: 'column',
        overflow: 'hidden',
        border: `0.5px solid ${t.border}`,
      }}>
        {/* Header */}
        <div style={{
          padding: '14px 18px',
          borderBottom: `0.5px solid ${t.borderSoft}`,
          display: 'flex', alignItems: 'center', gap: 12,
          background: t.panelAlt,
        }}>
          <div>
            <div style={{ fontSize: 13.5, fontWeight: 700, letterSpacing: -0.2 }}>Bead refinement rounds</div>
            <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 1 }}>
              Each round, a planner model rereads AGENTS.md and revises beads via the <span style={{ fontFamily: BEAD_MONO, color: t.crest }}>br</span> tool.
            </div>
          </div>
          <div style={{ flex: 1 }} />
          <button onClick={onClose} style={{
            background: 'transparent', border: 'none', color: t.textDim, cursor: 'pointer', fontSize: 16,
          }}>✕</button>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: '220px 1fr', flex: 1, minHeight: 0 }}>
          {/* Round list */}
          <div style={{ borderRight: `0.5px solid ${t.borderSoft}`, padding: 8, overflow: 'auto', background: t.panelAlt }}>
            {BEAD_ROUNDS.map((r) => {
              const author = BEAD_AUTHORS[r.model];
              const sel = activeRound === r.n;
              return (
                <div key={r.n} onClick={() => setActiveRound(r.n)} style={{
                  padding: '8px 10px',
                  borderRadius: 6,
                  background: sel ? t.panel : 'transparent',
                  border: sel ? `0.5px solid ${t.borderSoft}` : '0.5px solid transparent',
                  cursor: 'pointer',
                  marginBottom: 3,
                  display: 'flex', flexDirection: 'column', gap: 3,
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span style={{ fontSize: 10.5, fontWeight: 700, color: t.textMute }}>ROUND {r.n}</span>
                    <span style={{ flex: 1 }} />
                    <RoundStatusPill t={t} status={r.status} />
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
                    <span style={{ width: 6, height: 6, borderRadius: '50%', background: author.dot }} />
                    <span style={{ fontSize: 11.5, fontWeight: 600, color: t.text }}>{author.name}</span>
                  </div>
                </div>
              );
            })}
          </div>

          {/* Round detail */}
          <div style={{ padding: 18, overflow: 'auto' }}>
            <RoundDetail t={t} round={round} />
          </div>
        </div>
      </div>
    </div>
  );
}

function RoundDetail({ t, round }) {
  if (!round) return null;
  const author = BEAD_AUTHORS[round.model];
  const totalChanges = round.actions.reduce((s, a) => s + a.count, 0);
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <span style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase' }}>Round {round.n}</span>
        <span style={{ display: 'flex', alignItems: 'center', gap: 5, padding: '2px 7px', borderRadius: 4, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}` }}>
          <span style={{ width: 6, height: 6, borderRadius: '50%', background: author.dot }} />
          <span style={{ fontSize: 10.5, fontWeight: 600, color: t.textDim }}>{author.name}</span>
        </span>
        <RoundStatusPill t={t} status={round.status} />
        <span style={{ flex: 1 }} />
        {round.durationMs > 0 && (
          <span style={{ fontSize: 11, color: t.textMute, fontFamily: BEAD_MONO }}>
            {(round.durationMs / 1000 / 60).toFixed(1)} min · {totalChanges} changes
          </span>
        )}
      </div>

      <div style={{ fontSize: 13, fontWeight: 600, color: t.text, letterSpacing: -0.1 }}>{round.summary}</div>

      {/* The prompt */}
      <div>
        <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase', marginBottom: 6 }}>Prompt sent to {author.name}</div>
        <pre style={{
          background: t.panelAlt,
          border: `0.5px solid ${t.borderSoft}`,
          borderRadius: 8,
          padding: '12px 14px',
          fontSize: 11.5, lineHeight: 1.55,
          color: t.text,
          fontFamily: BEAD_MONO,
          whiteSpace: 'pre-wrap',
          margin: 0,
          maxHeight: 200,
          overflow: 'auto',
        }}>{BR_PROMPT}</pre>
      </div>

      {/* Actions taken */}
      <div>
        <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase', marginBottom: 6 }}>
          {round.status === 'active' ? 'Actions in flight' : round.status === 'pending' ? 'Awaiting kickoff' : 'Actions taken via br tool'}
        </div>
        {round.actions.length === 0 ? (
          <div style={{ fontSize: 12, color: t.textMute, fontStyle: 'italic' }}>This round has not yet run.</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            {round.actions.map((a, i) => (
              <div key={i} style={{
                display: 'flex', alignItems: 'flex-start', gap: 10,
                padding: '8px 10px',
                background: t.panelAlt,
                border: `0.5px solid ${t.borderSoft}`,
                borderRadius: 6,
              }}>
                <ActionKindPill t={t} kind={a.kind} count={a.count} />
                <span style={{ fontSize: 12, color: t.text, flex: 1, lineHeight: 1.45 }}>{a.note || <span style={{ color: t.textMute, fontStyle: 'italic' }}>—</span>}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function RoundStatusPill({ t, status }) {
  const map = {
    done:    { bg: 'rgba(48,182,107,0.15)', fg: '#30B66B', label: 'Done' },
    active:  { bg: 'rgba(229,150,63,0.15)', fg: '#E9963F', label: 'Active' },
    pending: { bg: t.panelAlt, fg: t.textMute, label: 'Pending' },
  };
  const m = map[status] || map.pending;
  return (
    <span style={{
      padding: '2px 7px', borderRadius: 4,
      background: m.bg, color: m.fg,
      fontSize: 10, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase',
    }}>{m.label}</span>
  );
}

function ActionKindPill({ t, kind, count }) {
  const map = {
    'create':  { bg: 'rgba(48,182,107,0.15)', fg: '#30B66B', label: 'create' },
    'modify':  { bg: 'rgba(10,132,255,0.15)', fg: '#0A84FF', label: 'modify' },
    'remove':  { bg: 'rgba(229,72,77,0.15)',  fg: '#E5484D', label: 'remove' },
    'add-dep': { bg: 'rgba(229,130,83,0.15)', fg: '#E58253', label: 'add dep' },
  };
  const m = map[kind] || { bg: t.panelAlt, fg: t.textDim, label: kind };
  return (
    <span style={{
      padding: '2px 7px', borderRadius: 4,
      background: m.bg, color: m.fg,
      fontSize: 10, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase',
      fontFamily: BEAD_MONO,
      whiteSpace: 'nowrap', flexShrink: 0,
    }}>br {m.label} ×{count}</span>
  );
}

// pill button helper
function beadPillBtn(t, kind = 'ghost') {
  const base = {
    height: 28, padding: '0 12px', borderRadius: 7,
    fontFamily: BEAD_FONT, fontSize: 12, fontWeight: 600,
    display: 'inline-flex', alignItems: 'center', gap: 6,
    cursor: 'pointer', border: 'none',
  };
  if (kind === 'primary') {
    return { ...base, background: t.crest, color: '#fff' };
  }
  return {
    ...base,
    background: t.panel,
    color: t.text,
    border: `0.5px solid ${t.borderSoft}`,
  };
}

Object.assign(window, {
  BeadsView,
});
