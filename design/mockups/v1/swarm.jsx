// Swarm tab: Claude Code worker fleet executing beads end-to-end (commit + push).

const SWARM_FONT = '-apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, sans-serif';
const SWARM_MONO = '"SF Mono", "JetBrains Mono", ui-monospace, Menlo, monospace';

// ─────────────────────────────────────────────────────────────
// Models the user can pick for workers
// ─────────────────────────────────────────────────────────────
const SWARM_MODELS = [
  { id: 'gpt-5-5-xhigh',   name: 'GPT-5.5 xhigh',     short: 'GPT-5.5 xh', dot: '#10A37F', cost: 0.020 },
  { id: 'claude-opus-4-7-max', name: 'Claude Opus 4.7 max', short: 'Opus 4.7 max', dot: '#C25A2E', cost: 0.018 },
  { id: 'gemini-3-1-pro',  name: 'Gemini 3.1 Pro',    short: 'Gemini 3.1 Pro', dot: '#4285F4', cost: 0.012 },
];

// ─────────────────────────────────────────────────────────────
// Initial worker fleet (after launch)
// Each worker has: id, name, model, status, currentBead, log[], tokens, cost, startedAt
// ─────────────────────────────────────────────────────────────
const INITIAL_WORKERS = [
  {
    id: 'w1', name: 'forager-01', model: 'claude-opus-4-7-max', status: 'working',
    currentBead: 'b04', startedAt: 92,
    tokens: 142400, cost: 2.13,
    log: [
      { t: 92, kind: 'tool', text: 'br get b04' },
      { t: 88, kind: 'read', text: 'plan.hybrid.md (lines 412–698)' },
      { t: 70, kind: 'read', text: 'core/plan-store/mod.rs' },
      { t: 52, kind: 'edit', text: 'src/architecture/ipc.md +47 −12' },
      { t: 28, kind: 'tool', text: 'cargo check --workspace' },
      { t: 12, kind: 'think', text: 'Reconciling sidecar protobuf surface with bead-graph IPC...' },
      { t: 3,  kind: 'edit', text: 'src/architecture/ipc.md +18 −0' },
    ],
  },
  {
    id: 'w2', name: 'forager-02', model: 'gpt-5-5-xhigh', status: 'working',
    currentBead: 'b05', startedAt: 184,
    tokens: 218900, cost: 3.28,
    log: [
      { t: 184, kind: 'tool', text: 'br get b05' },
      { t: 170, kind: 'read', text: 'core/store/schema.sql' },
      { t: 142, kind: 'edit', text: 'core/store/schema.sql +84 −6' },
      { t: 96,  kind: 'edit', text: 'core/store/migrations/0003_beads.sql +63 −0' },
      { t: 48,  kind: 'tool', text: 'sqlx migrate run' },
      { t: 22,  kind: 'tool', text: 'cargo test -p store' },
      { t: 4,   kind: 'log',  text: '14 tests passed' },
    ],
  },
  {
    id: 'w3', name: 'forager-03', model: 'gemini-3-1-pro', status: 'working',
    currentBead: 'b06', startedAt: 47,
    tokens: 51200, cost: 0.15,
    log: [
      { t: 47, kind: 'tool', text: 'br get b06' },
      { t: 38, kind: 'read', text: 'ipc/agent-mail/proto.proto' },
      { t: 22, kind: 'edit', text: 'ipc/agent-mail/proto.proto +31 −3' },
      { t: 6,  kind: 'think', text: 'Adding reservation TTL to claim envelope...' },
    ],
  },
  {
    id: 'w4', name: 'forager-04', model: 'claude-opus-4-7-max', status: 'review',
    currentBead: 'b08', startedAt: 412,
    tokens: 184600, cost: 0.55,
    log: [
      { t: 412, kind: 'tool', text: 'br get b08' },
      { t: 380, kind: 'read', text: 'core/store/migrations/' },
      { t: 290, kind: 'edit', text: 'core/store/db.rs +112 −8' },
      { t: 180, kind: 'tool', text: 'cargo test -p store' },
      { t: 174, kind: 'log',  text: 'all 26 tests passed' },
      { t: 92,  kind: 'tool', text: 'cargo bench -p store' },
      { t: 4,   kind: 'log',  text: 'Awaiting CI green before commit' },
    ],
  },
  {
    id: 'w5', name: 'forager-05', model: 'gpt-5-5-xhigh', status: 'idle',
    currentBead: null, startedAt: null,
    tokens: 0, cost: 0,
    log: [
      { t: 1, kind: 'log', text: 'Idle — waiting for next runnable bead' },
    ],
  },
  {
    id: 'w6', name: 'forager-06', model: 'gemini-3-1-pro', status: 'blocked',
    currentBead: 'b13', startedAt: 230,
    tokens: 9400, cost: 0.008,
    log: [
      { t: 230, kind: 'tool', text: 'br get b13' },
      { t: 210, kind: 'read', text: 'docs/code-review-spec.md' },
      { t: 90,  kind: 'log',  text: 'Blocked: dependency b10 still in review' },
    ],
  },
];

