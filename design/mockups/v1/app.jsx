// Hoopoe — Plan creation & multi-model synthesis flow
// macOS Tahoe-inspired, light + dark, native vibrancy aesthetic

const { useState, useEffect, useRef, useMemo } = React;

const FONT = '-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", "Helvetica Neue", system-ui, sans-serif';
const MONO = '"SF Mono", ui-monospace, "JetBrains Mono", Menlo, monospace';

// ─────────────────────────────────────────────────────────────
// Theme tokens
// ─────────────────────────────────────────────────────────────
const TOKENS = {
  light: {
    bg: '#ECECEE',
    bgDeep: '#D8DBE0',
    sidebar: 'rgba(232, 234, 240, 0.72)',
    sidebarBorder: 'rgba(0,0,0,0.07)',
    panel: '#FFFFFF',
    panelAlt: '#F7F7F9',
    border: 'rgba(0,0,0,0.10)',
    borderSoft: 'rgba(0,0,0,0.06)',
    text: 'rgba(0,0,0,0.88)',
    textDim: 'rgba(0,0,0,0.55)',
    textMute: 'rgba(0,0,0,0.38)',
    accent: '#0A84FF',
    accentSoft: 'rgba(10,132,255,0.12)',
    crest: '#C25A2E', // hoopoe russet
    crestDeep: '#A0421C',
    crestSoft: 'rgba(194,90,46,0.10)',
    success: '#30B66B',
    warn: '#E9963F',
    danger: '#E5484D',
    shadow: '0 1px 0 rgba(255,255,255,0.6) inset, 0 1px 2px rgba(0,0,0,0.04)',
    selectedRow: 'rgba(10,132,255,0.18)',
    hoverRow: 'rgba(0,0,0,0.04)',
    desktop: 'radial-gradient(120% 80% at 30% 0%, #f4d4b8 0%, #e8c5e0 35%, #c5d8f0 70%, #b8c8e0 100%)'
  },
  dark: {
    bg: '#1E1E20',
    bgDeep: '#0F0F11',
    sidebar: 'rgba(38, 38, 42, 0.72)',
    sidebarBorder: 'rgba(255,255,255,0.06)',
    panel: '#26262A',
    panelAlt: '#1F1F23',
    border: 'rgba(255,255,255,0.10)',
    borderSoft: 'rgba(255,255,255,0.06)',
    text: 'rgba(255,255,255,0.92)',
    textDim: 'rgba(255,255,255,0.58)',
    textMute: 'rgba(255,255,255,0.38)',
    accent: '#0A84FF',
    accentSoft: 'rgba(10,132,255,0.20)',
    crest: '#E58253',
    crestDeep: '#C25A2E',
    crestSoft: 'rgba(229,130,83,0.14)',
    success: '#32D173',
    warn: '#F2A045',
    danger: '#FF5C60',
    shadow: '0 1px 0 rgba(255,255,255,0.04) inset, 0 1px 2px rgba(0,0,0,0.4)',
    selectedRow: 'rgba(10,132,255,0.32)',
    hoverRow: 'rgba(255,255,255,0.05)',
    desktop: 'radial-gradient(120% 80% at 30% 0%, #2a1a14 0%, #1a1820 40%, #0f1418 100%)'
  }
};

// ─────────────────────────────────────────────────────────────
// Hoopoe crest mark — geometric interpretation of the hoopoe's fan crest
// ─────────────────────────────────────────────────────────────
function HoopoeCrest({ size = 22, color, accent }) {
  return (
    <svg width={size} height={size} viewBox="0 0 28 28" fill="none">
      {/* fan crest — 5 feathers */}
      {[-32, -16, 0, 16, 32].map((rot, i) =>
      <g key={i} transform={`rotate(${rot} 14 18)`}>
          <path
          d="M14 18 L14 6 L13 6 L13 18 Z"
          fill={i === 2 ? accent : color}
          opacity={i === 2 ? 1 : 0.85} />
        
          <circle cx="13.5" cy="6" r="1" fill={i === 2 ? accent : color} />
        </g>
      )}
      {/* head */}
      <circle cx="14" cy="20" r="4" fill={color} />
      {/* beak */}
      <path d="M18 20 L25 22 L18 21.5 Z" fill={color} />
      {/* eye */}
      <circle cx="15" cy="19.5" r="0.6" fill={accent} />
    </svg>);

}

