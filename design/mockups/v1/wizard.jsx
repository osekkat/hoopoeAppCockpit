// Wizard, Compare, Synthesize, Rounds, Code Health views

const FONT2 = '-apple-system, BlinkMacSystemFont, "SF Pro Text", system-ui, sans-serif';
const MONO2 = '"SF Mono", ui-monospace, Menlo, monospace';

// ─────────────────────────────────────────────────────────────
// Wizard — the new-plan flow
// Step 1: chat / brief
// Step 2: model picker
// Step 3: compare drafts
// Step 4: synthesize
// Step 5: refinement rounds
// ─────────────────────────────────────────────────────────────
function Wizard({ t, step, setStep, setView }) {
  const steps = [
  { n: 1, label: 'Brief' },
  { n: 2, label: 'Models' },
  { n: 3, label: 'Compare' },
  { n: 4, label: 'Synthesize' },
  { n: 5, label: 'Refine' }];


  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* Stepper — hidden on step 0 (mode picker) */}
      {step !== 0 && (
      <div style={{
        padding: '14px 28px',
        borderBottom: `0.5px solid ${t.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 8,
        background: t.panelAlt
      }}>
        {steps.map((s, i) => {
          const done = step > s.n;
          const active = step === s.n;
          return (
            <React.Fragment key={s.n}>
              <div
                onClick={() => setStep(s.n)}
                style={{
                  display: 'flex', alignItems: 'center', gap: 6,
                  padding: '4px 10px', borderRadius: 7,
                  cursor: 'pointer',
                  background: active ? t.panel : 'transparent',
                  border: active ? `0.5px solid ${t.border}` : `0.5px solid transparent`,
                  boxShadow: active ? t.shadow : 'none'
                }}>
                <div style={{
                  width: 18, height: 18, borderRadius: '50%',
                  background: done ? t.success : active ? t.crest : t.panelAlt,
                  border: !done && !active ? `0.5px solid ${t.border}` : 'none',
                  color: '#fff', fontSize: 10, fontWeight: 700,
                  display: 'flex', alignItems: 'center', justifyContent: 'center'
                }}>
                  {done ? '✓' : <span style={{ color: !done && !active ? t.textMute : '#fff' }}>{s.n}</span>}
                </div>
                <span style={{
                  fontSize: 12, fontWeight: active ? 600 : 500,
                  color: active ? t.text : t.textDim
                }}>{s.label}</span>
              </div>
              {i < steps.length - 1 &&
              <div style={{ width: 14, height: 1, background: t.borderSoft }} />
              }
            </React.Fragment>);

        })}
        <div style={{ flex: 1 }} />
        <button onClick={() => setView('plan')} style={{
          height: 24, padding: '0 10px', borderRadius: 6,
          background: 'transparent', border: 'none',
          color: t.textDim, fontSize: 12, cursor: 'pointer'
        }}>Cancel</button>
      </div>
      )}

      <div style={{ flex: 1, overflow: 'auto' }}>
        {step === 0 && <StepChoose t={t} onDraft={() => setStep(1)} onImport={() => setStep(99)} onCancel={() => setView('plan')} />}
        {step === 99 && <StepImport t={t} onBack={() => setStep(0)} onFinish={() => { setStep(5); }} />}
        {step === 1 && <StepBrief t={t} onNext={() => setStep(2)} />}
        {step === 2 && <StepModels t={t} onNext={() => setStep(3)} onBack={() => setStep(1)} />}
        {step === 3 && <StepCompare t={t} onNext={() => setStep(4)} onBack={() => setStep(2)} />}
        {step === 4 && <StepSynthesize t={t} onNext={() => setStep(5)} onBack={() => setStep(3)} />}
        {step === 5 && <StepRounds t={t} onFinish={() => setView('plan')} onBack={() => setStep(4)} />}
      </div>
    </div>);

}

// ─────────── Step 0: Mode chooser (Import vs Draft) ───────────
function StepChoose({ t, onDraft, onImport, onCancel }) {
  return (
    <div style={{ padding: '40px 32px', maxWidth: 880, margin: '0 auto' }}>
      <div style={{ marginBottom: 28 }}>
        <div style={{ fontSize: 11, color: t.textDim, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>New plan</div>
        <div style={{ fontSize: 24, fontWeight: 700, letterSpacing: -0.4, marginTop: 4 }}>How are you starting this plan?</div>
        <div style={{ fontSize: 13, color: t.textDim, marginTop: 6, lineHeight: 1.55, maxWidth: 620 }}>
          Bring an existing plan document into Hoopoe, or draft one from scratch with multi-model help.
          Either way, you&rsquo;ll end up at refinement rounds before generating beads.
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
        <ChooseCard
          t={t}
          accent={t.crest}
          tag="Recommended"
          icon={<DraftGlyph color={t.crest} />}
          title="Draft with Hoopoe"
          desc="Walk through the multi-model flywheel: brief → competing drafts → synthesis → refinement. 4–6 rounds typically yields a 3,000–6,000-line plan."
          steps={['Write a brief', 'Pick primary + 3 challenger models', 'Compare drafts', 'Synthesize hybrid', 'Refine 4–6 rounds']}
          onClick={onDraft}
          cta="Start drafting"
        />
        <ChooseCard
          t={t}
          accent={t.accent}
          icon={<ImportGlyph color={t.accent} />}
          title="Import a plan file"
          desc="Already have a plan in Markdown, Notion, or a Doc? Drop it in. Hoopoe will parse the structure and skip straight to refinement rounds."
          steps={['Upload .md / .docx / .pdf', 'Confirm parsed structure', 'Jump to refinement rounds']}
          onClick={onImport}
          cta="Import plan"
        />
      </div>

      <div style={{ marginTop: 24, display: 'flex', alignItems: 'center', gap: 8 }}>
        <button onClick={onCancel} style={{
          height: 28, padding: '0 12px', borderRadius: 7,
          background: 'transparent', border: 'none',
          color: t.textDim, fontSize: 12, cursor: 'pointer',
        }}>Cancel</button>
      </div>
    </div>
  );
}

function ChooseCard({ t, accent, tag, icon, title, desc, steps, onClick, cta }) {
  const [hover, setHover] = useState(false);
  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        textAlign: 'left',
        background: t.panel,
        border: `1px solid ${hover ? accent : t.border}`,
        borderRadius: 12,
        padding: 22,
        cursor: 'pointer',
        boxShadow: hover ? `0 6px 16px rgba(0,0,0,0.06), 0 0 0 3px ${accent}1a` : t.shadow,
        transition: 'all 140ms ease',
        display: 'flex', flexDirection: 'column', gap: 14,
        fontFamily: FONT2,
        position: 'relative',
      }}
    >
      {tag && (
        <span style={{
          position: 'absolute', top: 16, right: 16,
          fontSize: 10, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase',
          color: accent,
          background: `${accent}1a`,
          padding: '3px 8px', borderRadius: 4,
        }}>{tag}</span>
      )}
      <div style={{
        width: 40, height: 40, borderRadius: 9,
        background: `${accent}1a`,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}>{icon}</div>
      <div>
        <div style={{ fontSize: 16, fontWeight: 700, letterSpacing: -0.2, color: t.text }}>{title}</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4, lineHeight: 1.5 }}>{desc}</div>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, marginTop: 4 }}>
        {steps.map((s, i) => (
          <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 11.5, color: t.textDim }}>
            <span style={{ width: 16, height: 16, borderRadius: '50%', background: t.panelAlt, color: t.textMute, fontSize: 9.5, fontWeight: 700, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>{i + 1}</span>
            <span>{s}</span>
          </div>
        ))}
      </div>
      <div style={{ marginTop: 6, display: 'flex', alignItems: 'center', gap: 5, fontSize: 12, color: accent, fontWeight: 600 }}>
        <span>{cta}</span><span>→</span>
      </div>
    </button>
  );
}

function DraftGlyph({ color }) {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
      <path d="M3 16l3-1 9-9-2-2-9 9-1 3z" stroke={color} strokeWidth="1.4" strokeLinejoin="round" fill={`${color}33`} />
      <path d="M12 5l3 3" stroke={color} strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}
function ImportGlyph({ color }) {
  return (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
      <path d="M10 3v9m-3-3l3 3 3-3" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M3.5 14v2A1.5 1.5 0 005 17.5h10a1.5 1.5 0 001.5-1.5v-2" stroke={color} strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

// ─────────── Step 0b: Import (drag-drop stub) ───────────
function StepImport({ t, onBack, onFinish }) {
  const [stage, setStage] = useState('drop'); // 'drop' | 'parsing' | 'review'
  const [filename, setFilename] = useState(null);

  const startMockImport = () => {
    setFilename('hoopoe-spec-v1.md');
    setStage('parsing');
    setTimeout(() => setStage('review'), 1400);
  };

  return (
    <div style={{ padding: '32px 32px', maxWidth: 760, margin: '0 auto' }}>
      <div style={{ marginBottom: 18 }}>
        <button onClick={onBack} style={{
          background: 'transparent', border: 'none', color: t.textDim, fontSize: 12, cursor: 'pointer', padding: 0,
          display: 'inline-flex', alignItems: 'center', gap: 4,
        }}>← Back</button>
        <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, marginTop: 8 }}>Import a plan file</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          Drop a Markdown, Word, or PDF document. Hoopoe will parse the structure and seed the refinement loop.
        </div>
      </div>

      {stage === 'drop' && (
        <div
          onClick={startMockImport}
          style={{
            border: `1.5px dashed ${t.border}`,
            borderRadius: 12,
            padding: '48px 24px',
            background: t.panel,
            textAlign: 'center', cursor: 'pointer',
            display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 12,
          }}
        >
          <div style={{
            width: 48, height: 48, borderRadius: 12,
            background: t.accentSoft,
            display: 'flex', alignItems: 'center', justifyContent: 'center',
          }}><ImportGlyph color={t.accent} /></div>
          <div>
            <div style={{ fontSize: 14, fontWeight: 600, color: t.text }}>Drop a plan file here, or click to browse</div>
            <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 4 }}>Supports .md, .docx, .pdf, .txt — up to 5 MB</div>
          </div>
        </div>
      )}

      {stage === 'parsing' && (
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 12, padding: 20,
          display: 'flex', alignItems: 'center', gap: 14,
        }}>
          <ImportSpinner color={t.accent} />
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, fontWeight: 600 }}>Parsing {filename}…</div>
            <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 2 }}>Detecting headings, sections, and decision points</div>
          </div>
        </div>
      )}

      {stage === 'review' && (
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 12, padding: 20,
          display: 'flex', flexDirection: 'column', gap: 14,
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <div style={{
              width: 32, height: 32, borderRadius: 7,
              background: `${t.success}1a`,
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              color: t.success, fontSize: 16, fontWeight: 700,
            }}>✓</div>
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600 }}>{filename} parsed</div>
              <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 1 }}>2,184 lines · 9 sections · 47 decision points</div>
            </div>
          </div>
          <div style={{ borderTop: `0.5px solid ${t.borderSoft}`, paddingTop: 14, display: 'flex', flexDirection: 'column', gap: 6 }}>
            <div style={{ fontSize: 10.5, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase' }}>Detected sections</div>
            {['§ 1 — Goals & non-goals', '§ 2 — Bead lifecycle', '§ 3 — Multi-model synthesis', '§ 4 — Storage & IPC', '§ 5 — UI surface'].map((s, i) => (
              <div key={i} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: t.text, padding: '4px 0' }}>
                <span style={{ width: 6, height: 6, borderRadius: '50%', background: t.crest }} />
                <span>{s}</span>
              </div>
            ))}
          </div>
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8, marginTop: 4 }}>
            <button onClick={onFinish} style={{
              height: 30, padding: '0 14px', borderRadius: 7,
              background: t.crest, color: '#fff', border: 'none',
              fontSize: 12.5, fontWeight: 600, cursor: 'pointer',
              display: 'inline-flex', alignItems: 'center', gap: 6,
            }}>
              <span>Continue to refinement</span><span>→</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function ImportSpinner({ color }) {
  return (
    <svg width="22" height="22" viewBox="0 0 22 22" style={{ animation: 'wizSpin 0.9s linear infinite' }}>
      <circle cx="11" cy="11" r="8" fill="none" stroke={`${color}33`} strokeWidth="2.5" />
      <path d="M11 3 a8 8 0 0 1 8 8" fill="none" stroke={color} strokeWidth="2.5" strokeLinecap="round" />
    </svg>
  );
}
if (typeof document !== 'undefined' && !document.getElementById('wiz-spin-keyframes')) {
  const style = document.createElement('style');
  style.id = 'wiz-spin-keyframes';
  style.textContent = `@keyframes wizSpin { to { transform: rotate(360deg); } }`;
  document.head.appendChild(style);
}

// ─────────── Step 1: Chat brief ───────────
function StepBrief({ t, onNext }) {
  const [text, setText] = useState(`A desktop coordination layer for an agent swarm. The user creates a plan, runs competing models, synthesizes a hybrid, then refines through 4–5 rounds. After finalizing, the plan is converted to beads (a DAG of self-contained tasks). The swarm of agents then works the beads autonomously while the user monitors progress.`);
  const [stack, setStack] = useState('Electron + TypeScript + Rust sidecars');
  const [kickoff, setKickoff] = useState('clarify');

  const kickoffOptions = [
  {
    id: 'clarify',
    title: 'Ask clarifying questions',
    desc: 'Models interview you to align on goals, constraints, and unknowns before drafting.'
  },
  {
    id: 'draft',
    title: 'Take a first shot',
    desc: 'Models go straight to a draft plan from the brief above. You refine after.'
  }];


  return (
    <div style={{ padding: '24px 28px', maxWidth: 880, display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>What are you building?</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          Describe the project in your own words. The fuzzier and more product-focused, the better.
          Frontier models will turn this into a 3,000–6,000 line plan.
        </div>
      </div>

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, display: 'flex', flexDirection: 'column', gap: 10,
        boxShadow: t.shadow
      }}>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Stream-of-consciousness is fine. Goals, user workflows, why it matters…"
          style={{
            width: '100%', minHeight: 140,
            background: 'transparent', border: 'none', outline: 'none',
            color: t.text, fontFamily: FONT2, fontSize: 13.5, lineHeight: 1.55,
            resize: 'vertical'
          }} />
        
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingTop: 8, borderTop: `0.5px solid ${t.borderSoft}` }}>
          <ChatChip t={t} label="Add context file" />
          <ChatChip t={t} label="Reference repo" />
          <ChatChip t={t} label="Best-practices guide" />
          <div style={{ flex: 1 }} />
          <span style={{ fontSize: 11, color: t.textMute, fontVariantNumeric: 'tabular-nums' }}>
            {text.split(/\s+/).filter(Boolean).length} words
          </span>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
          padding: 14, boxShadow: t.shadow
        }}>
          <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Tech stack</div>
          <input
            value={stack}
            onChange={(e) => setStack(e.target.value)}
            style={{
              width: '100%', marginTop: 6,
              background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
              borderRadius: 6, padding: '6px 8px',
              fontFamily: MONO2, fontSize: 12, color: t.text, outline: 'none'
            }} />
          
        </div>
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
          padding: 14, boxShadow: t.shadow, gridColumn: 'span 2'
        }}>
          <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Kick-off mode</div>
          <div style={{ fontSize: 12, color: t.textDim, marginTop: 4 }}>
            How should the models start?
          </div>
          <div style={{ display: 'flex', gap: 8, marginTop: 10 }}>
            {kickoffOptions.map((o) => {
              const sel = kickoff === o.id;
              return (
                <button key={o.id} onClick={() => setKickoff(o.id)} style={{
                  flex: 1, textAlign: 'left',
                  background: sel ? t.crestSoft : t.panelAlt,
                  border: `1px solid ${sel ? t.crest : t.borderSoft}`,
                  borderRadius: 8, padding: '10px 12px',
                  cursor: 'pointer', fontFamily: FONT2,
                  display: 'flex', flexDirection: 'column', gap: 4
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{
                      width: 14, height: 14, borderRadius: '50%',
                      border: `1.5px solid ${sel ? t.crest : t.borderSoft}`,
                      background: sel ? t.crest : 'transparent',
                      display: 'flex', alignItems: 'center', justifyContent: 'center',
                      flexShrink: 0
                    }}>
                      {sel && <span style={{ width: 5, height: 5, borderRadius: '50%', background: '#fff' }} />}
                    </span>
                    <span style={{ fontSize: 12.5, fontWeight: 600, color: sel ? t.crest : t.text }}>{o.title}</span>
                  </div>
                  <div style={{ fontSize: 11.5, color: t.textDim, lineHeight: 1.4, paddingLeft: 22 }}>{o.desc}</div>
                </button>);

            })}
          </div>
        </div>
      </div>

      <div style={{ display: 'flex', justifyContent: 'flex-end', paddingTop: 4 }}>
        <button onClick={onNext} style={pillBtn2(t, 'primary')}>
          <span>Choose models</span>{Icon.chevR(9, '#fff')}
        </button>
      </div>
    </div>);

}

function ChatChip({ t, label }) {
  return (
    <div style={{
      height: 22, padding: '0 8px', borderRadius: 6,
      background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
      color: t.textDim, fontSize: 11, fontWeight: 500,
      display: 'flex', alignItems: 'center', gap: 4, cursor: 'pointer'
    }}>
      <span style={{ fontSize: 12, lineHeight: 1, color: t.textMute }}>+</span>
      <span>{label}</span>
    </div>);

}

// ─────────── Step 2: Model picker ───────────
function StepModels({ t, onNext, onBack }) {
  const [primary, setPrimary] = useState('gpt-pro');
  const [competing, setCompeting] = useState(['opus', 'gemini', 'grok']);

  const toggleCompeting = (id) => {
    if (id === primary) return;
    setCompeting((c) => c.includes(id) ? c.filter((x) => x !== id) : c.length < 3 ? [...c, id] : c);
  };

  return (
    <div style={{ padding: '24px 28px', maxWidth: 880, display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>Pick models</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          One primary model creates the initial plan. Up to 3 competing models produce alternative drafts.
          Chat GPT Pro recommended as primary for first plans.
        </div>
      </div>

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, boxShadow: t.shadow
      }}>
        <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase', marginBottom: 8 }}>Primary model</div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 8 }}>
          {MODELS.map((m) =>
          <ModelTile
            key={m.id} t={t} model={m}
            selected={primary === m.id}
            onClick={() => {setPrimary(m.id);setCompeting((c) => c.filter((x) => x !== m.id));}}
            variant="primary" />

          )}
        </div>
      </div>

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, boxShadow: t.shadow
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
          <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Competing models · {competing.length} of 3</div>
          <div style={{ fontSize: 11, color: t.textDim }}>Drafts run in parallel</div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, 1fr)', gap: 8 }}>
          {MODELS.filter((m) => m.id !== primary).map((m) =>
          <ModelTile
            key={m.id} t={t} model={m}
            selected={competing.includes(m.id)}
            onClick={() => toggleCompeting(m.id)}
            variant="competing" />

          )}
        </div>
      </div>

      <div style={{
        padding: 12, borderRadius: 9,
        background: t.crestSoft, border: `0.5px solid ${t.crestSoft}`,
        display: 'flex', gap: 10, alignItems: 'flex-start'
      }}>
        <div style={{ marginTop: 1 }}>{Icon.spark(13, t.crest)}</div>
        <div style={{ fontSize: 12, lineHeight: 1.55, color: t.text }}>
          <b>Recommended for first plans.</b> Use Chat GPT Pro as primary, then ask 3 competing models
          for alternative drafts. After all four return, Chat GPT Pro will synthesize a "best-of-all-worlds" hybrid.
        </div>
      </div>

      <div style={{ display: 'flex', justifyContent: 'space-between', paddingTop: 4 }}>
        <button onClick={onBack} style={pillBtn2(t, 'ghost')}>Back</button>
        <button onClick={onNext} style={pillBtn2(t, 'primary')}>
          <span>Run drafts ({1 + competing.length})</span>{Icon.chevR(9, '#fff')}
        </button>
      </div>
    </div>);

}

function ModelTile({ t, model, selected, onClick, variant }) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onClick={onClick}
      onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}
      style={{
        padding: 12, borderRadius: 9,
        background: selected ? variant === 'primary' ? t.crestSoft : t.accentSoft : t.panelAlt,
        border: `0.5px solid ${selected ? variant === 'primary' ? t.crest : t.accent : t.borderSoft}`,
        cursor: 'pointer',
        display: 'flex', gap: 10, alignItems: 'flex-start'
      }}>
      <div style={{
        width: 28, height: 28, borderRadius: 7,
        background: model.dot, opacity: 0.9, flexShrink: 0,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        color: '#fff', fontWeight: 700, fontSize: 12
      }}>{model.name[0]}</div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <span style={{ fontSize: 13, fontWeight: 600 }}>{model.name}</span>
          {model.recommended && variant === 'primary' &&
          <span style={{
            fontSize: 9.5, fontWeight: 700, padding: '2px 6px', borderRadius: 999,
            background: t.crest, color: '#fff', letterSpacing: 0.3
          }}>RECOMMENDED</span>
          }
        </div>
        <div style={{ fontSize: 11, color: t.textDim, marginTop: 2 }}>{model.vendor} · {model.tag}</div>
      </div>
      <div style={{
        width: 16, height: 16, borderRadius: '50%',
        background: selected ? variant === 'primary' ? t.crest : t.accent : 'transparent',
        border: `1px solid ${selected ? 'transparent' : t.border}`,
        display: 'flex', alignItems: 'center', justifyContent: 'center'
      }}>
        {selected && Icon.check(10, '#fff')}
      </div>
    </div>);

}

// ─────────── Step 3: Compare drafts ───────────
function StepCompare({ t, onNext, onBack }) {
  const [active, setActive] = useState('gpt-pro');
  const [progress, setProgress] = useState({ 'gpt-pro': 100, opus: 100, gemini: 100, grok: 100 });
  const [generating, setGenerating] = useState(false);

  // Simulate generation on mount
  useEffect(() => {
    setGenerating(true);
    setProgress({ 'gpt-pro': 0, opus: 0, gemini: 0, grok: 0 });
    const interval = setInterval(() => {
      setProgress((p) => {
        const next = { ...p };
        let done = true;
        Object.keys(next).forEach((k) => {
          if (next[k] < 100) {
            next[k] = Math.min(100, next[k] + 8 + Math.random() * 6);
            if (next[k] < 100) done = false;
          }
        });
        if (done) {
          clearInterval(interval);
          setGenerating(false);
        }
        return next;
      });
    }, 220);
    return () => clearInterval(interval);
  }, []);

  const draft = PLAN_DRAFTS[active];

  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 14, height: '100%' }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>
          {generating ? 'Generating drafts…' : 'Compare drafts'}
        </div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          {generating ?
          'Each model is producing an independent plan. This usually takes a few minutes.' :
          'Read each draft, mark strengths, and continue to synthesis when ready.'}
        </div>
      </div>

      {/* Model tabs with progress */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 8 }}>
        {MODELS.map((m) => {
          const pct = progress[m.id] ?? 100;
          const isActive = active === m.id;
          const ready = pct >= 100;
          return (
            <div
              key={m.id}
              onClick={() => ready && setActive(m.id)}
              style={{
                padding: 10, borderRadius: 9,
                background: isActive ? t.panel : t.panelAlt,
                border: `0.5px solid ${isActive ? t.border : t.borderSoft}`,
                cursor: ready ? 'pointer' : 'default',
                opacity: ready ? 1 : 0.85,
                position: 'relative', overflow: 'hidden',
                boxShadow: isActive ? t.shadow : 'none'
              }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <div style={{ width: 8, height: 8, borderRadius: '50%', background: m.dot }} />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 600 }}>{m.name}</div>
                  <div style={{ fontSize: 10.5, color: t.textDim }}>
                    {ready ? `${PLAN_DRAFTS[m.id].lines.toLocaleString()} lines · ready` : `Drafting… ${Math.round(pct)}%`}
                  </div>
                </div>
                {ready &&
                <div style={{ color: t.success }}>{Icon.check(11, t.success)}</div>
                }
              </div>
              {!ready &&
              <div style={{ height: 2, background: t.borderSoft, marginTop: 8, borderRadius: 1, overflow: 'hidden' }}>
                  <div style={{ width: `${pct}%`, height: '100%', background: m.dot, transition: 'width 200ms' }} />
                </div>
              }
            </div>);

        })}
      </div>

      {/* Active draft view */}
      {!generating && draft &&
      <div style={{
        flex: 1, display: 'grid', gridTemplateColumns: '1fr 280px', gap: 12, minHeight: 0
      }}>
          <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
          boxShadow: t.shadow, display: 'flex', flexDirection: 'column', overflow: 'hidden'
        }}>
            <div style={{
            padding: '10px 14px', borderBottom: `0.5px solid ${t.borderSoft}`,
            display: 'flex', alignItems: 'center', gap: 8
          }}>
              <span style={{ fontSize: 12.5, fontWeight: 600 }}>{MODELS.find((m) => m.id === active).name} draft</span>
              <span style={{ fontSize: 11, color: t.textDim, fontFamily: MONO2 }}>
                · {draft.lines.toLocaleString()} lines · {draft.sections} sections
              </span>
              <div style={{ flex: 1 }} />
              <button style={pillBtnSm(t, 'ghost')}>Open full</button>
            </div>
            <div style={{
            flex: 1, overflow: 'auto', padding: '12px 16px',
            fontFamily: MONO2, fontSize: 12.5, lineHeight: 1.65, color: t.text,
            whiteSpace: 'pre-wrap'
          }}>{draft.excerpt}
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            <div style={{
            background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
            padding: 12, boxShadow: t.shadow
          }}>
              <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Headline</div>
              <div style={{ fontSize: 12.5, marginTop: 6, lineHeight: 1.5 }}>{draft.headline}</div>
            </div>
            <div style={{
            background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
            padding: 12, boxShadow: t.shadow
          }}>
              <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Strengths</div>
              <div style={{ marginTop: 6, display: 'flex', flexDirection: 'column', gap: 4 }}>
                {draft.strengths.map((s) =>
              <div key={s} style={{ fontSize: 12, display: 'flex', gap: 6, alignItems: 'center' }}>
                    <span style={{ color: t.success }}>{Icon.check(11, t.success)}</span>
                    <span>{s}</span>
                  </div>
              )}
              </div>
            </div>
            <div style={{
            background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
            padding: 12, boxShadow: t.shadow
          }}>
              <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>Blind spots</div>
              <div style={{ marginTop: 6, display: 'flex', flexDirection: 'column', gap: 4 }}>
                {draft.blindspots.map((s) =>
              <div key={s} style={{ fontSize: 12, color: t.textDim }}>· {s}</div>
              )}
              </div>
            </div>
          </div>
        </div>
      }

      {generating &&
      <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: t.textDim, fontSize: 12 }}>
          Drafting in parallel — switch tabs as they finish.
        </div>
      }

      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button onClick={onBack} style={pillBtn2(t, 'ghost')}>Back</button>
        <button onClick={onNext} disabled={generating} style={{ ...pillBtn2(t, 'primary'), opacity: generating ? 0.5 : 1 }}>
          <span>Synthesize hybrid</span>{Icon.chevR(9, '#fff')}
        </button>
      </div>
    </div>);

}

// ─────────── Step 4: Synthesize ───────────
function StepSynthesize({ t, onNext, onBack }) {
  const [running, setRunning] = useState(true);
  const [stage, setStage] = useState(0);
  const stages = [
  { label: 'Reading all four drafts', detail: 'Chat GPT Pro · 18,213 lines total' },
  { label: 'Identifying complementary ideas', detail: 'Cross-referencing architectural decisions' },
  { label: 'Producing git-diff style revisions', detail: '47 additions, 12 reframings' },
  { label: 'Integrating into hybrid plan', detail: 'Claude Code applying patches' },
  { label: 'Synthesis complete', detail: '4,820 → 5,547 lines · best-of-all-worlds hybrid ready' }];


  useEffect(() => {
    if (stage < stages.length - 1) {
      const t = setTimeout(() => setStage((s) => s + 1), 1100);
      return () => clearTimeout(t);
    } else {
      setRunning(false);
    }
  }, [stage]);

  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>Best-of-all-worlds synthesis</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          Chat GPT Pro reads all four competing drafts, identifies complementary ideas, and produces a single
          hybrid plan that blends the strongest elements from each.
        </div>
      </div>

      {/* Synthesis flow visualization */}
      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 18, boxShadow: t.shadow,
        display: 'flex', alignItems: 'center', gap: 14, justifyContent: 'center'
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {MODELS.map((m, i) =>
          <div key={m.id} style={{
            display: 'flex', alignItems: 'center', gap: 6,
            padding: '4px 8px', borderRadius: 6,
            background: t.panelAlt,
            border: `0.5px solid ${t.borderSoft}`,
            fontSize: 11.5,
            opacity: stage >= 0 ? 1 : 0.4
          }}>
              <div style={{ width: 6, height: 6, borderRadius: '50%', background: m.dot }} />
              <span>{m.name}</span>
              <span style={{ color: t.textMute, fontFamily: MONO2, fontSize: 10.5 }}>{PLAN_DRAFTS[m.id].lines.toLocaleString()}L</span>
            </div>
          )}
        </div>

        {/* Arrow flow */}
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
          <SynthArrow t={t} active={stage >= 1} />
        </div>

        <div style={{
          padding: '12px 16px', borderRadius: 10,
          background: t.crestSoft, border: `0.5px solid ${t.crest}`,
          display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4,
          minWidth: 120
        }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 4, color: t.crest, fontSize: 11, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>
            {Icon.spark(11, t.crest)}<span>Chat GPT Pro</span>
          </div>
          <div style={{ fontSize: 12, color: t.text, fontWeight: 500 }}>Arbiter</div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
          <SynthArrow t={t} active={stage >= 2} />
        </div>

        <div style={{
          padding: '14px 18px', borderRadius: 10,
          background: stage >= 4 ? 'rgba(48,182,107,0.15)' : t.panelAlt,
          border: `0.5px solid ${stage >= 4 ? t.success : t.borderSoft}`,
          display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4,
          minWidth: 130
        }}>
          <div style={{ fontSize: 11, fontWeight: 600, color: stage >= 4 ? t.success : t.textDim, letterSpacing: 0.3, textTransform: 'uppercase' }}>Hybrid plan</div>
          <div style={{ fontSize: 18, fontWeight: 700, fontVariantNumeric: 'tabular-nums', color: t.text }}>
            {stage >= 4 ? '5,547' : '—'}
          </div>
          <div style={{ fontSize: 10.5, color: t.textDim }}>lines</div>
        </div>
      </div>

      {/* Live log */}
      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, boxShadow: t.shadow
      }}>
        <div style={{ fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase', marginBottom: 8 }}>Synthesis pipeline</div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {stages.map((s, i) => {
            const done = i < stage;
            const active = i === stage && running;
            const pending = i > stage;
            return (
              <div key={i} style={{
                display: 'flex', alignItems: 'center', gap: 10,
                padding: '6px 8px', borderRadius: 6,
                opacity: pending ? 0.4 : 1
              }}>
                <div style={{
                  width: 18, height: 18, borderRadius: '50%',
                  background: done ? t.success : active ? t.crest : 'transparent',
                  border: pending ? `1px solid ${t.border}` : 'none',
                  display: 'flex', alignItems: 'center', justifyContent: 'center'
                }}>
                  {done && Icon.check(10, '#fff')}
                  {active && <Spinner color="#fff" />}
                </div>
                <div style={{ fontSize: 12.5, fontWeight: active ? 600 : 500 }}>{s.label}</div>
                <div style={{ flex: 1 }} />
                <div style={{ fontSize: 11, color: t.textDim, fontFamily: MONO2 }}>{s.detail}</div>
              </div>);

          })}
        </div>
      </div>

      {/* Hybrid plan preview — appears when synthesis completes */}
      {stage >= 4 &&
      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        boxShadow: t.shadow, overflow: 'hidden'
      }}>
          <div style={{
          padding: '12px 16px',
          borderBottom: `0.5px solid ${t.borderSoft}`,
          display: 'flex', alignItems: 'center', gap: 10,
          background: t.panelAlt
        }}>
            <div style={{ width: 8, height: 8, borderRadius: '50%', background: t.success }} />
            <div style={{ fontSize: 12.5, fontWeight: 600 }}>Hybrid plan ready</div>
            <div style={{ fontSize: 11, color: t.textDim, fontFamily: MONO2 }}>plan.hybrid.md · 5,547 lines · 18 sections</div>
            <div style={{ flex: 1 }} />
            <button style={{
            height: 22, padding: '0 8px', borderRadius: 5,
            border: `0.5px solid ${t.borderSoft}`, background: t.panel,
            color: t.textDim, fontFamily: FONT2, fontSize: 11, cursor: 'pointer',
            display: 'flex', alignItems: 'center', gap: 4
          }}>{Icon.chevR(8, t.textDim)}<span>Open in editor</span></button>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: '180px 1fr', minHeight: 220 }}>
            {/* TOC with provenance */}
            <div style={{
            borderRight: `0.5px solid ${t.borderSoft}`,
            padding: '10px 0',
            fontSize: 11.5,
            background: t.panelAlt
          }}>
              {[
            { n: '1', t: 'Architecture overview', m: ['gpt-pro', 'opus'] },
            { n: '2', t: 'Bead lifecycle', m: ['opus'], active: true },
            { n: '3', t: 'Agent coordination', m: ['gpt-pro', 'gemini'] },
            { n: '4', t: 'Synthesis protocol', m: ['gpt-pro'] },
            { n: '5', t: 'Refinement rounds', m: ['gemini', 'grok'] },
            { n: '6', t: 'Code health gates', m: ['opus', 'gpt-pro'] },
            { n: '7', t: 'Failure modes', m: ['grok'] }].
            map((s, i) =>
            <div key={i} style={{
              display: 'flex', alignItems: 'center', gap: 6,
              padding: '5px 12px',
              background: s.active ? t.crestSoft : 'transparent',
              borderLeft: `2px solid ${s.active ? t.crest : 'transparent'}`,
              cursor: 'pointer'
            }}>
                  <span style={{ color: t.textMute, fontVariantNumeric: 'tabular-nums', minWidth: 12 }}>{s.n}</span>
                  <span style={{ flex: 1, color: s.active ? t.text : t.textDim, fontWeight: s.active ? 600 : 500, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{s.t}</span>
                  <span style={{ display: 'flex', gap: 2 }}>
                    {s.m.map((mid) => {
                  const m = MODELS.find((x) => x.id === mid);
                  return <span key={mid} title={m?.name} style={{ width: 5, height: 5, borderRadius: '50%', background: m?.dot }} />;
                })}
                  </span>
                </div>
            )}
            </div>

            {/* Plan body excerpt */}
            <div style={{
            padding: '14px 18px',
            fontFamily: MONO2, fontSize: 11.5, lineHeight: 1.65,
            color: t.text, overflow: 'auto', maxHeight: 280
          }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
                <span style={{ fontFamily: FONT2, fontSize: 13.5, fontWeight: 700, color: t.text }}>§ 2 — Bead lifecycle</span>
                <ProvenanceBadge t={t} model="opus" label="Primary: Opus" />
                <ProvenanceBadge t={t} model="gpt-pro" label="+ Chat GPT Pro refinements" />
              </div>

              <div style={{ color: t.textDim }}># 2. Bead lifecycle</div>
              <div style={{ marginTop: 6 }}>
                A <span style={{ color: t.crest, fontWeight: 600 }}>bead</span> is the smallest reviewable unit
                of progress — a tightly-scoped task that an agent can complete in one cycle. Beads thread into
                chains; chains assemble into a finalized plan.
              </div>
              <div style={{ marginTop: 8, padding: '6px 10px', background: 'rgba(48,182,107,0.10)', borderLeft: `2px solid ${t.success}`, borderRadius: 3 }}>
                <span style={{ color: t.success, fontWeight: 600 }}>+ </span>
                <span>Each bead carries a provenance trail noting which model authored which decision.</span>
                <span style={{ color: t.textMute, marginLeft: 6, fontSize: 10.5 }}>(merged from Chat GPT Pro)</span>
              </div>
              <div style={{ marginTop: 8 }}>States: <span style={{ color: t.accent }}>draft</span> → <span style={{ color: t.warn }}>refining</span> → <span style={{ color: t.success }}>finalized</span> → <span style={{ color: t.crest }}>beads</span>.</div>
              <div style={{ marginTop: 8, padding: '6px 10px', background: 'rgba(229,130,83,0.10)', borderLeft: `2px solid ${t.crest}`, borderRadius: 3 }}>
                <span style={{ color: t.crest, fontWeight: 600 }}>~ </span>
                <span>Reframed: beads are append-only; revisions create successor beads rather than mutating in place.</span>
                <span style={{ color: t.textMute, marginLeft: 6, fontSize: 10.5 }}>(reframed from Opus)</span>
              </div>
              <div style={{ marginTop: 8, color: t.textMute, fontStyle: 'italic' }}>… continued in plan.hybrid.md (lines 412–698)</div>
            </div>
          </div>
        </div>
      }

      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button onClick={onBack} style={pillBtn2(t, 'ghost')}>Back</button>
        <button onClick={onNext} disabled={running} style={{ ...pillBtn2(t, 'primary'), opacity: running ? 0.5 : 1 }}>
          <span>Begin refinement rounds</span>{Icon.chevR(9, '#fff')}
        </button>
      </div>
    </div>);

}

function ProvenanceBadge({ t, model, label }) {
  const m = MODELS.find((x) => x.id === model);
  if (!m) return null;
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 4,
      height: 18, padding: '0 6px', borderRadius: 4,
      background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
      fontFamily: FONT2, fontSize: 10.5, color: t.textDim, fontWeight: 500
    }}>
      <span style={{ width: 5, height: 5, borderRadius: '50%', background: m.dot }} />
      <span>{label}</span>
    </span>);

}

function SynthArrow({ t, active }) {
  return (
    <svg width="40" height="14" viewBox="0 0 40 14">
      <line x1="2" y1="7" x2="34" y2="7" stroke={active ? t.crest : t.border} strokeWidth="1.5" strokeDasharray={active ? '0' : '3 3'} />
      <path d="M30 3 L36 7 L30 11" stroke={active ? t.crest : t.border} strokeWidth="1.5" fill="none" strokeLinecap="round" strokeLinejoin="round" />
    </svg>);

}

function Spinner({ color = '#fff' }) {
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" style={{ animation: 'spin 0.8s linear infinite' }}>
      <circle cx="5" cy="5" r="3.5" stroke={color} strokeWidth="1.4" fill="none" strokeDasharray="6 22" strokeLinecap="round" />
    </svg>);

}

// ─────────── Step 5: Refinement rounds ───────────
function StepRounds({ t, onFinish, onBack }) {
  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>Refinement rounds</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          Run 4–5 fresh Chat GPT Pro conversations against the plan. Each round finds issues the
          previous round missed. Stop when suggestions become incremental.
        </div>
      </div>

      <RoundsTimeline t={t} />

      <div style={{ display: 'flex', justifyContent: 'space-between' }}>
        <button onClick={onBack} style={pillBtn2(t, 'ghost')}>Back</button>
        <button onClick={onFinish} style={pillBtn2(t, 'primary')}>
          <span>Save plan & exit wizard</span>{Icon.check(10, '#fff')}
        </button>
      </div>
    </div>);

}

function RoundsTimeline({ t }) {
  return (
    <div style={{
      background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
      boxShadow: t.shadow, overflow: 'hidden'
    }}>
      <div style={{
        padding: '10px 14px', borderBottom: `0.5px solid ${t.borderSoft}`,
        display: 'flex', alignItems: 'center', gap: 10
      }}>
        <span style={{ fontSize: 12.5, fontWeight: 600 }}>Convergence</span>
        <ConvergenceBar t={t} pct={78} />
        <span style={{ fontSize: 11, color: t.textDim, fontVariantNumeric: 'tabular-nums', fontFamily: MONO2 }}>78%</span>
        <div style={{ flex: 1 }} />
        <span style={{ fontSize: 11, color: t.textDim }}>Recommended: 1–2 more rounds</span>
      </div>
      <div>
        {REVIEW_ROUNDS.map((r, i) =>
        <RoundRow key={r.n} t={t} round={r} last={i === REVIEW_ROUNDS.length - 1} />
        )}
      </div>
    </div>);

}

function RoundRow({ t, round, last }) {
  const isDone = round.status === 'done';
  const isActive = round.status === 'active';
  return (
    <div style={{
      display: 'flex', gap: 14, padding: '12px 14px',
      borderBottom: last ? 'none' : `0.5px solid ${t.borderSoft}`,
      alignItems: 'flex-start',
      background: isActive ? t.crestSoft : 'transparent'
    }}>
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
        <div style={{
          width: 22, height: 22, borderRadius: '50%',
          background: isDone ? t.success : isActive ? t.crest : t.panelAlt,
          border: !isDone && !isActive ? `1px solid ${t.border}` : 'none',
          color: '#fff', fontSize: 11, fontWeight: 700,
          display: 'flex', alignItems: 'center', justifyContent: 'center'
        }}>
          {isDone ? Icon.check(11, '#fff') : isActive ? <Spinner color="#fff" /> : <span style={{ color: t.textMute }}>{round.n}</span>}
        </div>
      </div>
      <div style={{ flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 12.5, fontWeight: 600 }}>Round {round.n} · {round.title}</span>
          <span style={{
            fontSize: 10.5, padding: '1px 7px', borderRadius: 999,
            background: isDone ? 'rgba(48,182,107,0.15)' : isActive ? t.crest : t.panelAlt,
            color: isDone ? t.success : isActive ? '#fff' : t.textDim,
            fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase'
          }}>{round.status}</span>
          {round.status !== 'pending' &&
          <span style={{ fontSize: 11, color: t.textDim, fontFamily: MONO2 }}>
              · {round.deltas} {round.deltas === 1 ? 'change' : 'changes'}
            </span>
          }
        </div>
        <div style={{ fontSize: 12, color: t.textDim, marginTop: 4, lineHeight: 1.5 }}>{round.summary}</div>
      </div>
      {round.status === 'pending' &&
      <button style={pillBtnSm(t, 'ghost')}>Run round</button>
      }
      {round.status === 'active' &&
      <button style={pillBtnSm(t, 'primary')}>Open</button>
      }
    </div>);

}

function ConvergenceBar({ t, pct }) {
  return (
    <div style={{ flex: '0 0 140px', height: 6, borderRadius: 3, background: t.panelAlt, overflow: 'hidden', border: `0.5px solid ${t.borderSoft}` }}>
      <div style={{
        width: `${pct}%`, height: '100%',
        background: `linear-gradient(90deg, ${t.crest}, ${t.success})`,
        borderRadius: 3
      }} />
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Compare view (full-page version of step 3, accessed from plan overview)
// ─────────────────────────────────────────────────────────────
function CompareView({ t, setView }) {
  return (
    <div style={{ padding: '24px 28px' }}>
      <button onClick={() => setView('plan')} style={pillBtn2(t, 'ghost')}>← Back to plan</button>
      <div style={{ marginTop: 14 }}>
        <StepCompare t={t} onNext={() => setView('plan')} onBack={() => setView('plan')} />
      </div>
    </div>);

}
function SynthesizeView({ t, setView }) {
  return (
    <div style={{ padding: '24px 28px' }}>
      <button onClick={() => setView('plan')} style={pillBtn2(t, 'ghost')}>← Back to plan</button>
      <div style={{ marginTop: 14 }}>
        <StepSynthesize t={t} onNext={() => setView('plan')} onBack={() => setView('plan')} />
      </div>
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Refinement rounds tab
// ─────────────────────────────────────────────────────────────
function RoundsView({ t, setView }) {
  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between' }}>
        <div>
          <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>Refinement rounds</div>
          <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4, maxWidth: 600 }}>
            Plans created with the flywheel reach 3,000–6,000+ lines through 4–6 fresh Chat GPT Pro conversations.
            Each round catches issues the previous one missed.
          </div>
        </div>
        <button style={pillBtn2(t, 'primary')}>
          {Icon.spark(11, '#fff')}<span>Run next round</span>
        </button>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10 }}>
        <Stat2 t={t} label="Rounds completed" value="3" suffix="of 5 typical" />
        <Stat2 t={t} label="Plan length" value="5,547" suffix="lines · +1,242 since round 1" />
        <Stat2 t={t} label="Convergence" value="78%" suffix="content similarity" />
      </div>

      <RoundsTimeline t={t} />

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, boxShadow: t.shadow
      }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, marginBottom: 8 }}>Convergence signals</div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10 }}>
          <SignalCard t={t} label="Output shrinking" value="Round 3 was 32% shorter" trend="up" />
          <SignalCard t={t} label="Change velocity" value="47 → 28 → 19 deltas" trend="up" />
          <SignalCard t={t} label="Content similarity" value="0.78 vs round 2" trend="up" />
        </div>
      </div>
    </div>);

}

function Stat2({ t, label, value, suffix }) {
  return (
    <div style={{
      background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
      padding: '12px 14px', boxShadow: t.shadow
    }}>
      <div style={{ fontSize: 10.5, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>{label}</div>
      <div style={{ display: 'flex', alignItems: 'baseline', gap: 4, marginTop: 6 }}>
        <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, fontVariantNumeric: 'tabular-nums' }}>{value}</div>
      </div>
      <div style={{ fontSize: 11, color: t.textDim, marginTop: 2 }}>{suffix}</div>
    </div>);

}

function SignalCard({ t, label, value, trend }) {
  return (
    <div style={{
      padding: 10, borderRadius: 8,
      background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <div style={{
          width: 14, height: 14, borderRadius: '50%',
          background: t.success,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: '#fff', fontSize: 10, fontWeight: 700
        }}>↗</div>
        <div style={{ fontSize: 11, color: t.textDim }}>{label}</div>
      </div>
      <div style={{ fontSize: 12.5, fontWeight: 500, marginTop: 6 }}>{value}</div>
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Code Health tab
// ─────────────────────────────────────────────────────────────
function CodeHealthView({ t }) {
  const modules = [
  { id: 'core/plan-store', cov: 94, cyc: 8, files: 6, status: 'good' },
  { id: 'core/bead-graph', cov: 87, cyc: 14, files: 11, status: 'good' },
  { id: 'core/synthesis', cov: 71, cyc: 22, files: 8, status: 'warn' },
  { id: 'ipc/agent-mail', cov: 89, cyc: 11, files: 9, status: 'good' },
  { id: 'ui/wizard', cov: 42, cyc: 18, files: 14, status: 'risk' },
  { id: 'ui/sidebar', cov: 78, cyc: 9, files: 7, status: 'good' },
  { id: 'sidecar/bv-bridge', cov: 65, cyc: 16, files: 5, status: 'warn' },
  { id: 'sidecar/br-bridge', cov: 81, cyc: 12, files: 4, status: 'good' }];


  return (
    <div style={{ padding: '24px 28px', display: 'flex', flexDirection: 'column', gap: 14 }}>
      <div>
        <div style={{ fontSize: 18, fontWeight: 700, letterSpacing: -0.3 }}>Code health</div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 4 }}>
          Coverage and cyclomatic complexity per module. Updated after each agent run.
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 10 }}>
        <Stat2 t={t} label="Overall coverage" value="76%" suffix="78 of 102 paths covered" />
        <Stat2 t={t} label="Avg complexity" value="13.8" suffix="cyclomatic, per fn" />
        <Stat2 t={t} label="Risk modules" value="2" suffix="below 60% coverage" />
        <Stat2 t={t} label="Tests" value="1,847" suffix="passing · 0 failing" />
      </div>

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        boxShadow: t.shadow, overflow: 'hidden'
      }}>
        <div style={{
          display: 'grid', gridTemplateColumns: '1.6fr 1fr 110px 80px 60px',
          padding: '10px 14px', borderBottom: `0.5px solid ${t.borderSoft}`,
          fontSize: 10.5, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase'
        }}>
          <div>Module</div>
          <div>Coverage</div>
          <div>Complexity</div>
          <div>Files</div>
          <div></div>
        </div>
        {modules.map((m, i) =>
        <ModuleRow key={m.id} t={t} m={m} last={i === modules.length - 1} />
        )}
      </div>

      <div style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
        padding: 14, boxShadow: t.shadow
      }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, marginBottom: 10 }}>Coverage × complexity</div>
        <ScatterPlot t={t} modules={modules} />
        <div style={{ fontSize: 11, color: t.textDim, marginTop: 8, display: 'flex', gap: 14 }}>
          <span>● High-risk quadrant: low coverage + high complexity</span>
          <span style={{ color: t.textMute }}>· hover any dot for details</span>
        </div>
      </div>
    </div>);

}

function ModuleRow({ t, m, last }) {
  const cMap = { good: t.success, warn: t.warn, risk: t.danger };
  const dotC = cMap[m.status];
  return (
    <div style={{
      display: 'grid', gridTemplateColumns: '1.6fr 1fr 110px 80px 60px',
      padding: '10px 14px', borderBottom: last ? 'none' : `0.5px solid ${t.borderSoft}`,
      alignItems: 'center', fontSize: 12.5
    }}>
      <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
        <div style={{ width: 6, height: 6, borderRadius: '50%', background: dotC }} />
        <span style={{ fontFamily: MONO2, fontSize: 12 }}>{m.id}</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <div style={{ flex: 1, height: 5, borderRadius: 3, background: t.panelAlt, overflow: 'hidden' }}>
          <div style={{ width: `${m.cov}%`, height: '100%', background: dotC }} />
        </div>
        <span style={{ fontFamily: MONO2, fontSize: 11, color: t.textDim, minWidth: 30, textAlign: 'right' }}>{m.cov}%</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <span style={{ fontFamily: MONO2, fontSize: 12 }}>{m.cyc}</span>
        <ComplexityChip t={t} c={m.cyc} />
      </div>
      <div style={{ fontFamily: MONO2, fontSize: 12, color: t.textDim }}>{m.files}</div>
      <div>{Icon.chevR(9, t.textMute)}</div>
    </div>);

}

function ComplexityChip({ t, c }) {
  const level = c < 10 ? 'low' : c < 20 ? 'med' : 'high';
  const map = { low: { c: t.success, l: 'low' }, med: { c: t.warn, l: 'med' }, high: { c: t.danger, l: 'high' } };
  const m = map[level];
  return (
    <span style={{
      fontSize: 9.5, padding: '1px 6px', borderRadius: 999,
      background: m.c, color: '#fff', fontWeight: 700, letterSpacing: 0.3, textTransform: 'uppercase'
    }}>{m.l}</span>);

}

function ScatterPlot({ t, modules }) {
  const W = 720,H = 200;
  return (
    <div style={{ position: 'relative' }}>
      <svg width="100%" viewBox={`0 0 ${W} ${H}`} style={{ display: 'block' }}>
        {/* axes */}
        <line x1="40" y1={H - 24} x2={W - 8} y2={H - 24} stroke={t.borderSoft} strokeWidth="1" />
        <line x1="40" y1="8" x2="40" y2={H - 24} stroke={t.borderSoft} strokeWidth="1" />
        {/* danger zone */}
        <rect x="40" y="8" width={(W - 48) * 0.6 - 40 + 40} height={(H - 32) * 0.6} fill={t.danger} fillOpacity="0.04" />
        {/* gridlines */}
        {[25, 50, 75].map((p) =>
        <g key={p}>
            <line x1="40" y1={H - 24 - (H - 32) * (p / 100)} x2={W - 8} y2={H - 24 - (H - 32) * (p / 100)} stroke={t.borderSoft} strokeWidth="0.5" strokeDasharray="2 4" />
            <text x="34" y={H - 21 - (H - 32) * (p / 100)} fontSize="9" fill={t.textMute} textAnchor="end">{p}%</text>
          </g>
        )}
        <text x="34" y={H - 21} fontSize="9" fill={t.textMute} textAnchor="end">0</text>
        {/* x labels */}
        {[10, 20, 30].map((c) => {
          const x = 40 + (W - 48) * (c / 30);
          return (
            <g key={c}>
              <line x1={x} y1={H - 24} x2={x} y2={H - 20} stroke={t.borderSoft} strokeWidth="0.5" />
              <text x={x} y={H - 8} fontSize="9" fill={t.textMute} textAnchor="middle">{c}</text>
            </g>);

        })}
        <text x={W / 2} y={H - 0.5} fontSize="9" fill={t.textDim} textAnchor="middle">complexity →</text>
        {/* points */}
        {modules.map((m) => {
          const cx = 40 + (W - 48) * Math.min(m.cyc / 30, 1);
          const cy = H - 24 - (H - 32) * (m.cov / 100);
          const cMap = { good: t.success, warn: t.warn, risk: t.danger };
          return (
            <g key={m.id}>
              <circle cx={cx} cy={cy} r="6" fill={cMap[m.status]} fillOpacity="0.9" />
              <text x={cx + 9} y={cy + 3} fontSize="9.5" fill={t.text} fontFamily={MONO2}>{m.id.split('/')[1]}</text>
            </g>);

        })}
      </svg>
    </div>);

}

// ─────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────
function pillBtn2(t, kind = 'default') {
  const map = {
    primary: { bg: t.crest, fg: '#fff', border: 'transparent' },
    default: { bg: t.panel, fg: t.text, border: t.border },
    ghost: { bg: 'transparent', fg: t.text, border: t.border }
  }[kind];
  return {
    height: 30, padding: '0 14px', borderRadius: 7,
    background: map.bg, color: map.fg,
    border: `0.5px solid ${map.border}`,
    fontFamily: FONT2, fontSize: 12.5, fontWeight: 600,
    display: 'inline-flex', alignItems: 'center', gap: 6,
    cursor: 'pointer',
    boxShadow: kind === 'primary' ? '0 1px 0 rgba(255,255,255,0.2) inset, 0 1px 1px rgba(0,0,0,0.1)' : 'none'
  };
}

function pillBtnSm(t, kind = 'default') {
  return { ...pillBtn2(t, kind), height: 24, fontSize: 11.5, padding: '0 10px' };
}

Object.assign(window, {
  Wizard, CompareView, SynthesizeView, RoundsView, CodeHealthView
});