// Recent bead-completion events (commits + pushes)
const INITIAL_EVENTS = [
  { t: -8,    worker: 'w2', kind: 'start',  bead: 'b05', text: 'Starting bead b05 — Implement database' },
  { t: -120,  worker: 'w1', kind: 'commit', bead: 'b03', text: 'feat(plan): research-and-eval shortlist',     sha: '8af2c1d' },
  { t: -118,  worker: 'w1', kind: 'push',   bead: 'b03', text: 'pushed to origin/main',                       sha: '8af2c1d' },
  { t: -240,  worker: 'w3', kind: 'commit', bead: 'b02', text: 'feat(req): consolidated requirements doc',    sha: 'd03ee47' },
  { t: -238,  worker: 'w3', kind: 'push',   bead: 'b02', text: 'pushed to origin/main',                       sha: 'd03ee47' },
  { t: -610,  worker: 'w4', kind: 'commit', bead: 'b01', text: 'docs(plan): goals & non-goals locked in',     sha: 'a0e221b' },
  { t: -608,  worker: 'w4', kind: 'push',   bead: 'b01', text: 'pushed to origin/main',                       sha: 'a0e221b' },
];

// ─────────────────────────────────────────────────────────────
// Top-level Swarm view
// ─────────────────────────────────────────────────────────────
function SwarmView({ t, launched, setLaunched }) {
  const [paused, setPaused] = useState(false);
  const [workers, setWorkers] = useState(INITIAL_WORKERS);
  const [events, setEvents] = useState(INITIAL_EVENTS);
  const [openWorker, setOpenWorker] = useState(null);
  const [tick, setTick] = useState(0);

  // Allow external "show launch panel" trigger from the Beads tab CTA
  useEffect(() => {
    window.__swarmShowLaunch = () => setLaunched(false);
    return () => { delete window.__swarmShowLaunch; };
  }, [setLaunched]);

  // Subtle motion — tick drives wall-clock + log timestamps
  useEffect(() => {
    if (paused) return;
    const id = setInterval(() => setTick((x) => x + 1), 1000);
    return () => clearInterval(id);
  }, [paused]);

  // Pre-launch panel
  if (!launched) {
    return <LaunchPanel t={t} onLaunch={() => setLaunched(true)} />;
  }

  const activeCount = workers.filter((w) => w.status === 'working').length;
  const reviewCount = workers.filter((w) => w.status === 'review').length;
  const blockedCount = workers.filter((w) => w.status === 'blocked').length;
  const idleCount = workers.filter((w) => w.status === 'idle').length;

  const togglePause = () => setPaused((p) => !p);
  const togglePauseWorker = (id) =>
    setWorkers((ws) =>
      ws.map((w) =>
        w.id === id ? { ...w, status: w.status === 'paused' ? 'working' : 'paused' } : w,
      ),
    );

  const openW = openWorker ? workers.find((w) => w.id === openWorker) : null;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      {/* Header */}
      <div style={{
        padding: '20px 28px 14px',
        borderBottom: `0.5px solid ${t.borderSoft}`,
        flexShrink: 0,
        display: 'flex', alignItems: 'flex-end', gap: 12, flexWrap: 'wrap',
      }}>
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 11, color: t.textDim, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Swarm · executing beads</div>
          <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, marginTop: 2, display: 'flex', alignItems: 'center', gap: 10 }}>
            <span>{workers.length} workers</span>
            {!paused ? (
              <span style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 12, color: t.success, fontWeight: 600 }}>
                <PulseDot color={t.success} />
                <span>swarm running</span>
              </span>
            ) : (
              <span style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 12, color: t.warn, fontWeight: 600 }}>
                <span style={{ width: 8, height: 8, borderRadius: '50%', background: t.warn }} />
                <span>paused</span>
              </span>
            )}
          </div>
          <div style={{ display: 'flex', gap: 14, marginTop: 8 }}>
            <Legend t={t} dot={t.accent}  label="working"  count={activeCount} />
            <Legend t={t} dot={t.warn}    label="review"   count={reviewCount} />
            <Legend t={t} dot="#E5484D"   label="blocked"  count={blockedCount} />
            <Legend t={t} dot={t.textMute} label="idle"    count={idleCount} />
          </div>
        </div>

        <button onClick={togglePause} style={{ ...swarmPillBtn(t, paused ? 'primary' : 'ghost'), whiteSpace: 'nowrap', flexShrink: 0 }}>
          {paused ? <PlayGlyph color="#fff" /> : <PauseGlyph color={t.text} />}
          <span>{paused ? 'Resume' : 'Pause'}</span>
        </button>
      </div>

      {/* Body — grid + side panel */}
      <div style={{ flex: 1, display: 'flex', minHeight: 0, overflow: 'hidden' }}>
        <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
          <div style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))',
            gap: 12,
          }}>
            {workers.map((w) => (
              <WorkerCard
                key={w.id}
                t={t}
                worker={w}
                tick={tick}
                paused={paused}
                onOpen={() => setOpenWorker(w.id)}
                onTogglePause={() => togglePauseWorker(w.id)}
              />
            ))}
          </div>

          {/* Recent events feed */}
          <div style={{ marginTop: 18 }}>
            <div style={{ fontSize: 10.5, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase', marginBottom: 8 }}>
              Recent activity
            </div>
            <div style={{
              background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 8,
              padding: '8px 0',
            }}>
              {events.map((e, i) => (
                <EventRow key={i} t={t} event={e} workers={workers} tick={tick} />
              ))}
            </div>
          </div>
        </div>

        {openW && (
          <WorkerLogPanel
            t={t}
            worker={openW}
            tick={tick}
            paused={paused}
            onClose={() => setOpenWorker(null)}
          />
        )}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Worker card
// ─────────────────────────────────────────────────────────────
function WorkerCard({ t, worker, tick, paused, onOpen, onTogglePause }) {
  const model = SWARM_MODELS.find((m) => m.id === worker.model);
  const bead = worker.currentBead ? BEADS.find((b) => b.id === worker.currentBead) : null;
  const elapsed = worker.startedAt != null ? worker.startedAt + (paused ? 0 : tick) : null;

  return (
    <div
      onClick={onOpen}
      style={{
        background: t.panel,
        border: `0.5px solid ${t.border}`,
        borderRadius: 10,
        boxShadow: t.shadow,
        cursor: 'pointer',
        overflow: 'hidden',
        transition: 'border-color 120ms ease',
        position: 'relative',
      }}
    >
      {/* Active shimmer top stripe */}
      {worker.status === 'working' && !paused && (
        <div style={{
          position: 'absolute', top: 0, left: 0, right: 0, height: 2,
          background: `linear-gradient(90deg, transparent 0%, ${t.accent} 50%, transparent 100%)`,
          backgroundSize: '200% 100%',
          animation: 'swarmShimmer 2.4s linear infinite',
        }} />
      )}

      {/* Header */}
      <div style={{
        padding: '10px 12px 8px',
        display: 'flex', alignItems: 'center', gap: 8,
        borderBottom: `0.5px solid ${t.borderSoft}`,
      }}>
        <WorkerStatusIndicator t={t} status={worker.status} paused={paused} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12.5, fontWeight: 700, color: t.text, fontFamily: SWARM_MONO, letterSpacing: -0.1 }}>
            {worker.name}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 10.5, color: t.textDim, marginTop: 1 }}>
            <span style={{ width: 5, height: 5, borderRadius: '50%', background: model.dot }} />
            <span>{model.short}</span>
          </div>
        </div>
        <button
          onClick={(e) => { e.stopPropagation(); onTogglePause(); }}
          title={worker.status === 'paused' ? 'Resume worker' : 'Pause worker'}
          style={{
            width: 22, height: 22, borderRadius: 5,
            border: 'none', background: 'transparent',
            color: t.textDim, cursor: 'pointer',
            display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
          }}
        >
          {worker.status === 'paused' ? <PlayGlyph size={10} color="currentColor" /> : <PauseGlyph size={10} color="currentColor" />}
        </button>
      </div>

      {/* Current bead */}
      <div style={{ padding: '10px 12px', display: 'flex', flexDirection: 'column', gap: 6 }}>
        {bead ? (
          <div>
            <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Currently on</div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginTop: 3 }}>
              <span style={{ fontFamily: SWARM_MONO, fontSize: 10.5, color: t.crest, fontWeight: 700 }}>{bead.id}</span>
              <span style={{ fontSize: 12, fontWeight: 600, color: t.text, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {bead.title}
              </span>
            </div>
          </div>
        ) : (
          <div style={{ fontSize: 12, color: t.textMute, fontStyle: 'italic' }}>Idle — waiting for runnable bead</div>
        )}

        {/* Mini log */}
        <div style={{
          background: t.panelAlt,
          border: `0.5px solid ${t.borderSoft}`,
          borderRadius: 6,
          padding: '6px 8px',
          fontSize: 10.5, fontFamily: SWARM_MONO, lineHeight: 1.5,
          color: t.textDim,
          maxHeight: 76, overflow: 'hidden',
          display: 'flex', flexDirection: 'column', gap: 2,
          position: 'relative',
        }}>
          {worker.log.slice(-3).map((l, i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 6, opacity: i === worker.log.slice(-3).length - 1 ? 1 : 0.65 }}>
              <LogKindGlyph t={t} kind={l.kind} />
              <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: t.text }}>{l.text}</span>
            </div>
          ))}
          {worker.status === 'working' && !paused && (
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 2, color: t.textMute }}>
              <TypingDots />
              <span style={{ fontSize: 10 }}>thinking</span>
            </div>
          )}
        </div>

        {/* Footer stats */}
        <div style={{
          display: 'flex', alignItems: 'center', gap: 12,
          fontSize: 10.5, color: t.textMute,
          fontVariantNumeric: 'tabular-nums',
        }}>
          {elapsed != null && (
            <span title="Time on current bead" style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
              <ClockGlyph color="currentColor" /> <span>{fmtDuration(elapsed)}</span>
            </span>
          )}
          <span title="Tokens used" style={{ display: 'flex', alignItems: 'center', gap: 3 }}>
            <span style={{ fontWeight: 600 }}>{(worker.tokens / 1000).toFixed(1)}k</span><span>tok</span>
          </span>
          <span style={{ flex: 1 }} />
          <span style={{ color: t.textMute, fontSize: 10 }}>open log →</span>
        </div>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Worker status indicator (left dot, pulses when working)