// ─────────────────────────────────────────────────────────────
// Tiny SF-Symbols-style icons
// ─────────────────────────────────────────────────────────────
const Icon = {
  folder: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <path d="M2 5a1.5 1.5 0 0 1 1.5-1.5h2.8c.4 0 .8.16 1.06.44l.7.7c.27.28.66.44 1.06.44h3.38A1.5 1.5 0 0 1 14 6.58V11.5A1.5 1.5 0 0 1 12.5 13h-9A1.5 1.5 0 0 1 2 11.5z" fill={c} fillOpacity="0.85" />
    </svg>,

  doc: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <path d="M4 2.5A1.5 1.5 0 0 1 5.5 1h4L13 4.5v8A1.5 1.5 0 0 1 11.5 14h-6A1.5 1.5 0 0 1 4 12.5z" stroke={c} strokeWidth="1.2" fill="none" />
      <path d="M9.5 1v3.5H13" stroke={c} strokeWidth="1.2" fill="none" />
    </svg>,

  plus: (s = 12, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 12 12" fill="none">
      <path d="M6 2v8M2 6h8" stroke={c} strokeWidth="1.5" strokeLinecap="round" />
    </svg>,

  compose: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <path d="M3 4.5A1.5 1.5 0 0 1 4.5 3H8" stroke={c} strokeWidth="1.3" strokeLinecap="round" />
      <path d="M13 8v3.5A1.5 1.5 0 0 1 11.5 13h-7A1.5 1.5 0 0 1 3 11.5v-7" stroke={c} strokeWidth="1.3" strokeLinecap="round" />
      <path d="M11.2 2.6l2.2 2.2-4.6 4.6-2.6.4.4-2.6 4.6-4.6z" stroke={c} strokeWidth="1.3" strokeLinejoin="round" fill="none" />
    </svg>,

  chevR: (s = 10, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 10 10" fill="none">
      <path d="M3.5 2L7 5L3.5 8" stroke={c} strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>,

  chevD: (s = 10, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 10 10" fill="none">
      <path d="M2 3.5L5 7L8 3.5" stroke={c} strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>,

  send: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <path d="M8 2v12M8 2l4 4M8 2L4 6" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>,

  check: (s = 12, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 12 12" fill="none">
      <path d="M2.5 6.2L5 8.5L9.5 3.5" stroke={c} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>,

  spark: (s = 12, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 12 12" fill="none">
      <path d="M6 1L7 5L11 6L7 7L6 11L5 7L1 6L5 5Z" fill={c} />
    </svg>,

  sun: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <circle cx="8" cy="8" r="3" stroke={c} strokeWidth="1.3" />
      {[0, 45, 90, 135, 180, 225, 270, 315].map((a) =>
    <line key={a} x1="8" y1="2" x2="8" y2="3.4"
    stroke={c} strokeWidth="1.3" strokeLinecap="round"
    transform={`rotate(${a} 8 8)`} />
    )}
    </svg>,

  moon: (s = 14, c = 'currentColor') =>
  <svg width={s} height={s} viewBox="0 0 16 16" fill="none">
      <path d="M12 9.5A4.5 4.5 0 0 1 6.5 4a5 5 0 1 0 5.5 5.5z" fill={c} />
    </svg>

};

// ─────────────────────────────────────────────────────────────
// Mock data
// ─────────────────────────────────────────────────────────────
const PROJECTS = [
{ id: 'cass', name: 'CASS Memory System', plans: 3, beads: 347, lang: 'Rust' },
{ id: 'frank', name: 'FrankenSQLite', plans: 2, beads: 189, lang: 'Go' },
{ id: 'asup', name: 'Asupersync', plans: 4, beads: 412, lang: 'Rust' },
{ id: 'atlas', name: 'Atlas Notes', plans: 1, beads: 28, lang: 'TypeScript' }];


const PLANS = {
  cass: [
  { id: 'p1', name: 'Memory System Core', status: 'finalized', lines: 5547, rounds: 6 },
  { id: 'p2', name: 'Web Export Feature', status: 'refining', lines: 3402, rounds: 3 },
  { id: 'p3', name: 'CLI Wrapper', status: 'draft', lines: 812, rounds: 1 }],

  frank: [
  { id: 'p4', name: 'Storage Engine', status: 'finalized', lines: 4120, rounds: 5 },
  { id: 'p5', name: 'Query Layer', status: 'beads', lines: 2980, rounds: 4 }],

  asup: [
  { id: 'p6', name: 'NATS Integration', status: 'finalized', lines: 6201, rounds: 7 },
  { id: 'p7', name: 'Replay System', status: 'draft', lines: 1844, rounds: 2 }],

  atlas: [
  { id: 'p8', name: 'Atlas Notes MVP', status: 'beads', lines: 5547, rounds: 5 }]

};

const MODELS = [
{ id: 'gpt-pro', name: 'Chat GPT Pro', vendor: 'OpenAI', recommended: true, dot: '#10A37F', tag: 'Extended Reasoning' },
{ id: 'opus', name: 'Opus 4.7', vendor: 'Anthropic', dot: '#C25A2E', tag: 'Architectural depth' },
{ id: 'gemini', name: 'Gemini 3.1 Pro', vendor: 'Google', dot: '#4285F4', tag: 'Alt framings' },
{ id: 'grok', name: 'Grok Heavy', vendor: 'xAI', dot: '#888', tag: 'Counterintuitive options' }];


// Simulated plan summaries by model
const PLAN_DRAFTS = {
  'gpt-pro': {
    lines: 4820,
    sections: 38,
    headline: 'System-wide coherence; thorough workflow coverage.',
    strengths: ['Whole-system architecture', 'Tradeoff analysis', 'Failure-mode mapping'],
    blindspots: ['Conservative on novel patterns'],
    excerpt: `# Plan: Bead-Driven Coordination Layer

## 1. Goals & Non-Goals
A plan-centric desktop app that orchestrates an agent swarm…

## 2. Architecture
- Electron shell (main + renderer)
- Local SQLite for plan + bead state
- MCP Agent Mail bridge process
- bv as a sidecar binary

## 3. User Workflows
### 3.1 Plan Creation
The user opens a project, creates a plan, and selects up to four
competing models. The app stores each draft separately and offers
a "best-of-all-worlds" synthesis…`
  },
  'opus': {
    lines: 5104,
    sections: 42,
    headline: 'Sharp structural edits; execution detail.',
    strengths: ['Concrete data shapes', 'Precise IPC contracts', 'Test scaffolding'],
    blindspots: ['Less coverage of operational concerns'],
    excerpt: `# Coordination Layer — Detailed Plan

## Module Map
- core/plan-store     — append-only event log, snapshot every 50 ops
- core/bead-graph     — DAG with PageRank + betweenness via petgraph
- ipc/agent-mail      — protobuf-typed bridge, 30s TTL on reservations

## Plan Document Schema
struct Plan {
  id: PlanId,
  title: String,
  rounds: Vec<RefinementRound>,
  sources: Vec<ModelDraft>,
  finalized_at: Option<Timestamp>,
}`
  },
  'gemini': {
    lines: 4377,
    sections: 35,
    headline: 'Alternative framings; missed edge cases surfaced.',
    strengths: ['Edge-case coverage', 'Accessibility considerations', 'Multi-window flows'],
    blindspots: ['Less opinionated on architecture'],
    excerpt: `# Hoopoe — Alternative Framing

What if the unit of work isn't a *plan* but a *thread*?
Each plan is a long-running conversation with branches, where
each branch holds an alternate model's draft…

## Edge cases catalogue
- Mid-refinement model rate-limit → graceful resume
- Plan exceeds context window → auto-chunk + cross-ref index
- User edits during synthesis → optimistic CRDT merge`
  },
  'grok': {
    lines: 3912,
    sections: 31,
    headline: 'Counterintuitive options; stress-tested assumptions.',
    strengths: ['Aggressive parallelism', 'Novel UI metaphors', 'Risk-taking'],
    blindspots: ['Some ideas need refinement'],
    excerpt: `# Hoopoe (alt take)

The whole "plan → bead → swarm" pipeline is sequential. What if
the plan IS the bead graph, with markdown rendered as a flattened
view? Refinement rounds become graph diffs, not text diffs.

## Provocations
1. Drop the markdown plan entirely; beads from minute one.
2. Treat each model as an *agent* in the plan-creation phase too.
3. Continuous synthesis — never finalize, just freeze a snapshot.`
  }
};

const REVIEW_ROUNDS = [
{ n: 1, title: 'Initial review', deltas: 47, status: 'done', summary: '23 architectural revisions, 12 new sections, 8 reframings, 4 deletions.' },
{ n: 2, title: 'Architecture pass', deltas: 28, status: 'done', summary: 'Tightened module boundaries; added IPC schema; resolved 6 contradictions.' },
{ n: 3, title: 'Edge cases', deltas: 19, status: 'done', summary: 'Surfaced 11 failure modes; added retry policies; auth boundary clarified.' },
{ n: 4, title: 'Robustness', deltas: 9, status: 'active', summary: 'In progress — cross-checking against competing plan elements.' },
{ n: 5, title: 'Convergence check', deltas: 0, status: 'pending', summary: 'Final pass — looking for incremental-only suggestions.' }];


// ─────────────────────────────────────────────────────────────
// App
// ─────────────────────────────────────────────────────────────
const TWEAK_DEFAULTS = /*EDITMODE-BEGIN*/{
  "theme": "light"
} /*EDITMODE-END*/;

function App() {
  const [tweaks, setTweak] = useTweaks(TWEAK_DEFAULTS);
  const t = TOKENS[tweaks.theme === 'dark' ? 'dark' : 'light'];

  const [showFirstRun, setShowFirstRun] = useState(() => {
    try { return localStorage.getItem('hoopoe.firstRunComplete') !== 'true'; }
    catch { return true; }
  });
  const finishFirstRun = () => {
    try { localStorage.setItem('hoopoe.firstRunComplete', 'true'); } catch {}
    setShowFirstRun(false);
  };

  const [projectId, setProjectId] = useState('atlas');
  const [planId, setPlanId] = useState('p8');
  const [view, setView] = useState('plan'); // 'plan' | 'wizard' | 'compare' | 'synthesize' | 'rounds' | 'health'
  const [swarmLaunched, setSwarmLaunched] = useState(true);
  const [wizardStep, setWizardStep] = useState(1); // 1: chat, 2: models, 3: compare, 4: synthesize, 5: rounds

  const project = PROJECTS.find((p) => p.id === projectId);
  const plans = PLANS[projectId] || [];
  const plan = plans.find((p) => p.id === planId) || plans[0];

  const startWizard = () => {
    setView('wizard');
    setWizardStep(0);
  };

  return (
    <div style={{
      width: '100vw', height: '100vh', background: t.desktop,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      fontFamily: FONT, color: t.text,
      transition: 'background 300ms ease'
    }}>
      <TweaksPanel title="Tweaks" defaultPos={{ right: 16, bottom: 16 }}>
        <TweakSection title="Appearance">
          <TweakRadio
            label="Theme"
            value={tweaks.theme}
            options={[{ value: 'light', label: 'Light' }, { value: 'dark', label: 'Dark' }]}
            onChange={(v) => setTweak('theme', v)} />
          
        </TweakSection>
        <TweakSection title="First-run flow">
          <TweakButton label="Replay setup wizard" onClick={() => {
            try { localStorage.removeItem('hoopoe.firstRunComplete'); } catch {}
            setShowFirstRun(true);
          }} />
        </TweakSection>
      </TweaksPanel>

      <MacShell t={t} theme={tweaks.theme}>
        {showFirstRun && (
          <FirstRun t={t} theme={tweaks.theme} onFinish={finishFirstRun} />
        )}
        {!showFirstRun && <>
        <Sidebar
          t={t}
          projectId={projectId}
          setProjectId={(id) => {setProjectId(id);const ps = PLANS[id];if (ps) setPlanId(ps[0].id);setView('plan');}}
          planId={planId}
          setPlanId={(id) => {setPlanId(id);setView('plan');}}
          theme={tweaks.theme}
          setTheme={(v) => setTweak('theme', v)}
          onNewPlan={startWizard}
          onNewProject={() => {/* placeholder */}} />
        
        <Main
          t={t}
          project={project}
          plan={plan}
          view={view}
          setView={setView}
          wizardStep={wizardStep}
          setWizardStep={setWizardStep}
          onStartWizard={startWizard}
          swarmLaunched={swarmLaunched}
          setSwarmLaunched={setSwarmLaunched} />
        </>}
      </MacShell>
    </div>);

}

// ─────────────────────────────────────────────────────────────
// macOS window shell — light/dark aware, traffic lights, vibrancy
// ─────────────────────────────────────────────────────────────
function MacShell({ children, t, theme }) {
  return (
    <div style={{
      width: 1200, height: 780,
      borderRadius: 12, overflow: 'hidden',
      background: t.bg,
      boxShadow: theme === 'dark' ?
      '0 0 0 0.5px rgba(255,255,255,0.12), 0 30px 80px rgba(0,0,0,0.6), 0 12px 30px rgba(0,0,0,0.4)' :
      '0 0 0 0.5px rgba(0,0,0,0.18), 0 30px 80px rgba(0,0,0,0.25), 0 12px 30px rgba(0,0,0,0.15)',
      display: 'flex',
      position: 'relative'
    }}>
      {children}
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Sidebar
// ─────────────────────────────────────────────────────────────
function Sidebar({ t, projectId, setProjectId, planId, setPlanId, theme, setTheme, onNewPlan, onNewProject }) {
  const [expanded, setExpanded] = useState({ [projectId]: true });
  const [sidebarMode, setSidebarMode] = useState('plans'); // 'plans' | 'all'

  return (
    <div style={{
      width: 248, height: '100%', flexShrink: 0,
      background: t.sidebar,
      backdropFilter: 'blur(40px) saturate(180%)',
      WebkitBackdropFilter: 'blur(40px) saturate(180%)',
      borderRight: `0.5px solid ${t.sidebarBorder}`,
      display: 'flex', flexDirection: 'column'
    }}>
      {/* traffic lights row */}
      <div style={{
        height: 38, display: 'flex', alignItems: 'center',
        padding: '0 14px', flexShrink: 0
      }}>
        <div style={{ display: 'flex', gap: 8 }}>
          <Light c="#ff5f57" />
          <Light c="#febc2e" />
          <Light c="#28c840" />
        </div>
      </div>

      {/* brand */}
      <div style={{
        padding: '4px 16px 14px', display: 'flex', alignItems: 'center', gap: 10
      }}>
        <HoopoeCrest size={26} color={t.text} accent={t.crest} />
        <div>
          <div style={{ fontSize: 14, fontWeight: 700, letterSpacing: -0.2 }}>Hoopoe</div>
          <div style={{ fontSize: 10.5, color: t.textDim, marginTop: -1 }}>Agentic Coding Cockpit</div>
        </div>
      </div>

      {/* primary CTA — New Project */}
      <div style={{ padding: '0 12px 10px' }}>
        <button onClick={onNewProject} style={{
          width: '100%', height: 32, borderRadius: 8,
          border: 'none',
          background: `linear-gradient(180deg, ${t.crest} 0%, ${t.crestDeep || t.crest} 100%)`,
          color: '#fff',
          fontFamily: FONT, fontSize: 12.5, fontWeight: 600, letterSpacing: -0.1,
          display: 'flex', alignItems: 'center', justifyContent: 'center', gap: 6,
          cursor: 'pointer',
          boxShadow: theme === 'dark' ?
          `inset 0 0.5px 0 rgba(255,255,255,0.18), 0 1px 2px rgba(0,0,0,0.4)` :
          `inset 0 0.5px 0 rgba(255,255,255,0.4), 0 1px 2px rgba(0,0,0,0.12)`
        }}>
          {Icon.plus(12, '#fff')}
          <span>New Project</span>
        </button>
      </div>

      {/* projects header w/ view-mode toggle */}
      <div style={{
        padding: '8px 12px 4px',
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
      }}>
        <div style={{
          fontSize: 10.5, fontWeight: 700, letterSpacing: 0.4,
          color: t.textMute, textTransform: 'uppercase',
        }}>{sidebarMode === 'plans' ? 'Projects' : 'Workspace'}</div>
        <div style={{
          display: 'flex',
          background: t.panelAlt || t.hoverRow,
          border: `0.5px solid ${t.sidebarBorder}`,
          borderRadius: 5, padding: 1.5, height: 20,
        }}>
          {[
            { id: 'plans', label: 'Plans', tip: 'Show only plan documents' },
            { id: 'all',   label: 'Files', tip: 'Show all project files' },
          ].map((o) => (
            <button
              key={o.id}
              onClick={() => setSidebarMode(o.id)}
              title={o.tip}
              style={{
                padding: '0 7px',
                borderRadius: 3.5, border: 'none',
                background: sidebarMode === o.id ? t.panel : 'transparent',
                color: sidebarMode === o.id ? t.text : t.textDim,
                fontFamily: '-apple-system, BlinkMacSystemFont, sans-serif',
                fontSize: 10, fontWeight: 600,
                cursor: 'pointer',
                boxShadow: sidebarMode === o.id ? '0 0.5px 1.5px rgba(0,0,0,0.08)' : 'none',
              }}
            >{o.label}</button>
          ))}
        </div>
      </div>

      <div style={{ flex: 1, overflow: 'auto', padding: '0 8px' }}>
        {PROJECTS.map((p) => {
          const isOpen = expanded[p.id];
          const isSel = p.id === projectId;
          return (
            <div key={p.id}>
              <ProjectRow
                t={t}
                project={p}
                isOpen={isOpen}
                selected={isSel && !planId}
                onClick={() => {
                  setExpanded((e) => ({ ...e, [p.id]: !e[p.id] }));
                  setProjectId(p.id);
                }}
                onNewPlan={(e) => {
                  e.stopPropagation();
                  setProjectId(p.id);
                  setExpanded((prev) => ({ ...prev, [p.id]: true }));
                  onNewPlan();
                }} />

              {isOpen && sidebarMode === 'plans' && (PLANS[p.id] || []).map((pl) =>
              <Row
                key={pl.id}
                t={t}
                selected={pl.id === planId && p.id === projectId}
                indent={1}
                onClick={() => {setProjectId(p.id);setPlanId(pl.id);}}>

                  <span style={{ width: 12 }} />
                  {Icon.doc(12, t.textDim)}
                  <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pl.name}</span>
                  <StatusDot t={t} status={pl.status} />
                </Row>
              )}

              {isOpen && sidebarMode === 'all' && (
                <FileTree
                  t={t}
                  project={p}
                  plans={PLANS[p.id] || []}
                  planId={planId}
                  onSelectPlan={(pid) => { setProjectId(p.id); setPlanId(pid); }}
                />
              )}
            </div>);

        })}
      </div>

      {/* footer — theme toggle */}
      <div style={{
        borderTop: `0.5px solid ${t.sidebarBorder}`,
        padding: 10, display: 'flex', alignItems: 'center', justifyContent: 'space-between'
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <div style={{
            width: 22, height: 22, borderRadius: '50%',
            background: t.accentSoft,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
            color: t.accent, fontSize: 10, fontWeight: 700
          }}>JE</div>
          <div style={{ fontSize: 11.5 }}>jeffrey</div>
        </div>
        <button
          onClick={() => setTheme(theme === 'dark' ? 'light' : 'dark')}
          style={{
            width: 24, height: 24, borderRadius: 6,
            background: 'transparent', border: 'none', cursor: 'pointer',
            color: t.textDim, display: 'flex', alignItems: 'center', justifyContent: 'center'
          }}>
          {theme === 'dark' ? Icon.sun(14, t.textDim) : Icon.moon(14, t.textDim)}
        </button>
      </div>
    </div>);

}

function Light({ c }) {
  return <div style={{ width: 12, height: 12, borderRadius: '50%', background: c, border: '0.5px solid rgba(0,0,0,0.12)' }} />;
}

function SectionLabel({ t, label }) {
  return (
    <div style={{
      padding: '8px 16px 4px',
      fontSize: 10.5, fontWeight: 700, letterSpacing: 0.4,
      color: t.textMute, textTransform: 'uppercase'
    }}>{label}</div>);

}

function Row({ t, selected, indent = 0, onClick, children }) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'flex', alignItems: 'center', gap: 7,
        height: 26, padding: `0 8px 0 ${8 + indent * 14}px`,
        margin: '1px 0',
        borderRadius: 6,
        background: selected ? t.selectedRow : hover ? t.hoverRow : 'transparent',
        color: selected ? t.text : t.text,
        fontSize: 12.5,
        cursor: 'pointer',
        userSelect: 'none'
      }}>
      
      {children}
    </div>);

}

function ProjectRow({ t, project, isOpen, selected, onClick, onNewPlan }) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'flex', alignItems: 'center', gap: 7,
        height: 26, padding: '0 6px 0 8px',
        margin: '1px 0',
        borderRadius: 6,
        background: selected ? t.selectedRow : hover ? t.hoverRow : 'transparent',
        color: t.text,
        fontSize: 12.5,
        cursor: 'pointer',
        userSelect: 'none'
      }}>
      
      <span style={{ width: 12, display: 'inline-flex', justifyContent: 'center' }}>
        {isOpen ? Icon.chevD(8, t.textDim) : Icon.chevR(8, t.textDim)}
      </span>
      {Icon.folder(13, t.crest)}
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{project.name}</span>
      <button
        onClick={onNewPlan}
        title="New plan in this project"
        style={{
          width: 22, height: 22, borderRadius: 5,
          border: 'none', background: 'transparent',
          color: t.textDim, cursor: 'pointer',
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          padding: 0, flexShrink: 0
        }}
        onMouseEnter={(e) => {e.currentTarget.style.background = t.hoverRow;e.currentTarget.style.color = t.text;}}
        onMouseLeave={(e) => {e.currentTarget.style.background = 'transparent';e.currentTarget.style.color = t.textDim;}}>
        
        {Icon.compose(14, 'currentColor')}
      </button>
    </div>);

}

function StatusDot({ t, status }) {
  const map = {
    draft: { c: t.textMute, l: 'Draft' },
    refining: { c: t.warn, l: 'Refining' },
    finalized: { c: t.success, l: 'Final' },
    beads: { c: t.accent, l: 'Beads' }
  };
  const { c } = map[status] || map.draft;
  return <div title={map[status]?.l} style={{ width: 6, height: 6, borderRadius: '50%', background: c }} />;
}

// ─────────────────────────────────────────────────────────────
// File tree (workspace view)
// ─────────────────────────────────────────────────────────────
const FILE_TREE = {
  cass: [
    { kind: 'dir', name: 'plans', children: 'plans' }, // placeholder; injected from PLANS
    { kind: 'dir', name: 'src', children: [
      { kind: 'dir', name: 'core', children: [
        { kind: 'file', name: 'memory.rs' },
        { kind: 'file', name: 'embedding.rs' },
        { kind: 'file', name: 'index.rs' },
      ]},
      { kind: 'dir', name: 'store', children: [
        { kind: 'file', name: 'schema.sql' },
        { kind: 'file', name: 'db.rs' },
      ]},
      { kind: 'file', name: 'main.rs' },
    ]},
    { kind: 'dir', name: 'docs', children: [
      { kind: 'file', name: 'architecture.md' },
      { kind: 'file', name: 'AGENTS.md' },
    ]},
    { kind: 'file', name: 'Cargo.toml' },
    { kind: 'file', name: 'README.md' },
  ],
  frank: [
    { kind: 'dir', name: 'plans', children: 'plans' },
    { kind: 'dir', name: 'cmd', children: [
      { kind: 'file', name: 'frank.go' },
    ]},
    { kind: 'dir', name: 'internal', children: [
      { kind: 'file', name: 'parser.go' },
      { kind: 'file', name: 'engine.go' },
    ]},
    { kind: 'file', name: 'go.mod' },
    { kind: 'file', name: 'AGENTS.md' },
  ],
  atlas: [
    { kind: 'dir', name: 'plans', children: 'plans' },
    { kind: 'dir', name: 'src', children: [
      { kind: 'dir', name: 'architecture', children: [
        { kind: 'file', name: 'ipc.md' },
        { kind: 'file', name: 'sidecar.md' },
      ]},
      { kind: 'dir', name: 'app', children: [
        { kind: 'file', name: 'shell.tsx' },
        { kind: 'file', name: 'sidebar.tsx' },
      ]},
    ]},
    { kind: 'dir', name: 'core', children: [
      { kind: 'dir', name: 'plan-store', children: [
        { kind: 'file', name: 'mod.rs' },
      ]},
      { kind: 'dir', name: 'store', children: [
        { kind: 'file', name: 'schema.sql' },
      ]},
    ]},
    { kind: 'file', name: 'package.json' },
    { kind: 'file', name: 'AGENTS.md' },
    { kind: 'file', name: 'README.md' },
  ],
};