// ─────────────────────────────────────────────────────────────
function WorkerStatusIndicator({ t, status, paused }) {
  const map = {
    working: { color: t.accent,  pulse: true },
    review:  { color: t.warn,    pulse: false },
    blocked: { color: '#E5484D', pulse: false },
    idle:    { color: t.textMute,pulse: false },
    paused:  { color: t.warn,    pulse: false },
  };
  const m = map[status] || map.idle;
  return m.pulse && !paused ? <PulseDot color={m.color} /> : (
    <span style={{ width: 8, height: 8, borderRadius: '50%', background: m.color, flexShrink: 0 }} />
  );
}

function PulseDot({ color }) {
  return (
    <span style={{ position: 'relative', width: 8, height: 8, flexShrink: 0 }}>
      <span style={{
        position: 'absolute', inset: 0, borderRadius: '50%',
        background: color,
      }} />
      <span style={{
        position: 'absolute', inset: -3, borderRadius: '50%',
        background: color, opacity: 0.35,
        animation: 'swarmPulse 1.6s ease-out infinite',
      }} />
    </span>
  );
}

// ─────────────────────────────────────────────────────────────
// Side panel — full live log for selected worker
// ─────────────────────────────────────────────────────────────
function WorkerLogPanel({ t, worker, tick, paused, onClose }) {
  const model = SWARM_MODELS.find((m) => m.id === worker.model);
  const bead = worker.currentBead ? BEADS.find((b) => b.id === worker.currentBead) : null;
  return (
    <div style={{
      width: 380, flexShrink: 0,
      borderLeft: `0.5px solid ${t.border}`,
      background: t.panel,
      display: 'flex', flexDirection: 'column',
      minHeight: 0,
    }}>
      <div style={{
        padding: '12px 14px',
        borderBottom: `0.5px solid ${t.borderSoft}`,
        display: 'flex', alignItems: 'flex-start', gap: 8,
      }}>
        <WorkerStatusIndicator t={t} status={worker.status} paused={paused} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontFamily: SWARM_MONO, fontSize: 13, fontWeight: 700 }}>{worker.name}</div>
          <div style={{ fontSize: 11, color: t.textDim, marginTop: 1, display: 'flex', alignItems: 'center', gap: 5 }}>
            <span style={{ width: 5, height: 5, borderRadius: '50%', background: model.dot }} />
            <span>{model.name}</span>
          </div>
        </div>
        <button onClick={onClose} style={{
          background: 'transparent', border: 'none', color: t.textDim, cursor: 'pointer', fontSize: 14, padding: 4,
        }}>✕</button>
      </div>

      {bead && (
        <div style={{
          padding: '10px 14px',
          borderBottom: `0.5px solid ${t.borderSoft}`,
          background: t.panelAlt,
        }}>
          <div style={{ fontSize: 10, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Working on</div>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6, marginTop: 3 }}>
            <span style={{ fontFamily: SWARM_MONO, fontSize: 11, color: t.crest, fontWeight: 700 }}>{bead.id}</span>
            <span style={{ fontSize: 13, fontWeight: 600 }}>{bead.title}</span>
          </div>
          <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 2 }}>{bead.goal}</div>
        </div>
      )}

      <div style={{
        flex: 1, overflow: 'auto',
        padding: '8px 0',
        fontFamily: SWARM_MONO, fontSize: 11.5, lineHeight: 1.55,
      }}>
        {worker.log.slice().reverse().map((l, i) => (
          <div key={i} style={{
            padding: '4px 14px',
            display: 'flex', alignItems: 'flex-start', gap: 8,
            borderLeft: i === 0 && worker.status === 'working' && !paused ? `2px solid ${t.accent}` : '2px solid transparent',
          }}>
            <span style={{ color: t.textMute, flexShrink: 0, fontSize: 10.5, minWidth: 36 }}>
              −{fmtDuration(l.t + (paused ? 0 : tick) - (worker.startedAt || 0))}
            </span>
            <LogKindGlyph t={t} kind={l.kind} />
            <span style={{ flex: 1, color: t.text, wordBreak: 'break-word' }}>{l.text}</span>
          </div>
        ))}
        {worker.status === 'working' && !paused && (
          <div style={{
            padding: '6px 14px',
            display: 'flex', alignItems: 'center', gap: 6,
            color: t.textMute,
          }}>
            <TypingDots />
            <span style={{ fontSize: 11 }}>streaming…</span>
          </div>
        )}
      </div>

      {/* Footer stats */}
      <div style={{
        padding: '10px 14px',
        borderTop: `0.5px solid ${t.borderSoft}`,
        background: t.panelAlt,
        display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8,
        fontSize: 11, color: t.textDim, fontVariantNumeric: 'tabular-nums',
      }}>
        <Stat2 t={t} label="Elapsed" value={worker.startedAt != null ? fmtDuration(worker.startedAt + (paused ? 0 : tick)) : '—'} />
        <Stat2 t={t} label="Tokens"  value={`${(worker.tokens / 1000).toFixed(1)}k`} />
      </div>
    </div>
  );
}