function FileTree({ t, project, plans, planId, onSelectPlan }) {
  const tree = FILE_TREE[project.id] || [];
  return (
    <div>
      {tree.map((node, i) => (
        <FileNode
          key={i} t={t} node={node} indent={1}
          plans={plans} planId={planId} onSelectPlan={onSelectPlan}
        />
      ))}
    </div>
  );
}

function FileNode({ t, node, indent, plans, planId, onSelectPlan }) {
  // Auto-expand the plans dir so plan files are visible
  const initOpen = node.kind === 'dir' && node.children === 'plans';
  const [open, setOpen] = useState(initOpen);

  if (node.kind === 'file') {
    return (
      <Row t={t} indent={indent} onClick={() => {}}>
        <span style={{ width: 12 }} />
        {Icon.doc(11, t.textDim)}
        <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: t.textDim }}>
          {node.name}
        </span>
      </Row>
    );
  }

  // dir
  const children = node.children === 'plans' ? null : node.children;

  return (
    <>
      <Row t={t} indent={indent} onClick={() => setOpen((o) => !o)}>
        <span style={{ width: 12, display: 'inline-flex', justifyContent: 'center' }}>
          {open ? Icon.chevD(8, t.textDim) : Icon.chevR(8, t.textDim)}
        </span>
        {Icon.folder(11, node.children === 'plans' ? t.crest : t.textDim)}
        <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
          {node.name}
        </span>
      </Row>
      {open && node.children === 'plans' && plans.map((pl) => (
        <Row
          key={pl.id} t={t}
          selected={pl.id === planId}
          indent={indent + 1}
          onClick={() => onSelectPlan(pl.id)}
        >
          <span style={{ width: 12 }} />
          {Icon.doc(11, t.textDim)}
          <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{pl.name}</span>
          <StatusDot t={t} status={pl.status} />
        </Row>
      ))}
      {open && children && children.map((child, i) => (
        <FileNode
          key={i} t={t} node={child} indent={indent + 1}
          plans={plans} planId={planId} onSelectPlan={onSelectPlan}
        />
      ))}
    </>
  );
}

// ─────────────────────────────────────────────────────────────
// Main column
// ─────────────────────────────────────────────────────────────
function Main({ t, project, plan, view, setView, wizardStep, setWizardStep, onStartWizard, swarmLaunched, setSwarmLaunched }) {
  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', minWidth: 0 }}>
      <Toolbar t={t} project={project} plan={plan} view={view} setView={setView} />
      <div style={{
        flex: 1, overflow: 'auto',
        background: t.bg,
        position: 'relative'
      }}>
        {view === 'plan' && <PlanOverview t={t} plan={plan} setView={setView} onStartWizard={onStartWizard} />}
        {view === 'wizard' && <Wizard t={t} step={wizardStep} setStep={setWizardStep} setView={setView} />}
        {view === 'compare' && <CompareView t={t} setView={setView} />}
        {view === 'synthesize' && <SynthesizeView t={t} setView={setView} />}
        {view === 'rounds' && <RoundsView t={t} setView={setView} />}
        {view === 'beads' && <BeadsView t={t} setView={setView} onLaunchSwarm={() => { setSwarmLaunched(false); setView('swarm'); }} />}
        {view === 'swarm' && <SwarmView t={t} setView={setView} launched={swarmLaunched} setLaunched={setSwarmLaunched} />}
        {view === 'health' && <CodeHealthView t={t} />}
      </div>
    </div>);

}