function Stat2({ t, label, value }) {
  return (
    <div>
      <div style={{ fontSize: 9.5, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>{label}</div>
      <div style={{ fontSize: 13, fontWeight: 600, color: t.text }}>{value}</div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Recent activity rows (commits + pushes + bead starts)
// ─────────────────────────────────────────────────────────────
function EventRow({ t, event, workers }) {
  const w = workers.find((x) => x.id === event.worker);
  const map = {
    commit: { color: t.success, label: 'commit',  glyph: <GitCommitGlyph color={t.success} /> },
    push:   { color: t.accent,  label: 'push',    glyph: <UploadGlyph color={t.accent} /> },
    start:  { color: t.crest,   label: 'start',   glyph: <SparkGlyph color={t.crest} /> },
  };
  const m = map[event.kind] || map.start;
  return (
    <div style={{
      padding: '6px 14px',
      display: 'flex', alignItems: 'center', gap: 10,
      fontSize: 12,
    }}>
      <span style={{ width: 14, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>{m.glyph}</span>
      <span style={{ fontFamily: SWARM_MONO, fontSize: 10.5, color: t.textMute, minWidth: 60 }}>−{fmtDuration(-event.t)}</span>
      <span style={{ fontFamily: SWARM_MONO, fontSize: 10.5, color: t.textDim, minWidth: 70 }}>{w?.name}</span>
      <span style={{ fontFamily: SWARM_MONO, fontSize: 10.5, color: t.crest, fontWeight: 700, minWidth: 28 }}>{event.bead}</span>
      <span style={{ flex: 1, color: t.text }}>{event.text}</span>
      {event.sha && (
        <span style={{ fontFamily: SWARM_MONO, fontSize: 10.5, color: t.textMute, padding: '1px 6px', borderRadius: 3, background: t.panelAlt, border: `0.5px solid ${t.borderSoft}` }}>
          {event.sha}
        </span>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Launch panel
// ─────────────────────────────────────────────────────────────
function LaunchPanel({ t, onLaunch }) {
  const [mode, setMode] = useState('manual'); // 'auto' | 'manual'
  const [counts, setCounts] = useState({
    'gpt-5-5-xhigh': 2,
    'claude-opus-4-7-max': 2,
    'gemini-3-1-pro': 2,
  });

  const total = Object.values(counts).reduce((s, n) => s + n, 0);

  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 18, maxWidth: 760 }}>
      <div>
        <div style={{ fontSize: 11, color: t.textDim, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Swarm · launch</div>
        <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, marginTop: 2 }}>Configure your fleet</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4, lineHeight: 1.5 }}>
          Workers pick beads off the graph in topological order. When a bead is done, the worker commits
          the changes, pushes to GitHub, and moves to the next runnable bead.
        </div>
      </div>

      {/* Mode toggle */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
        {[
          { id: 'auto',   title: 'Let Hoopoe decide',         desc: 'Right-size the fleet from bead complexity, parallelism, and cost budget.' },
          { id: 'manual', title: 'Configure fleet manually',  desc: 'Pick how many of each Claude model. Mix Opus for hard beads + Sonnet/Haiku for grunt work.' },
        ].map((o) => {
          const sel = mode === o.id;
          return (
            <button key={o.id} onClick={() => setMode(o.id)} style={{
              textAlign: 'left',
              background: sel ? t.crestSoft : t.panel,
              border: `1px solid ${sel ? t.crest : t.border}`,
              borderRadius: 10, padding: 14,
              cursor: 'pointer', fontFamily: SWARM_FONT,
              display: 'flex', flexDirection: 'column', gap: 6,
              boxShadow: t.shadow,
            }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{
                  width: 14, height: 14, borderRadius: '50%',
                  border: `1.5px solid ${sel ? t.crest : t.borderSoft}`,
                  background: sel ? t.crest : 'transparent',
                  display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0,
                }}>
                  {sel && <span style={{ width: 5, height: 5, borderRadius: '50%', background: '#fff' }} />}
                </span>
                <span style={{ fontSize: 13, fontWeight: 600, color: sel ? t.crest : t.text }}>{o.title}</span>
              </div>
              <div style={{ fontSize: 11.5, color: t.textDim, lineHeight: 1.45, paddingLeft: 22 }}>{o.desc}</div>
            </button>
          );
        })}
      </div>

      {/* Manual config */}
      {mode === 'manual' && (
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10, padding: 16,
          boxShadow: t.shadow,
          display: 'flex', flexDirection: 'column', gap: 10,
        }}>
          <div style={{ fontSize: 11, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Fleet composition</div>
          {SWARM_MODELS.map((m) => (
            <div key={m.id} style={{
              display: 'grid', gridTemplateColumns: '20px 160px 1fr 100px 120px',
              gap: 12, alignItems: 'center',
            }}>
              <span style={{ width: 8, height: 8, borderRadius: '50%', background: m.dot }} />
              <span style={{ fontSize: 12.5, fontWeight: 600 }}>{m.name}</span>
              <input
                type="range"
                min={0} max={8} step={1}
                value={counts[m.id]}
                onChange={(e) => setCounts((c) => ({ ...c, [m.id]: Number(e.target.value) }))}
                style={{ width: '100%', accentColor: t.crest }}
              />
              <span style={{ fontSize: 12, fontVariantNumeric: 'tabular-nums', color: t.textDim }}>
                {counts[m.id]} {counts[m.id] === 1 ? 'worker' : 'workers'}
              </span>
              <span style={{ fontSize: 11, color: t.textMute, fontFamily: SWARM_MONO, textAlign: 'right' }}>
                ${m.cost.toFixed(4)}/1k tok
              </span>
            </div>
          ))}
        </div>
      )}

      {mode === 'auto' && (
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10, padding: 16,
          display: 'flex', flexDirection: 'column', gap: 6,
          boxShadow: t.shadow,
        }}>
          <div style={{ fontSize: 12.5, color: t.text }}>
            Recommended fleet for <span style={{ fontWeight: 600 }}>14 beads</span> with current dependency depth:
          </div>
          <div style={{ fontFamily: SWARM_MONO, fontSize: 12, color: t.textDim, lineHeight: 1.7 }}>
            <span style={{ color: '#10A37F' }}>●</span> 2× Chat GPT Pro &nbsp;·&nbsp;
            <span style={{ color: '#C25A2E' }}>●</span> 2× Opus 4.7 max &nbsp;·&nbsp;
            <span style={{ color: '#4285F4' }}>●</span> 2× Gemini 3.1 Pro
          </div>
          <div style={{ fontSize: 11.5, color: t.textMute }}>Estimated wall-clock: ~38 min · estimated cost: $4.20</div>
        </div>
      )}

      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <span style={{ fontSize: 12, color: t.textDim }}>
          Total: <span style={{ color: t.text, fontWeight: 600 }}>{total} workers</span>
        </span>
        <span style={{ flex: 1 }} />
        <button onClick={onLaunch} style={swarmPillBtn(t, 'primary')}>
          <SparkGlyph color="#fff" />
          <span>Launch swarm</span>
        </button>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Tiny visual primitives
// ─────────────────────────────────────────────────────────────
function Legend({ t, dot, label, count }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 11.5, color: t.textDim }}>
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: dot }} />
      <span style={{ fontWeight: 600, color: t.text, fontVariantNumeric: 'tabular-nums' }}>{count}</span>
      <span>{label}</span>
    </div>
  );
}

function LogKindGlyph({ t, kind }) {
  const c = {
    tool:  '#0A84FF',
    read:  '#8E8E93',
    edit:  '#30B66B',
    think: '#C25A2E',
    log:   '#8E8E93',
  }[kind] || t.textMute;
  const glyph = {
    tool:  '⌘',
    read:  '◇',
    edit:  '✎',
    think: '✻',
    log:   '·',
  }[kind] || '·';
  return <span style={{ color: c, fontFamily: SWARM_MONO, fontWeight: 700, width: 12, textAlign: 'center', flexShrink: 0 }}>{glyph}</span>;
}

function PauseGlyph({ size = 11, color = 'currentColor' }) {
  return (
    <svg width={size} height={size} viewBox="0 0 12 12">
      <rect x="3" y="2.5" width="2" height="7" rx="0.5" fill={color} />
      <rect x="7" y="2.5" width="2" height="7" rx="0.5" fill={color} />
    </svg>
  );
}
function PlayGlyph({ size = 11, color = 'currentColor' }) {
  return (
    <svg width={size} height={size} viewBox="0 0 12 12">
      <path d="M3 2.5 L9.5 6 L3 9.5 Z" fill={color} />
    </svg>
  );
}
function ClockGlyph({ color = 'currentColor' }) {
  return (
    <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
      <circle cx="6" cy="6" r="4.5" stroke={color} strokeWidth="1" />
      <path d="M6 3.5V6L7.5 7" stroke={color} strokeWidth="1" strokeLinecap="round" />
    </svg>
  );
}
function GitCommitGlyph({ color = 'currentColor' }) {
  return (
    <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
      <line x1="2" y1="7" x2="5" y2="7" stroke={color} strokeWidth="1.2" />
      <line x1="9" y1="7" x2="12" y2="7" stroke={color} strokeWidth="1.2" />
      <circle cx="7" cy="7" r="2.5" stroke={color} strokeWidth="1.2" fill="none" />
    </svg>
  );
}
function UploadGlyph({ color = 'currentColor' }) {
  return (
    <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
      <path d="M6 2v6M3 5l3-3 3 3M2.5 10h7" stroke={color} strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function SparkGlyph({ color = 'currentColor' }) {
  return (
    <svg width="11" height="11" viewBox="0 0 12 12" fill="none">
      <path d="M6 1.5l1.1 3.4L10.5 6l-3.4 1.1L6 10.5 4.9 7.1 1.5 6l3.4-1.1L6 1.5z" fill={color} />
    </svg>
  );
}
function TypingDots() {
  return (
    <span style={{ display: 'inline-flex', gap: 2, alignItems: 'center', height: 8 }}>
      {[0, 1, 2].map((i) => (
        <span key={i} style={{
          width: 3, height: 3, borderRadius: '50%',
          background: 'currentColor',
          animation: 'swarmTyping 1.2s ease-in-out infinite',
          animationDelay: `${i * 0.15}s`,
        }} />
      ))}
    </span>
  );
}

// pill button helper
function swarmPillBtn(t, kind = 'ghost') {
  const base = {
    height: 30, padding: '0 14px', borderRadius: 7,
    fontFamily: SWARM_FONT, fontSize: 12, fontWeight: 600,
    display: 'inline-flex', alignItems: 'center', gap: 6,
    cursor: 'pointer', border: 'none',
  };
  if (kind === 'primary') return { ...base, background: t.crest, color: '#fff' };
  return { ...base, background: t.panel, color: t.text, border: `0.5px solid ${t.borderSoft}` };
}

function fmtDuration(seconds) {
  if (seconds == null || isNaN(seconds)) return '—';
  const s = Math.max(0, Math.floor(seconds));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const r = s % 60;
  if (m < 60) return `${m}m ${r.toString().padStart(2, '0')}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${(m % 60).toString().padStart(2, '0')}m`;
}

// CSS keyframes injected once
if (typeof document !== 'undefined' && !document.getElementById('swarm-keyframes')) {
  const style = document.createElement('style');
  style.id = 'swarm-keyframes';
  style.textContent = `
    @keyframes swarmShimmer {
      0%   { background-position: 200% 0%; }
      100% { background-position: -200% 0%; }
    }
    @keyframes swarmPulse {
      0%   { transform: scale(0.6); opacity: 0.5; }
      100% { transform: scale(2.0); opacity: 0;   }
    }
    @keyframes swarmTyping {
      0%, 80%, 100% { opacity: 0.25; }
      40%           { opacity: 1;    }
    }
  `;
  document.head.appendChild(style);
}

Object.assign(window, {
  SwarmView,
});