function Toolbar({ t, project, plan, view, setView }) {
  const tabs = [
  { id: 'plan', label: 'Plan' },
  { id: 'rounds', label: 'Refinement' },
  { id: 'beads', label: 'Beads' },
  { id: 'swarm', label: 'Swarm' },
  { id: 'health', label: 'Code Health' }];

  const inWizard = ['wizard', 'compare', 'synthesize'].includes(view);

  return (
    <div style={{
      height: 52, flexShrink: 0,
      background: t.sidebar,
      backdropFilter: 'blur(40px) saturate(180%)',
      WebkitBackdropFilter: 'blur(40px) saturate(180%)',
      borderBottom: `0.5px solid ${t.sidebarBorder}`,
      display: 'flex', alignItems: 'center',
      padding: '0 16px', gap: 12
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 1, minWidth: 0 }}>
        <div style={{ fontSize: 11, color: t.textDim, display: 'flex', gap: 4, alignItems: 'center' }}>
          <span>{project?.name}</span>
          {Icon.chevR(8, t.textMute)}
          <span>{plan?.name}</span>
        </div>
        <div style={{ fontSize: 14, fontWeight: 700, letterSpacing: -0.2 }}>
          {inWizard ? 'New Plan' : plan?.name || 'No plan selected'}
        </div>
      </div>

      <div style={{ flex: 1 }} />

      {!inWizard &&
      <div style={{
        display: 'flex', gap: 2,
        background: t.panelAlt, borderRadius: 7, padding: 2,
        border: `0.5px solid ${t.borderSoft}`
      }}>
          {tabs.map((tb) =>
        <button
          key={tb.id}
          onClick={() => setView(tb.id)}
          style={{
            height: 24, padding: '0 10px', borderRadius: 5,
            background: view === tb.id ? t.panel : 'transparent',
            border: 'none',
            color: view === tb.id ? t.text : t.textDim,
            fontFamily: FONT, fontSize: 12, fontWeight: 500,
            cursor: 'pointer',
            boxShadow: view === tb.id ? t.shadow : 'none'
          }}>
          {tb.label}</button>
        )}
        </div>
      }
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Plan Overview
// ─────────────────────────────────────────────────────────────
function PlanOverview({ t, plan, setView, onStartWizard }) {
  if (!plan) {
    return <EmptyState t={t} onStartWizard={onStartWizard} />;
  }

  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 18 }}>
      {/* Status banner */}
      <div style={{
        background: t.panel,
        border: `0.5px solid ${t.border}`,
        borderRadius: 10,
        padding: 16,
        display: 'flex', gap: 16, alignItems: 'center',
        boxShadow: t.shadow
      }}>
        <PlanStatusBadge t={t} status={plan.status} />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: 14, fontWeight: 600 }}>{statusCopy(plan.status).title}</div>
          <div style={{ fontSize: 12, color: t.textDim, marginTop: 2 }}>{statusCopy(plan.status).body}</div>
        </div>
        {plan.status === 'finalized' || plan.status === 'beads' ? (
          <button onClick={() => setView('beads')} style={pillBtn(t, 'primary')}>
            {Icon.spark(11, '#fff')}
            <span>{plan.status === 'beads' ? 'Open beads' : 'Generate beads'}</span>
            {Icon.chevR(9, '#fff')}
          </button>
        ) : (
          <button onClick={() => setView('rounds')} style={pillBtn(t, 'primary')}>
            <span>Continue refining</span>
            {Icon.chevR(9, '#fff')}
          </button>
        )}
      </div>

      {/* Stats grid */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 10 }}>
        <Stat t={t} label="Plan length" value={plan.lines.toLocaleString()} suffix="lines" />
        <Stat t={t} label="Refinement rounds" value={plan.rounds} suffix={plan.rounds === 1 ? 'pass' : 'passes'} />
        <Stat t={t} label="Models contributed" value="4" suffix="drafts merged" />
        <Stat t={t} label="Convergence" value="78%" suffix="content similarity" />
      </div>

      {/* Two-column: timeline + actions */}
      <div style={{ display: 'grid', gridTemplateColumns: '1.4fr 1fr', gap: 14 }}>
        <Card t={t} title="Plan lifecycle">
          <Lifecycle t={t} status={plan.status} />
        </Card>
        <Card t={t} title="Actions">
          <ActionRow t={t} icon={Icon.spark(12, t.crest)} title="Run another refinement round" sub="Round 4 ready · Chat GPT Pro · fresh context" onClick={() => setView('rounds')} />
          <ActionRow t={t} icon={Icon.doc(12, t.accent)} title="View competing drafts" sub="Compare 4 model outputs side by side" onClick={() => setView('compare')} />
          <ActionRow
            t={t}
            icon={Icon.check(12, t.success)}
            title={plan.status === 'beads' ? 'Open bead graph' : 'Generate beads from plan'}
            sub={plan.status === 'beads' ? '24 beads · 4 rounds refined' : 'Carve plan into atomic beads · planner models will refine'}
            onClick={() => setView('beads')}
          />
        </Card>
      </div>

      {/* Plan excerpt */}
      <Card t={t} title="Plan document" right={<span style={{ fontSize: 11, color: t.textDim, fontFamily: MONO }}>last edited 2m ago</span>}>
        <div style={{
          fontFamily: MONO, fontSize: 12.5, lineHeight: 1.65,
          color: t.text, padding: '4px 2px',
          maxHeight: 220, overflow: 'auto'
        }}>
          <div style={{ color: t.crest, fontWeight: 700 }}># {plan.name}</div>
          <br />
          <div style={{ color: t.textDim }}>## 1. Goals & Non-Goals</div>
          <div>The plan-centric desktop app coordinates an agent swarm through</div>
          <div>plan creation, bead conversion, and supervised execution.</div>
          <br />
          <div style={{ color: t.textDim }}>## 2. Architecture</div>
          <div>- Electron shell (main + renderer)</div>
          <div>- Local SQLite for plan + bead state</div>
          <div>- MCP Agent Mail bridge process</div>
          <div>- bv as a sidecar binary, queried via robot-mode flags</div>
          <br />
          <div style={{ color: t.textDim }}>## 3. User Workflows</div>
          <div style={{ color: t.textDim, fontStyle: 'italic' }}>(continues for {(plan.lines - 12).toLocaleString()} more lines)</div>
        </div>
      </Card>
    </div>);

}

function statusCopy(s) {
  return {
    draft: { title: 'Draft plan — start the multi-model loop', body: 'Run drafts from competing models, then synthesize.' },
    refining: { title: 'Refining plan — round 4 in progress', body: 'Convergence at 78%. Recommended: 1–2 more rounds.' },
    finalized: { title: 'Plan finalized', body: 'Ready for bead conversion. Convert when ready.' },
    beads: { title: 'Beads generated', body: 'Plan converted; 347 beads in graph.' }
  }[s] || { title: '', body: '' };
}

function PlanStatusBadge({ t, status }) {
  const map = {
    draft: { bg: t.panelAlt, fg: t.textDim, l: 'Draft' },
    refining: { bg: 'rgba(233,150,63,0.15)', fg: t.warn, l: 'Refining' },
    finalized: { bg: 'rgba(48,182,107,0.15)', fg: t.success, l: 'Finalized' },
    beads: { bg: t.accentSoft, fg: t.accent, l: 'Beads' }
  };
  const { bg, fg, l } = map[status] || map.draft;
  return (
    <div style={{
      padding: '4px 10px', borderRadius: 999,
      background: bg, color: fg, fontSize: 11, fontWeight: 600,
      textTransform: 'uppercase', letterSpacing: 0.3
    }}>{l}</div>);

}

function Stat({ t, label, value, suffix }) {
  return (
    <div style={{
      background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
      padding: '12px 14px', boxShadow: t.shadow
    }}>
      <div style={{ fontSize: 10.5, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>{label}</div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 4, marginTop: 6 }}>
        <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, fontVariantNumeric: 'tabular-nums' }}>{value}</div>
        <div style={{ fontSize: 11, color: t.textDim }}>{suffix}</div>
      </div>
    </div>);

}

function Card({ t, title, children, right }) {
  return (
    <div style={{
      background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
      boxShadow: t.shadow, overflow: 'hidden'
    }}>
      <div style={{
        padding: '11px 14px',
        borderBottom: `0.5px solid ${t.borderSoft}`,
        display: 'flex', alignItems: 'center', justifyContent: 'space-between'
      }}>
        <div style={{ fontSize: 12, fontWeight: 600 }}>{title}</div>
        {right}
      </div>
      <div style={{ padding: 8 }}>{children}</div>
    </div>);

}

function ActionRow({ t, icon, title, sub, onClick, disabled }) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onClick={disabled ? undefined : onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'flex', alignItems: 'center', gap: 10,
        padding: '8px 8px', borderRadius: 7,
        background: hover && !disabled ? t.hoverRow : 'transparent',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1
      }}>
      <div style={{
        width: 26, height: 26, borderRadius: 7,
        background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
        display: 'flex', alignItems: 'center', justifyContent: 'center'
      }}>{icon}</div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ fontSize: 12.5, fontWeight: 500 }}>{title}</div>
        <div style={{ fontSize: 11, color: t.textDim }}>{sub}</div>
      </div>
      {!disabled && Icon.chevR(9, t.textMute)}
    </div>);

}

function Lifecycle({ t, status }) {
  const phases = [
  { id: 'draft', label: 'Initial draft' },
  { id: 'refining', label: 'Refinement rounds' },
  { id: 'finalized', label: 'Finalized' },
  { id: 'beads', label: 'Bead conversion' }];

  const order = ['draft', 'refining', 'finalized', 'beads'];
  const idx = order.indexOf(status);
  return (
    <div style={{ padding: '8px 6px' }}>
      <div style={{ display: 'flex', gap: 0, alignItems: 'flex-start' }}>
        {phases.map((p, i) => {
          const done = i < idx;
          const active = i === idx;
          return (
            <React.Fragment key={p.id}>
              <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 6, flex: 'none' }}>
                <div style={{
                  width: 24, height: 24, borderRadius: '50%',
                  background: done ? t.success : active ? t.crest : t.panelAlt,
                  border: `0.5px solid ${done || active ? 'transparent' : t.border}`,
                  color: '#fff', fontSize: 11, fontWeight: 700,
                  display: 'flex', alignItems: 'center', justifyContent: 'center'
                }}>
                  {done ? Icon.check(12, '#fff') : active ? <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#fff' }} /> : <span style={{ color: t.textMute }}>{i + 1}</span>}
                </div>
                <div style={{
                  fontSize: 11, fontWeight: active ? 600 : 500,
                  color: active ? t.text : done ? t.text : t.textDim,
                  width: 80, textAlign: 'center'
                }}>{p.label}</div>
              </div>
              {i < phases.length - 1 &&
              <div style={{
                flex: 1, height: 2,
                marginTop: 11,
                background: i < idx ? t.success : t.borderSoft,
                borderRadius: 1
              }} />
              }
            </React.Fragment>);

        })}
      </div>
    </div>);

}

function pillBtn(t, kind = 'default') {
  const map = {
    primary: { bg: t.crest, fg: '#fff', border: 'transparent' },
    default: { bg: t.panel, fg: t.text, border: t.border },
    ghost: { bg: 'transparent', fg: t.text, border: t.border }
  }[kind];
  return {
    height: 28, padding: '0 12px', borderRadius: 7,
    background: map.bg, color: map.fg,
    border: `0.5px solid ${map.border}`,
    fontFamily: FONT, fontSize: 12, fontWeight: 600,
    display: 'inline-flex', alignItems: 'center', gap: 6,
    cursor: 'pointer',
    boxShadow: kind === 'primary' ? '0 1px 0 rgba(255,255,255,0.2) inset, 0 1px 1px rgba(0,0,0,0.1)' : 'none'
  };
}

function EmptyState({ t, onStartWizard }) {
  return (
    <div style={{
      height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center',
      flexDirection: 'column', gap: 14, color: t.textDim
    }}>
      <HoopoeCrest size={48} color={t.textDim} accent={t.crest} />
      <div style={{ fontSize: 16, fontWeight: 600, color: t.text }}>No plan selected</div>
      <div style={{ fontSize: 12.5 }}>Start a new plan to begin the multi-model synthesis flow.</div>
      <button onClick={onStartWizard} style={pillBtn(t, 'primary')}>
        {Icon.plus(11, '#fff')}<span>New plan</span>
      </button>
    </div>);

}

Object.assign(window, { App });