// firstrun.jsx — Hoopoe first-run setup wizard
// 11 screens, takes over the macOS window on first load
// Uses tokens from app.jsx (TOKENS, FONT, MONO, HoopoeCrest, Icon)

const FR_FONT = '-apple-system, BlinkMacSystemFont, "SF Pro Text", "SF Pro Display", "Helvetica Neue", system-ui, sans-serif';
const FR_MONO = '"SF Mono", ui-monospace, "JetBrains Mono", Menlo, monospace';

// ─────────────────────────────────────────────────────────────
// Public component — call <FirstRun t={t} theme={theme} onFinish={...} />
// ─────────────────────────────────────────────────────────────
function FirstRun({ t, theme, onFinish }) {
  const [step, setStep] = React.useState(0);
  const [state, setState] = React.useState({
    sshHost: '',
    sshUser: 'jeffrey',
    sshKey: 'id_ed25519',
    sshConnected: false,
    fingerprintTrusted: false,
    connectionName: 'hoopoe-prod',
    importedRepo: null,
    sample: false,
    telemetry: false,
    oracleEnabled: true,
  });
  const update = (patch) => setState((s) => ({ ...s, ...patch }));

  const STEPS = [
    { id: 'welcome', label: 'Welcome' },
    { id: 'mode', label: 'Mode' },
    { id: 'ssh', label: 'SSH' },
    { id: 'preflight', label: 'Preflight' },
    { id: 'acfs', label: 'ACFS Install' },
    { id: 'daemon', label: 'Daemon' },
    { id: 'tools', label: 'Tools' },
    { id: 'subs', label: 'Subscriptions' },
    { id: 'oracle', label: 'Oracle' },
    { id: 'project', label: 'First project' },
    { id: 'ready', label: 'Ready' },
  ];

  const next = () => setStep((s) => Math.min(s + 1, STEPS.length - 1));
  const back = () => setStep((s) => Math.max(s - 1, 0));

  return (
    <div style={{
      width: '100%', height: '100%',
      display: 'flex', flexDirection: 'column',
      background: t.bg, color: t.text,
      fontFamily: FR_FONT,
      position: 'relative', overflow: 'hidden',
    }}>
      {/* Title bar — traffic lights only, no app chrome */}
      <div style={{
        height: 38, flexShrink: 0,
        display: 'flex', alignItems: 'center',
        padding: '0 14px',
        background: t.sidebar,
        backdropFilter: 'blur(40px) saturate(180%)',
        WebkitBackdropFilter: 'blur(40px) saturate(180%)',
        borderBottom: `0.5px solid ${t.sidebarBorder}`,
      }}>
        <div style={{ display: 'flex', gap: 8 }}>
          <Light c="#ff5f57" />
          <Light c="#febc2e" />
          <Light c="#28c840" />
        </div>
        <div style={{ flex: 1, textAlign: 'center', fontSize: 12, color: t.textDim, marginLeft: -56 }}>
          Hoopoe Setup
        </div>
      </div>

      {/* Progress rail (hidden on welcome/mode for cleaner intro) */}
      {step >= 2 && step < STEPS.length - 1 && (
        <ProgressRail t={t} steps={STEPS} current={step} />
      )}

      {/* Screen body */}
      <div style={{ flex: 1, overflow: 'auto', position: 'relative' }}>
        {step === 0  && <Welcome     t={t} onNext={next} onSkipToConnect={() => setStep(1)} />}
        {step === 1  && <ModeChoice  t={t} onNext={next} onBack={back} />}
        {step === 2  && <SshSetup    t={t} state={state} update={update} onNext={next} onBack={back} />}
        {step === 3  && <Preflight   t={t} onNext={next} onBack={back} />}
        {step === 4  && <AcfsInstall t={t} onNext={next} onBack={back} />}
        {step === 5  && <DaemonInstall t={t} onNext={next} onBack={back} />}
        {step === 6  && <ToolInventory t={t} onNext={next} onBack={back} />}
        {step === 7  && <Subscriptions t={t} onNext={next} onBack={back} />}
        {step === 8  && <OracleSetup  t={t} state={state} update={update} onNext={next} onBack={back} />}
        {step === 9  && <FirstProject t={t} state={state} update={update} onNext={next} onBack={back} />}
        {step === 10 && <ReadyScreen  t={t} state={state} update={update} onFinish={onFinish} onBack={back} />}
      </div>
    </div>
  );
}

function Light({ c }) {
  return <div style={{ width: 12, height: 12, borderRadius: '50%', background: c, border: '0.5px solid rgba(0,0,0,0.12)' }} />;
}

// ─────────────────────────────────────────────────────────────
// Progress rail — top breadcrumb showing current phase
// ─────────────────────────────────────────────────────────────
function ProgressRail({ t, steps, current }) {
  // skip welcome+mode in the rail
  const railSteps = steps.slice(2);
  const railIdx = current - 2;
  return (
    <div style={{
      flexShrink: 0,
      padding: '14px 28px 12px',
      borderBottom: `0.5px solid ${t.borderSoft}`,
      background: t.panelAlt,
      display: 'flex', alignItems: 'center', gap: 6,
      overflowX: 'auto',
    }}>
      {railSteps.map((s, i) => {
        const done = i < railIdx;
        const active = i === railIdx;
        return (
          <React.Fragment key={s.id}>
            <div style={{
              display: 'flex', alignItems: 'center', gap: 7,
              padding: '4px 10px', borderRadius: 999,
              background: active ? t.crestSoft : 'transparent',
              border: active ? `0.5px solid ${t.crest}55` : `0.5px solid transparent`,
              flexShrink: 0,
            }}>
              <div style={{
                width: 16, height: 16, borderRadius: '50%',
                background: done ? t.success : active ? t.crest : t.panel,
                border: done || active ? 'transparent' : `0.5px solid ${t.border}`,
                color: '#fff', fontSize: 10, fontWeight: 700,
                display: 'flex', alignItems: 'center', justifyContent: 'center',
              }}>
                {done ? '✓' : <span style={{ color: active ? '#fff' : t.textMute }}>{i + 1}</span>}
              </div>
              <div style={{
                fontSize: 11, fontWeight: active ? 600 : 500,
                color: active ? t.text : done ? t.text : t.textDim,
                whiteSpace: 'nowrap',
              }}>{s.label}</div>
            </div>
            {i < railSteps.length - 1 && (
              <div style={{
                width: 14, height: 1, background: i < railIdx ? t.success : t.borderSoft,
                flexShrink: 0,
              }} />
            )}
          </React.Fragment>
        );
      })}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Shared footer bar
// ─────────────────────────────────────────────────────────────
function Footer({ t, onBack, onNext, nextLabel = 'Continue', nextDisabled, primary = true, right, hint }) {
  return (
    <div style={{
      borderTop: `0.5px solid ${t.borderSoft}`,
      padding: '12px 28px',
      background: t.panelAlt,
      display: 'flex', alignItems: 'center', gap: 10,
    }}>
      {onBack && (
        <button onClick={onBack} style={btn(t, 'ghost')}>← Back</button>
      )}
      {hint && <div style={{ fontSize: 11.5, color: t.textDim }}>{hint}</div>}
      <div style={{ flex: 1 }} />
      {right}
      {onNext && (
        <button onClick={onNext} disabled={nextDisabled} style={{
          ...btn(t, primary ? 'primary' : 'default'),
          opacity: nextDisabled ? 0.45 : 1,
          cursor: nextDisabled ? 'not-allowed' : 'pointer',
        }}>
          <span>{nextLabel}</span><span style={{ fontSize: 11 }}>→</span>
        </button>
      )}
    </div>
  );
}

function btn(t, kind = 'default') {
  const map = {
    primary: { bg: t.crest, fg: '#fff', border: 'transparent', shadow: '0 1px 0 rgba(255,255,255,0.2) inset, 0 1px 2px rgba(0,0,0,0.12)' },
    accent:  { bg: t.accent, fg: '#fff', border: 'transparent', shadow: '0 1px 0 rgba(255,255,255,0.2) inset, 0 1px 2px rgba(0,0,0,0.12)' },
    default: { bg: t.panel, fg: t.text, border: t.border, shadow: 'none' },
    ghost:   { bg: 'transparent', fg: t.textDim, border: 'transparent', shadow: 'none' },
  }[kind];
  return {
    height: 30, padding: '0 14px', borderRadius: 7,
    background: map.bg, color: map.fg,
    border: `0.5px solid ${map.border}`,
    fontFamily: FR_FONT, fontSize: 12.5, fontWeight: 600,
    display: 'inline-flex', alignItems: 'center', gap: 6,
    cursor: 'pointer',
    whiteSpace: 'nowrap',
    boxShadow: map.shadow,
  };
}

// ─────────────────────────────────────────────────────────────
// Reusable bits
// ─────────────────────────────────────────────────────────────
function Body({ children, maxWidth = 760 }) {
  return (
    <div style={{
      maxWidth, margin: '0 auto', padding: '32px 32px 28px',
      display: 'flex', flexDirection: 'column', gap: 18,
    }}>
      {children}
    </div>
  );
}

function H1({ t, children, sub }) {
  return (
    <div>
      <div style={{ fontSize: 22, fontWeight: 700, letterSpacing: -0.4, color: t.text }}>{children}</div>
      {sub && <div style={{ fontSize: 13, color: t.textDim, marginTop: 6, lineHeight: 1.55 }}>{sub}</div>}
    </div>
  );
}

function Eyebrow({ t, children }) {
  return (
    <div style={{ fontSize: 10.5, color: t.textDim, fontWeight: 700, letterSpacing: 0.5, textTransform: 'uppercase' }}>
      {children}
    </div>
  );
}

function Card({ t, children, padding = 18, style = {} }) {
  return (
    <div style={{
      background: t.panel, border: `0.5px solid ${t.border}`,
      borderRadius: 10, padding, boxShadow: t.shadow,
      ...style,
    }}>{children}</div>
  );
}

function StatusIcon({ kind, t, size = 18 }) {
  const c =
    kind === 'ok' ? t.success :
    kind === 'warn' ? t.warn :
    kind === 'fail' ? t.danger :
    kind === 'spin' ? t.crest :
    t.textMute;
  if (kind === 'spin') {
    return (
      <svg width={size} height={size} viewBox="0 0 18 18" style={{ animation: 'spin 0.9s linear infinite' }}>
        <circle cx="9" cy="9" r="6.5" fill="none" stroke={`${c}33`} strokeWidth="2" />
        <path d="M9 2.5 a6.5 6.5 0 0 1 6.5 6.5" fill="none" stroke={c} strokeWidth="2" strokeLinecap="round" />
      </svg>
    );
  }
  if (kind === 'pending') {
    return <div style={{ width: size, height: size, borderRadius: '50%', border: `1.5px solid ${t.border}`, background: 'transparent' }} />;
  }
  const glyph = kind === 'ok' ? '✓' : kind === 'warn' ? '!' : kind === 'fail' ? '✕' : '·';
  return (
    <div style={{
      width: size, height: size, borderRadius: '50%',
      background: c, color: '#fff',
      fontSize: size * 0.62, fontWeight: 700,
      display: 'flex', alignItems: 'center', justifyContent: 'center',
      flexShrink: 0,
    }}>{glyph}</div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 0 — Welcome
// ─────────────────────────────────────────────────────────────
function Welcome({ t, onNext, onSkipToConnect }) {
  return (
    <div style={{
      height: '100%', display: 'flex', flexDirection: 'column',
      alignItems: 'center', justifyContent: 'center',
      padding: '40px 32px',
    }}>
      <div style={{ maxWidth: 600, textAlign: 'center', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 18 }}>
        <HoopoeCrest size={64} color={t.text} accent={t.crest} />
        <div>
          <div style={{ fontSize: 32, fontWeight: 700, letterSpacing: -0.6, color: t.text }}>Hoopoe</div>
          <div style={{ fontSize: 14, color: t.textDim, marginTop: 4, letterSpacing: 0.2 }}>Agentic Coding Cockpit</div>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12, fontSize: 13.5, color: t.text, lineHeight: 1.6, textAlign: 'left', marginTop: 8 }}>
          <p style={{ margin: 0 }}>
            Hoopoe is a desktop cockpit for orchestrating swarms of coding agents.
            You write a plan, Hoopoe carves it into beads, and a fleet of Claude, Codex, and Gemini
            agents tend the work in parallel — with you in the loop on every commit.
          </p>
          <p style={{ margin: 0 }}>
            <strong style={{ color: t.text }}>What you need:</strong> a Linux VPS you can SSH into,
            and at least one Pro / Max / Ultra subscription with a coding-agent CLI
            (Claude Code, Codex, or Gemini CLI).
          </p>
          <p style={{ margin: 0, color: t.textDim }}>
            <strong style={{ color: t.text }}>What you don't need:</strong> API keys.
            Hoopoe never asks for one. Ever.
          </p>
        </div>

        <button onClick={onNext} style={{
          ...btn(t, 'primary'), height: 38, padding: '0 22px', fontSize: 13.5, marginTop: 8,
        }}>
          <span>Get started</span><span>→</span>
        </button>

        <button onClick={onSkipToConnect} style={{
          background: 'transparent', border: 'none', color: t.textDim,
          fontSize: 12, cursor: 'pointer', marginTop: 4, textDecoration: 'underline',
          textDecorationColor: `${t.textDim}66`, textUnderlineOffset: 3,
        }}>
          I already set up Hoopoe on a VPS — connect to it
        </button>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 1 — Connection mode
// ─────────────────────────────────────────────────────────────
function ModeChoice({ t, onNext, onBack }) {
  const [mode, setMode] = React.useState('existing');

  const cards = [
    {
      id: 'existing',
      icon: '🖥', label: 'Connect existing VPS',
      tag: 'Recommended',
      desc: 'You already have a Linux server with SSH access. Hoopoe will install the daemon, agent CLIs, and supporting tools onto it.',
      sub: 'Ubuntu 22.04+ / Debian 12+ / Fedora 39+ · 4+ GB RAM · 30 GB free',
      enabled: true,
    },
    {
      id: 'provision',
      icon: '☁', label: 'Provision a new VPS',
      tag: 'Coming in v1.1',
      desc: 'Spin up a fresh server through Hetzner, DigitalOcean, or AWS, pre-configured with everything Hoopoe needs.',
      sub: 'One-click provisioning · auto-sized for swarm workloads',
      enabled: false,
    },
    {
      id: 'local',
      icon: '◐', label: 'Local demo',
      desc: 'A fixture-backed Hoopoe running entirely on this Mac. No real agents, no real swarm — just clicking through the UI on canned data.',
      sub: 'Great for kicking the tires before you commit to a VPS',
      enabled: true,
    },
  ];

  return (
    <>
      <Body>
        <Eyebrow t={t}>Step 1 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoe runs on a Linux VPS so the swarm keeps tending while your Mac sleeps. Where's that server going to live?">
          Where will Hoopoe run?
        </H1>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {cards.map((c) => (
            <ModeCard key={c.id} t={t} card={c} selected={mode === c.id} onClick={() => c.enabled && setMode(c.id)} />
          ))}
        </div>
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} />
    </>
  );
}

function ModeCard({ t, card, selected, onClick }) {
  const [hover, setHover] = React.useState(false);
  const dis = !card.enabled;
  return (
    <div
      onClick={dis ? undefined : onClick}
      onMouseEnter={() => !dis && setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        background: t.panel,
        border: `1px solid ${selected ? t.crest : (hover ? t.border : t.borderSoft)}`,
        borderRadius: 10, padding: 16,
        cursor: dis ? 'not-allowed' : 'pointer',
        opacity: dis ? 0.55 : 1,
        boxShadow: selected ? `0 0 0 3px ${t.crest}22` : 'none',
        display: 'flex', alignItems: 'flex-start', gap: 14,
        transition: 'all 120ms',
      }}
    >
      <div style={{
        width: 36, height: 36, borderRadius: 9,
        background: selected ? t.crest : t.panelAlt,
        color: selected ? '#fff' : t.crest,
        fontSize: 18, fontWeight: 600,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        flexShrink: 0,
      }}>{card.icon}</div>

      <div style={{ flex: 1 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
          <div style={{ fontSize: 14, fontWeight: 600, color: t.text }}>{card.label}</div>
          {card.tag && (
            <span style={{
              fontSize: 10, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase',
              color: card.enabled ? t.crest : t.textMute,
              background: card.enabled ? t.crestSoft : t.panelAlt,
              padding: '2px 7px', borderRadius: 4,
            }}>{card.tag}</span>
          )}
        </div>
        <div style={{ fontSize: 12.5, color: t.textDim, marginTop: 5, lineHeight: 1.5 }}>{card.desc}</div>
        <div style={{ fontSize: 11.5, color: t.textMute, marginTop: 6, fontFamily: FR_MONO }}>{card.sub}</div>
      </div>

      <div style={{
        width: 18, height: 18, borderRadius: '50%',
        border: `1.5px solid ${selected ? t.crest : t.border}`,
        background: selected ? t.crest : 'transparent',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        flexShrink: 0, marginTop: 2,
      }}>
        {selected && <div style={{ width: 7, height: 7, borderRadius: '50%', background: '#fff' }} />}
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 2 — SSH setup
// ─────────────────────────────────────────────────────────────
function SshSetup({ t, state, update, onNext, onBack }) {
  const [keyMode, setKeyMode] = React.useState('existing'); // 'existing' | 'new'
  const [host, setHost] = React.useState('jeffrey@hoopoe.farm:22');
  const [name, setName] = React.useState('hoopoe-prod');
  const [keyChoice, setKeyChoice] = React.useState('id_ed25519');
  const [testStage, setTestStage] = React.useState('idle'); // 'idle' | 'connecting' | 'fingerprint' | 'done' | 'fail'

  const detectedKeys = ['id_ed25519', 'id_ed25519_personal', 'id_rsa'];

  const runTest = () => {
    setTestStage('connecting');
    setTimeout(() => setTestStage('fingerprint'), 900);
  };
  const trustFingerprint = () => {
    setTestStage('done');
    update({ sshConnected: true, fingerprintTrusted: true, sshHost: host, connectionName: name });
  };

  const canContinue = testStage === 'done';

  return (
    <>
      <Body>
        <Eyebrow t={t}>Step 2 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoe will use this connection to install the daemon, run agent CLIs, and stream events back to your Mac.">
          Connect to your VPS over SSH
        </H1>

        <Card t={t} padding={20}>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Field t={t} label="Connection name" hint="How this server appears in Hoopoe">
              <input
                value={name} onChange={(e) => setName(e.target.value)}
                style={input(t)}
              />
            </Field>
            <Field t={t} label="user@host[:port]">
              <input
                value={host} onChange={(e) => setHost(e.target.value)}
                placeholder="ubuntu@10.0.1.42:22"
                style={{ ...input(t), fontFamily: FR_MONO, fontSize: 12.5 }}
              />
            </Field>
          </div>

          <div style={{ marginTop: 18 }}>
            <Field t={t} label="SSH key">
              <div style={{
                display: 'flex', flexDirection: 'column', gap: 8,
                background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
                borderRadius: 8, padding: 10,
              }}>
                <RadioRow t={t} selected={keyMode === 'existing'} onClick={() => setKeyMode('existing')} label="Use existing key from ~/.ssh/">
                  <select
                    value={keyChoice}
                    onChange={(e) => setKeyChoice(e.target.value)}
                    disabled={keyMode !== 'existing'}
                    style={{
                      ...input(t), fontFamily: FR_MONO, fontSize: 12,
                      width: 220, marginTop: 6, opacity: keyMode === 'existing' ? 1 : 0.5,
                    }}
                  >
                    {detectedKeys.map((k) => <option key={k}>~/.ssh/{k}</option>)}
                  </select>
                </RadioRow>
                <RadioRow t={t} selected={keyMode === 'new'} onClick={() => setKeyMode('new')} label="Generate a new ed25519 key for Hoopoe">
                  <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 4, fontFamily: FR_MONO }}>
                    ~/.ssh/hoopoe_ed25519 · key comment "hoopoe@{`{`}your-mac{`}`}"
                  </div>
                </RadioRow>
              </div>
            </Field>
          </div>

          <div style={{ marginTop: 16, display: 'flex', alignItems: 'center', gap: 10 }}>
            <button onClick={runTest} disabled={testStage === 'connecting'} style={{
              ...btn(t, testStage === 'idle' ? 'primary' : 'default'),
              opacity: testStage === 'connecting' ? 0.55 : 1,
            }}>
              {testStage === 'connecting' && <StatusIcon kind="spin" t={t} size={12} />}
              <span>{testStage === 'idle' ? 'Test connection' : testStage === 'connecting' ? 'Connecting…' : testStage === 'fingerprint' ? 'Test connection' : 'Re-test'}</span>
            </button>
            {testStage === 'idle' && <span style={{ fontSize: 11.5, color: t.textDim }}>Click to verify the SSH connection before continuing.</span>}
            {testStage === 'fingerprint' && <span style={{ fontSize: 11.5, color: t.warn, fontWeight: 600 }}>↓ Trust the host fingerprint to continue</span>}
            {testStage === 'done' && <span style={{ fontSize: 12, color: t.success, fontWeight: 600 }}>All checks passed</span>}
          </div>
        </Card>

        {testStage !== 'idle' && (
          <Card t={t} padding={0}>
            <CheckRow t={t} kind={testStage === 'connecting' ? 'spin' : 'ok'} title="TCP reachable"
              detail="hoopoe.farm:22 · 18ms" />
            <CheckRow t={t}
              kind={testStage === 'connecting' ? 'pending' : (testStage === 'fingerprint' ? 'warn' : 'ok')}
              title="Host fingerprint"
              detail={
                testStage === 'fingerprint'
                  ? 'SHA256:8mP3Fz9q… not in known_hosts'
                  : testStage === 'done'
                    ? 'SHA256:8mP3Fz9q… trusted ✓'
                    : 'Verifying…'
              }
              right={testStage === 'fingerprint' ? (
                <div style={{ display: 'flex', gap: 6 }}>
                  <button onClick={trustFingerprint} style={btn(t, 'primary')}>Trust</button>
                  <button onClick={() => setTestStage('idle')} style={btn(t, 'ghost')}>Reject</button>
                </div>
              ) : null}
            />
            <CheckRow t={t}
              kind={testStage === 'done' ? 'ok' : 'pending'}
              title="Sudo without password"
              detail={testStage === 'done' ? 'jeffrey ALL=(ALL) NOPASSWD: ALL ✓' : 'Pending fingerprint trust'}
            />
          </Card>
        )}
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} nextDisabled={!canContinue}
        hint={canContinue ? null : (testStage === 'idle' ? 'Click "Test connection" to verify the SSH setup' : testStage === 'fingerprint' ? 'Trust the host fingerprint to continue' : 'Verifying…')} />
    </>
  );
}

function input(t) {
  return {
    width: '100%', height: 30, padding: '0 10px',
    background: t.panelAlt, border: `0.5px solid ${t.border}`,
    borderRadius: 7, color: t.text, fontFamily: FR_FONT, fontSize: 12.5,
    outline: 'none', boxSizing: 'border-box',
  };
}

function Field({ t, label, hint, children }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
      <div style={{ fontSize: 11.5, fontWeight: 600, color: t.text }}>
        {label}
        {hint && <span style={{ color: t.textDim, fontWeight: 400, marginLeft: 6 }}>· {hint}</span>}
      </div>
      {children}
    </label>
  );
}

function RadioRow({ t, selected, onClick, label, children }) {
  return (
    <div onClick={onClick} style={{
      display: 'flex', alignItems: 'flex-start', gap: 10,
      padding: '6px 4px', cursor: 'pointer',
    }}>
      <div style={{
        width: 16, height: 16, borderRadius: '50%',
        border: `1.5px solid ${selected ? t.crest : t.border}`,
        background: selected ? t.crest : 'transparent',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        marginTop: 2, flexShrink: 0,
      }}>
        {selected && <div style={{ width: 6, height: 6, borderRadius: '50%', background: '#fff' }} />}
      </div>
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 12.5, fontWeight: 500, color: t.text }}>{label}</div>
        {children}
      </div>
    </div>
  );
}

function CheckRow({ t, kind, title, detail, right }) {
  return (
    <div style={{
      display: 'flex', alignItems: 'center', gap: 12,
      padding: '12px 16px',
      borderBottom: `0.5px solid ${t.borderSoft}`,
    }}>
      <StatusIcon kind={kind} t={t} />
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 12.5, fontWeight: 600, color: t.text }}>{title}</div>
        {detail && <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 2, fontFamily: FR_MONO }}>{detail}</div>}
      </div>
      {right}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 3 — Preflight
// ─────────────────────────────────────────────────────────────
function Preflight({ t, onNext, onBack }) {
  const [running, setRunning] = React.useState(true);
  const [results, setResults] = React.useState([]);
  const items = [
    { title: 'Operating system',           detail: 'Ubuntu 24.04.1 LTS · kernel 6.8.0-45',                kind: 'ok' },
    { title: 'CPU cores',                  detail: '8 vCPU · AMD EPYC 7763',                              kind: 'ok' },
    { title: 'Memory',                     detail: '16.0 GB RAM available',                               kind: 'ok' },
    { title: 'Free disk space',            detail: '94.2 GB free on / · room for ACFS + repos',           kind: 'ok' },
    { title: 'Network reachability',       detail: 'github.com ✓ · claude.ai ✓ · openai.com ✓ · googleapis.com ✓', kind: 'ok' },
    { title: 'Base tools',                 detail: 'bash 5.2 · curl 8.5 · git 2.43 · systemd 255',        kind: 'ok' },
    { title: 'Passwordless sudo',          detail: 'jeffrey ALL=(ALL) NOPASSWD: ALL',                     kind: 'ok' },
  ];

  React.useEffect(() => {
    let i = 0;
    const id = setInterval(() => {
      i += 1;
      setResults(items.slice(0, i));
      if (i >= items.length) {
        clearInterval(id);
        setRunning(false);
      }
    }, 320);
    return () => clearInterval(id);
  }, []);

  const allOk = !running && results.every((r) => r.kind === 'ok');

  return (
    <>
      <Body>
        <Eyebrow t={t}>Step 3 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoe checks that the VPS has the OS, hardware, and network it needs before installing anything.">
          Preflight checks
        </H1>

        <Card t={t} padding={0}>
          {items.map((item, i) => {
            const got = results[i];
            return (
              <CheckRow
                key={i} t={t}
                kind={got ? got.kind : (i === results.length && running ? 'spin' : 'pending')}
                title={item.title}
                detail={got ? item.detail : (i === results.length && running ? 'Checking…' : '—')}
              />
            );
          })}
        </Card>

        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <button onClick={() => { setRunning(true); setResults([]); }} disabled={running} style={{
            ...btn(t, 'default'), opacity: running ? 0.55 : 1,
          }}>↻ Re-run preflight</button>
          {allOk && <div style={{ fontSize: 12, color: t.success, fontWeight: 600 }}>All 7 checks passed — VPS is ready</div>}
        </div>
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} nextDisabled={running} />
    </>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 4 — ACFS install (the long one)
// ─────────────────────────────────────────────────────────────
function AcfsInstall({ t, onNext, onBack }) {
  const phases = [
    { id: 'prereqs',    label: 'System prereqs',     dur: 8,  cmds: ['apt-get update', 'apt-get install -y build-essential pkg-config'] },
    { id: 'packages',   label: 'Base packages',      dur: 20, cmds: ['apt-get install -y python3.12 nodejs ripgrep fd-find jq'] },
    { id: 'runtimes',   label: 'Language runtimes',  dur: 28, cmds: ['curl --proto =https --tlsv1.2 -sSf sh.rustup.rs | sh -s -- -y', 'pyenv install 3.12.6'] },
    { id: 'agentclis',  label: 'Agent CLIs',         dur: 26, cmds: ['npm i -g @anthropic-ai/claude-code', 'npm i -g @openai/codex-cli', 'npm i -g @google-gemini/cli'] },
    { id: 'ntm',        label: 'NTM',                dur: 12, cmds: ['cargo install --git https://github.com/steipete/ntm --locked'] },
    { id: 'brbv',       label: 'br + bv',            dur: 10, cmds: ['cargo install --git github.com/steipete/br', 'cargo install --git github.com/steipete/bv'] },
    { id: 'mail',       label: 'Agent Mail',         dur: 14, cmds: ['cargo install --git github.com/steipete/agent-mail', 'systemctl --user enable agent-mail'] },
    { id: 'safety',     label: 'Safety tools',       dur: 18, cmds: ['cargo install dcg caam caut casr pt srp sbh --locked'] },
    { id: 'skills',     label: 'Skills (jsm + jfp)', dur: 10, cmds: ['hoopoe skills install vibing-with-ntm jsm jfp'] },
    { id: 'done',       label: 'Finalize',           dur: 4,  cmds: ['hoopoe acfs verify'] },
  ];

  const [phaseIdx, setPhaseIdx] = React.useState(0);
  const [phaseProgress, setPhaseProgress] = React.useState(0);
  const [elapsed, setElapsed] = React.useState(0);
  const [showLogs, setShowLogs] = React.useState(true);
  const [logs, setLogs] = React.useState([]);

  const totalDur = phases.reduce((s, p) => s + p.dur, 0);
  const SPEED = 0.18; // seconds per simulated second — keeps demo brisk

  React.useEffect(() => {
    if (phaseIdx >= phases.length) return;
    const phase = phases[phaseIdx];
    let p = 0;
    const id = setInterval(() => {
      p += 4;
      setPhaseProgress(Math.min(p, 100));
      setElapsed((e) => e + 0.1 * 4);
      if (p >= 100) {
        clearInterval(id);
        setLogs((L) => [...L, `[${phase.id}] complete (${phase.dur}s)`]);
        setTimeout(() => {
          setPhaseIdx((i) => i + 1);
          setPhaseProgress(0);
        }, 250);
      }
    }, phase.dur * SPEED * 40);
    // log a couple of cmd lines
    phase.cmds.forEach((c, i) => {
      setTimeout(() => setLogs((L) => [...L, `$ ${c}`]), 60 + i * phase.dur * SPEED * 200);
    });
    return () => clearInterval(id);
  }, [phaseIdx]);

  const isDone = phaseIdx >= phases.length;
  const clampedElapsed = isDone ? totalDur : Math.min(elapsed, totalDur);
  const elapsedDisplay = `${Math.floor(clampedElapsed / 60)}m ${Math.floor(clampedElapsed % 60)}s`;
  const remaining = Math.max(0, Math.round(totalDur - clampedElapsed));
  const remDisplay = `${Math.floor(remaining / 60)}m ${remaining % 60}s`;

  return (
    <>
      <div style={{
        display: 'grid', gridTemplateColumns: '240px 1fr',
        gap: 0, padding: '24px 28px 0',
        flex: 1, minHeight: 0, overflow: 'auto',
      }}>
        {/* Left rail */}
        <div style={{
          background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 10,
          boxShadow: t.shadow, padding: 10, marginRight: 14, overflow: 'auto',
        }}>
          <div style={{ fontSize: 10.5, color: t.textMute, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase', padding: '4px 8px 8px' }}>
            ACFS Install
          </div>
          {phases.map((p, i) => {
            const status = i < phaseIdx ? 'ok' : i === phaseIdx ? 'spin' : 'pending';
            return (
              <div key={p.id} style={{
                display: 'flex', alignItems: 'center', gap: 9,
                padding: '7px 8px', borderRadius: 6,
                background: i === phaseIdx ? t.crestSoft : 'transparent',
              }}>
                <StatusIcon kind={status} t={t} size={14} />
                <div style={{ fontSize: 12, fontWeight: i === phaseIdx ? 600 : 500, color: status === 'pending' ? t.textDim : t.text }}>
                  {p.label}
                </div>
              </div>
            );
          })}
        </div>

        {/* Right pane */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14, minWidth: 0 }}>
          <Eyebrow t={t}>Step 4 of 10 · ACFS install</Eyebrow>
          <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between' }}>
            <div style={{ fontSize: 18, fontWeight: 700, color: t.text }}>
              {isDone ? 'Install complete' : `Installing ${phases[phaseIdx].label}…`}
            </div>
            <div style={{ fontSize: 11.5, color: t.textDim, fontFamily: FR_MONO }}>
              elapsed {elapsedDisplay} · {isDone ? 'done' : `~${remDisplay} remaining`}
            </div>
          </div>

          {/* Active phase card */}
          {!isDone && (
            <Card t={t} padding={16}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                <StatusIcon kind="spin" t={t} size={22} />
                <div style={{ flex: 1 }}>
                  <div style={{ fontSize: 13, fontWeight: 600 }}>{phases[phaseIdx].label}</div>
                  <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 2, fontFamily: FR_MONO }}>
                    {phases[phaseIdx].cmds[0]}
                  </div>
                </div>
              </div>
              <div style={{
                marginTop: 12, height: 6, borderRadius: 3,
                background: t.panelAlt, overflow: 'hidden',
              }}>
                <div style={{
                  width: `${phaseProgress}%`, height: '100%',
                  background: `linear-gradient(90deg, ${t.crest}, ${t.crestDeep || t.crest})`,
                  transition: 'width 120ms ease',
                }} />
              </div>
            </Card>
          )}

          {/* Overall progress */}
          <div>
            <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: t.textDim, marginBottom: 5 }}>
              <span>Overall progress</span>
              <span style={{ fontFamily: FR_MONO }}>{isDone ? 100 : Math.min(100, Math.round((clampedElapsed / totalDur) * 100))}%</span>
            </div>
            <div style={{ height: 4, borderRadius: 2, background: t.panelAlt, overflow: 'hidden' }}>
              <div style={{
                width: `${isDone ? 100 : Math.min(100, (clampedElapsed / totalDur) * 100)}%`, height: '100%',
                background: t.success, transition: 'width 150ms',
              }} />
            </div>
          </div>

          {/* Logs */}
          <Card t={t} padding={0} style={{ flex: 1, minHeight: 120, display: 'flex', flexDirection: 'column' }}>
            <div style={{
              padding: '9px 14px', borderBottom: `0.5px solid ${t.borderSoft}`,
              display: 'flex', alignItems: 'center', gap: 8,
            }}>
              <div style={{ fontSize: 12, fontWeight: 600 }}>Install log</div>
              <span style={{ fontSize: 10.5, color: t.textMute, fontFamily: FR_MONO }}>· {logs.length} lines</span>
              <div style={{ flex: 1 }} />
              <button onClick={() => setShowLogs(!showLogs)} style={{
                ...btn(t, 'ghost'), height: 22, padding: '0 8px', fontSize: 11,
              }}>{showLogs ? 'Hide' : 'Show'}</button>
            </div>
            {showLogs && (
              <div style={{
                flex: 1, padding: '10px 14px',
                fontFamily: FR_MONO, fontSize: 11.5, color: t.textDim,
                lineHeight: 1.55, overflow: 'auto', minHeight: 80, maxHeight: 180,
                background: t.panelAlt,
              }}>
                {logs.map((l, i) => (
                  <div key={i} style={{ color: l.startsWith('$') ? t.text : t.textDim }}>{l}</div>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>

      <Footer t={t} onBack={onBack} onNext={onNext} nextDisabled={!isDone}
        hint={isDone ? 'All 10 phases complete' : 'You can keep this window open in the background'} />
    </>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 5 — Daemon install
// ─────────────────────────────────────────────────────────────
function DaemonInstall({ t, onNext, onBack }) {
  const steps = [
    { id: 'verify',  label: 'Verify daemon binary',     detail: 'sha256:7d8a91… · signed-by Anthropic · provenance attested' },
    { id: 'unit',    label: 'Create systemd unit',      detail: 'Type=notify · NoNewPrivileges=yes · ProtectSystem=strict' },
    { id: 'start',   label: 'Start daemon service',     detail: 'systemctl --user start hoopoed.service' },
    { id: 'token',   label: 'Capture pairing token',    detail: 'F4M-9KP-3RT' },
    { id: 'bearer',  label: 'Exchange for 30-day bearer', detail: 'stored in macOS Keychain via safeStorage' },
    { id: 'tunnel',  label: 'Open SSH tunnel',           detail: 'localhost:54231 → 127.0.0.1:8800' },
    { id: 'ws',      label: 'WebSocket connected',      detail: 'seq cursor 0 · daemon v0.4.2 · protocol v1 ✓' },
  ];
  const [idx, setIdx] = React.useState(0);
  const [expanded, setExpanded] = React.useState({});

  React.useEffect(() => {
    if (idx >= steps.length) return;
    const id = setTimeout(() => setIdx(idx + 1), 700);
    return () => clearTimeout(id);
  }, [idx]);

  const done = idx >= steps.length;

  return (
    <>
      <Body maxWidth={680}>
        <Eyebrow t={t}>Step 5 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoed runs as a hardened systemd service on the VPS, talking to your Mac over an SSH-tunneled WebSocket.">
          Install the Hoopoe daemon
        </H1>

        <Card t={t} padding={0}>
          {steps.map((s, i) => {
            const kind = i < idx ? 'ok' : i === idx ? 'spin' : 'pending';
            const isExp = expanded[s.id];
            return (
              <div key={s.id} style={{ borderBottom: `0.5px solid ${t.borderSoft}` }}>
                <div
                  onClick={() => setExpanded((e) => ({ ...e, [s.id]: !e[s.id] }))}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    padding: '13px 16px', cursor: 'pointer',
                  }}
                >
                  <StatusIcon kind={kind} t={t} />
                  <div style={{ flex: 1 }}>
                    <div style={{ fontSize: 12.5, fontWeight: 600, color: kind === 'pending' ? t.textDim : t.text }}>{s.label}</div>
                    {(kind !== 'pending') && (
                      <div style={{ fontSize: 11.5, color: t.textDim, marginTop: 2, fontFamily: FR_MONO }}>{s.detail}</div>
                    )}
                  </div>
                  {kind === 'ok' && (
                    <div style={{ fontSize: 11, color: t.textDim }}>{isExp ? '▾' : '▸'}</div>
                  )}
                </div>
                {kind === 'ok' && isExp && (
                  <div style={{
                    padding: '10px 16px 14px 50px', background: t.panelAlt,
                    fontFamily: FR_MONO, fontSize: 11, color: t.textDim, lineHeight: 1.6,
                  }}>
                    {s.id === 'verify' && (
                      <>
                        <div>sha256: 7d8a91c4f3b2e6a5d8f1c9e0b4a7d2f5e8c1b6a9f4d7e0c3b6a1f4e7d0c5b8a3</div>
                        <div>signed-by: Anthropic Hoopoe Release Key (rsa4096/0xA1F3D...)</div>
                        <div>provenance: GitHub Actions · build cb87f2 · attested ✓</div>
                      </>
                    )}
                    {s.id === 'unit' && (
                      <>
                        <div>[Service]</div>
                        <div>Type=notify</div>
                        <div>ExecStart=/usr/local/bin/hoopoed serve</div>
                        <div>NoNewPrivileges=yes</div>
                        <div>ProtectSystem=strict</div>
                        <div>ReadWritePaths=/var/lib/hoopoe</div>
                      </>
                    )}
                    {s.id === 'token' && <div>Pairing token expires in 60 seconds (one-time use)</div>}
                    {s.id === 'tunnel' && <div>autossh -M 0 -N -L 54231:127.0.0.1:8800 jeffrey@hoopoe.farm</div>}
                  </div>
                )}
              </div>
            );
          })}
        </Card>

        {done && (
          <div style={{
            padding: 12, borderRadius: 8,
            background: `${t.success}1a`, color: t.success,
            display: 'flex', alignItems: 'center', gap: 10, fontSize: 12.5, fontWeight: 600,
          }}>
            <StatusIcon kind="ok" t={t} size={16} />
            <span>Daemon paired and streaming. Tunnel will auto-reconnect on Mac wake.</span>
          </div>
        )}
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} nextDisabled={!done} />
    </>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 6 — Tool inventory
// ─────────────────────────────────────────────────────────────
function ToolInventory({ t, onNext, onBack }) {
  const groups = [
    {
      name: 'Core flywheel',
      tools: [
        { id: 'acfs', name: 'ACFS', ver: '0.4.2',  caps: ['fs sandbox ✓', 'snapshot/restore ✓'] },
        { id: 'ntm',  name: 'NTM',  ver: '1.8.3',  caps: ['robot surfaces ✓', 'ws stream ✓'] },
        { id: 'br',   name: 'br',   ver: '0.6.1',  caps: ['branch graph ✓', 'rebase planner ✓'] },
        { id: 'bv',   name: 'bv',   ver: '0.9.4',  caps: ['robot mode ✓', 'review queue ✓'] },
        { id: 'mail', name: 'Agent Mail', ver: '0.3.7', caps: ['MCP bridge ✓', 'TTL reservations ✓'] },
        { id: 'ru',   name: 'ru',   ver: '0.5.0',  caps: ['rerun harness ✓', 'flake detector ✓'] },
      ],
    },
    {
      name: 'Safety & accounts',
      tools: [
        { id: 'dcg',  name: 'DCG',  ver: '0.2.4',  caps: ['dependency cap graph ✓'] },
        { id: 'caam', name: 'CAAM', ver: '0.7.2',  caps: ['credential attest ✓', '4 providers detected'] },
        { id: 'caut', name: 'caut', ver: '0.1.8',  caps: ['command audit log ✓'] },
        { id: 'casr', name: 'casr', ver: '0.3.0',  caps: ['snapshot restore ✓'] },
        { id: 'pt',   name: 'pt',   ver: '0.4.6',  caps: ['policy tester ✓'] },
        { id: 'srp',  name: 'srp',  ver: '0.2.1',  caps: ['secrets redactor ✓'] },
        { id: 'sbh',  name: 'sbh',  ver: '0.1.5',  caps: ['sandbox harness ✓'] },
      ],
    },
    {
      name: 'Skills',
      tools: [
        { id: 'jsm',  name: 'jsm',  ver: '2.1.0',  recommended: true, caps: ['plan→bead ✓', 'bead refinement ✓'] },
        { id: 'jfp',  name: 'jfp',  ver: '0.9.2',  caps: ['free fallback ✓', 'reduced parallelism'] },
      ],
    },
    {
      name: 'Review',
      tools: [
        { id: 'ubs',  name: 'UBS',  ver: '0.6.3',  caps: ['unified bead surface ✓', 'commit timeline ✓'] },
      ],
    },
  ];

  return (
    <>
      <Body maxWidth={1000}>
        <Eyebrow t={t}>Step 6 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoe just installed 16 tools and detected their capabilities. Re-detect any tile if a result looks wrong.">
          Tool inventory
        </H1>

        {groups.map((g) => (
          <div key={g.name} style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <Eyebrow t={t}>{g.name}</Eyebrow>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 10 }}>
              {g.tools.map((tool) => <ToolTile key={tool.id} t={t} tool={tool} />)}
            </div>
          </div>
        ))}
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext}
        right={<button style={btn(t, 'default')}>↻ Re-run full inventory</button>}
        hint="16 of 16 capable · no blockers" />
    </>
  );
}

function ToolTile({ t, tool }) {
  const [hover, setHover] = React.useState(false);
  return (
    <div
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        background: t.panel, border: `0.5px solid ${t.border}`, borderRadius: 9,
        padding: 12, boxShadow: t.shadow,
        position: 'relative', minHeight: 92,
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 7 }}>
        <StatusIcon kind="ok" t={t} size={14} />
        <div style={{ fontSize: 13, fontWeight: 700, fontFamily: FR_MONO, color: t.text }}>{tool.name}</div>
        <div style={{ fontSize: 10.5, color: t.textDim, fontFamily: FR_MONO }}>v{tool.ver}</div>
        {tool.recommended && (
          <span style={{
            fontSize: 9, fontWeight: 700, letterSpacing: 0.3, textTransform: 'uppercase',
            color: t.crest, background: t.crestSoft,
            padding: '1px 5px', borderRadius: 3, marginLeft: 'auto',
          }}>Pick</span>
        )}
      </div>
      <div style={{ marginTop: 6, display: 'flex', flexDirection: 'column', gap: 2 }}>
        {tool.caps.map((c, i) => (
          <div key={i} style={{ fontSize: 11, color: t.textDim, fontFamily: FR_MONO }}>· {c}</div>
        ))}
      </div>
      {hover && (
        <button style={{
          position: 'absolute', top: 8, right: 8,
          height: 20, padding: '0 7px', borderRadius: 4,
          background: t.panelAlt, border: `0.5px solid ${t.borderSoft}`,
          color: t.textDim, fontSize: 10, cursor: 'pointer',
        }}>↻</button>
      )}
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 7 — Subscriptions
// ─────────────────────────────────────────────────────────────
function Subscriptions({ t, onNext, onBack }) {
  const [signed, setSigned] = React.useState({
    claude: { ok: true,  email: 'jeffrey@hoopoe.so' },
    gpt:    { ok: true,  email: 'jeffrey@hoopoe.so' },
    gemini: { ok: false, email: null },
  });
  const [signingIn, setSigningIn] = React.useState(null); // 'gemini'

  const startSignIn = (id) => setSigningIn(id);
  const completeSignIn = (id) => {
    setSigned((s) => ({ ...s, [id]: { ok: true, email: 'jeffrey@hoopoe.so' } }));
    setSigningIn(null);
  };

  const providers = [
    { id: 'claude', name: 'Claude Max',     drives: 'Claude Code (Opus / Sonnet / Haiku)', dot: '#C25A2E', recommended: true },
    { id: 'gpt',    name: 'ChatGPT Pro',    drives: 'Codex CLI (GPT-5 / GPT-5 Pro)',      dot: '#10A37F' },
    { id: 'gemini', name: 'Gemini Ultra',   drives: 'Gemini CLI (3 Pro / Deep Think)',    dot: '#4285F4' },
    { id: 'oracle', name: 'ChatGPT Pro (Oracle)', drives: 'Mac-side Oracle for planning rounds', dot: '#10A37F', placeholder: true },
  ];

  const okCount = Object.values(signed).filter((s) => s.ok).length;

  return (
    <>
      <Body maxWidth={840}>
        <Eyebrow t={t}>Step 7 of 10</Eyebrow>
        <H1 t={t} sub="Hoopoe drives the agent CLIs using your existing subscriptions. No API keys, ever.">
          Sign in to your AI subscriptions
        </H1>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {providers.map((p) => (
            <SubscriptionCard
              key={p.id} t={t} provider={p}
              state={p.placeholder ? null : signed[p.id]}
              onSignIn={() => startSignIn(p.id)}
              onSignOut={() => setSigned((s) => ({ ...s, [p.id]: { ok: false, email: null } }))}
            />
          ))}
        </div>

        <div style={{
          padding: 12, borderRadius: 8,
          background: okCount > 0 ? `${t.success}14` : `${t.warn}14`,
          color: okCount > 0 ? t.success : t.warn,
          display: 'flex', alignItems: 'center', gap: 10, fontSize: 12.5,
        }}>
          <StatusIcon kind={okCount > 0 ? 'ok' : 'warn'} t={t} size={16} />
          <span style={{ fontWeight: 600 }}>
            {okCount > 0
              ? `${okCount} of 3 CLIs signed in — Hoopoe can plan, swarm, and tend.`
              : `Hoopoe will install and connect, but planning, swarm, and tending stay disabled until you sign into at least one CLI.`}
          </span>
        </div>
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} />

      {signingIn && <SignInTerminal t={t} provider={signingIn} onDone={() => completeSignIn(signingIn)} onCancel={() => setSigningIn(null)} />}
    </>
  );
}

function SubscriptionCard({ t, provider, state, onSignIn, onSignOut }) {
  const ok = state?.ok;
  const placeholder = !state;
  return (
    <Card t={t} padding={16}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
        <div style={{
          width: 36, height: 36, borderRadius: 8,
          background: `${provider.dot}22`, color: provider.dot,
          fontSize: 14, fontWeight: 700,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          flexShrink: 0,
        }}>
          {provider.name.split(' ').map((w) => w[0]).slice(0, 2).join('')}
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <div style={{ fontSize: 14, fontWeight: 600, color: t.text }}>{provider.name}</div>
            {provider.recommended && (
              <span style={{
                fontSize: 9, fontWeight: 700, letterSpacing: 0.4, textTransform: 'uppercase',
                color: t.crest, background: t.crestSoft, padding: '2px 6px', borderRadius: 3,
              }}>Recommended</span>
            )}
          </div>
          <div style={{ fontSize: 12, color: t.textDim, marginTop: 2 }}>{provider.drives}</div>
          {!placeholder && (
            <div style={{ fontSize: 11.5, color: ok ? t.success : t.warn, marginTop: 5, display: 'flex', alignItems: 'center', gap: 6 }}>
              <StatusIcon kind={ok ? 'ok' : 'warn'} t={t} size={12} />
              <span>{ok ? `Signed in as ${state.email}` : 'Not signed in'}</span>
            </div>
          )}
        </div>
        {placeholder ? (
          <span style={{ fontSize: 11.5, color: t.textDim, fontStyle: 'italic' }}>Set up next →</span>
        ) : ok ? (
          <button onClick={onSignOut} style={btn(t, 'default')}>Sign out</button>
        ) : (
          <button onClick={onSignIn} style={btn(t, 'primary')}>Open {provider.name.split(' ')[0]} login</button>
        )}
      </div>
    </Card>
  );
}

function SignInTerminal({ t, provider, onDone, onCancel }) {
  const lines = [
    `$ ssh hoopoe-prod -- gemini auth login`,
    ``,
    `Welcome to Gemini CLI — Ultra subscription`,
    ``,
    `▸ A browser will open for sign-in.`,
    `  Visit https://accounts.google.com/o/oauth2/auth?... in your browser`,
    `  and enter the code: HOOP-XR4Q-9K2T`,
    ``,
    `Waiting for confirmation…`,
    `✓ Authorized as jeffrey@hoopoe.so`,
    `✓ Subscription: Gemini Ultra (active until 2026-04-12)`,
    `✓ Token written to ~/.gemini/auth.json (mode 600)`,
    ``,
    `Sign-in complete.`,
  ];
  const [shown, setShown] = React.useState(0);
  React.useEffect(() => {
    if (shown >= lines.length) {
      const t = setTimeout(onDone, 600);
      return () => clearTimeout(t);
    }
    const id = setTimeout(() => setShown(shown + 1), 220);
    return () => clearTimeout(id);
  }, [shown]);

  return (
    <div style={{
      position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.45)',
      display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 10,
    }}>
      <div style={{
        width: 600, background: '#0e0e10', borderRadius: 12,
        boxShadow: '0 20px 60px rgba(0,0,0,0.5)',
        overflow: 'hidden', border: '0.5px solid rgba(255,255,255,0.1)',
      }}>
        <div style={{
          height: 32, background: '#1a1a1d', display: 'flex', alignItems: 'center',
          padding: '0 12px', gap: 8, borderBottom: '0.5px solid rgba(255,255,255,0.08)',
        }}>
          <Light c="#ff5f57" /><Light c="#febc2e" /><Light c="#28c840" />
          <div style={{ flex: 1, textAlign: 'center', fontSize: 11, color: 'rgba(255,255,255,0.5)' }}>
            ssh hoopoe-prod — gemini auth
          </div>
        </div>
        <div style={{
          padding: 18, fontFamily: FR_MONO, fontSize: 12, color: '#d8d8da',
          lineHeight: 1.6, minHeight: 320, maxHeight: 400, overflow: 'auto',
        }}>
          {lines.slice(0, shown).map((l, i) => (
            <div key={i} style={{
              color: l.startsWith('$') ? '#9be8a4' : l.startsWith('✓') ? '#9be8a4' : l.startsWith('▸') ? '#7fb8ff' : '#d8d8da',
            }}>{l || '\u00A0'}</div>
          ))}
          {shown < lines.length && <div style={{ color: '#9be8a4' }}>▊</div>}
        </div>
        <div style={{ padding: 10, borderTop: '0.5px solid rgba(255,255,255,0.08)', display: 'flex', justifyContent: 'flex-end' }}>
          <button onClick={onCancel} style={{
            ...btn(t, 'ghost'), color: 'rgba(255,255,255,0.5)',
          }}>Cancel</button>
        </div>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 8 — Oracle setup (ChatGPT Pro on Mac)
// ─────────────────────────────────────────────────────────────
function OracleSetup({ t, state, update, onNext, onBack }) {
  const [mode, setMode] = React.useState('intro'); // 'intro' | 'setup'
  const [phase, setPhase] = React.useState(0); // 0..4 sub-flow

  const skip = () => { update({ oracleEnabled: false }); onNext(); };
  const startSetup = () => { setMode('setup'); setPhase(1); };

  React.useEffect(() => {
    if (mode !== 'setup') return;
    if (phase === 1 || phase === 3 || phase === 4) {
      const id = setTimeout(() => setPhase(phase + 1), phase === 4 ? 1200 : 1000);
      return () => clearTimeout(id);
    }
  }, [mode, phase]);

  if (mode === 'intro') {
    return (
      <>
        <Body maxWidth={680}>
          <Eyebrow t={t}>Step 8 of 10 · Optional</Eyebrow>
          <H1 t={t} sub="ChatGPT Pro doesn't expose an API or stable CLI auth, so Hoopoe drives it via Oracle — a small Mac-side helper that talks to the chatgpt.com session in your Chrome.">
            Connect ChatGPT Pro via Oracle
          </H1>

          <Card t={t} padding={18}>
            <div style={{ fontSize: 13, color: t.text, lineHeight: 1.6 }}>
              <strong style={{ color: t.crest }}>Why this matters.</strong>{' '}
              ChatGPT Pro adds GPT-5 Pro to the planning round — a fourth perspective
              that catches things Claude, Gemini, and Codex miss. It's optional, but
              recommended if you have the subscription.
            </div>

            <div style={{
              marginTop: 14, padding: 12, borderRadius: 8,
              background: t.crestSoft, color: t.crestDeep || t.crest,
              fontSize: 12, lineHeight: 1.55,
              display: 'flex', alignItems: 'flex-start', gap: 10,
            }}>
              <StatusIcon kind="warn" t={t} size={16} />
              <div>
                <strong>While a planning round uses ChatGPT Pro, your Mac needs to be awake.</strong>{' '}
                Other models keep running when the Mac sleeps. Closing the lid mid-round
                pauses that round; it resumes when you wake up.
              </div>
            </div>
          </Card>

          <div style={{ display: 'flex', gap: 10, marginTop: 4 }}>
            <button onClick={startSetup} style={btn(t, 'primary')}>Set up ChatGPT Pro via Oracle</button>
            <button onClick={skip} style={btn(t, 'default')}>Skip for now</button>
          </div>
        </Body>
        <Footer t={t} onBack={onBack} onNext={onNext} nextLabel="Skip" primary={false} />
      </>
    );
  }

  const subSteps = [
    { id: 'install',  label: 'Install Oracle on this Mac' },
    { id: 'login',    label: 'First-time Chrome login' },
    { id: 'launch',   label: 'Configure oracle serve at login' },
    { id: 'register', label: 'Register with VPS daemon' },
    { id: 'test',     label: 'Test round-trip' },
  ];

  return (
    <>
      <Body maxWidth={760}>
        <Eyebrow t={t}>Step 8 of 10 · Oracle setup</Eyebrow>
        <H1 t={t}>Walking through Oracle setup</H1>

        <Card t={t} padding={0}>
          {subSteps.map((s, i) => {
            const idx = i + 1;
            const kind = phase > idx ? 'ok' : phase === idx ? 'spin' : 'pending';
            return (
              <div key={s.id} style={{ borderBottom: `0.5px solid ${t.borderSoft}` }}>
                <div style={{
                  display: 'flex', alignItems: 'center', gap: 12, padding: '13px 16px',
                }}>
                  <StatusIcon kind={kind} t={t} />
                  <div style={{ flex: 1, fontSize: 12.5, fontWeight: 600, color: kind === 'pending' ? t.textDim : t.text }}>
                    {s.label}
                  </div>
                </div>
                {phase === idx && (
                  <OracleSubStep t={t} step={s.id} onConfirm={() => setPhase(phase + 1)} />
                )}
                {phase > idx && (
                  <div style={{ padding: '0 16px 12px 50px', fontSize: 11.5, color: t.success, fontFamily: FR_MONO }}>
                    ✓ Done
                  </div>
                )}
              </div>
            );
          })}
        </Card>

        {phase > subSteps.length && (
          <div style={{
            padding: 12, borderRadius: 8,
            background: `${t.success}1a`, color: t.success,
            fontSize: 12.5, fontWeight: 600,
            display: 'flex', alignItems: 'center', gap: 10,
          }}>
            <StatusIcon kind="ok" t={t} size={16} />
            <span>Oracle is live · GPT-5 Pro available for planning rounds</span>
          </div>
        )}
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} nextDisabled={phase <= subSteps.length} />
    </>
  );
}

function OracleSubStep({ t, step, onConfirm }) {
  if (step === 'install') {
    return (
      <div style={{
        margin: '0 16px 14px 50px', padding: 12,
        background: '#0e0e10', borderRadius: 8,
        fontFamily: FR_MONO, fontSize: 11.5, color: '#d8d8da', lineHeight: 1.6,
      }}>
        <div style={{ color: '#9be8a4' }}>$ brew install steipete/tap/oracle</div>
        <div>==&gt; Downloading oracle 0.4.1 from steipete/tap…</div>
        <div>==&gt; Installing dependencies: chrome-cli, jq</div>
        <div style={{ color: '#9be8a4' }}>$ npm install -g @steipete/oracle</div>
        <div>added 23 packages in 4s</div>
        <div style={{ color: '#7fb8ff' }}>oracle 0.4.1 installed at /opt/homebrew/bin/oracle</div>
      </div>
    );
  }
  if (step === 'login') {
    return (
      <div style={{ margin: '0 16px 14px 50px' }}>
        <div style={{ fontSize: 12, color: t.textDim, lineHeight: 1.6, marginBottom: 10 }}>
          Oracle opens Chrome to <strong>chatgpt.com</strong>. Sign in there.
          Hoopoe never sees your password — Chrome stores the session.
        </div>
        <button onClick={onConfirm} style={btn(t, 'primary')}>I'm signed in →</button>
      </div>
    );
  }
  if (step === 'launch') {
    return (
      <div style={{ margin: '0 16px 14px 50px', fontSize: 11.5, color: t.textDim, fontFamily: FR_MONO }}>
        Writing ~/Library/LaunchAgents/com.hoopoe.oracle.plist…
        <a href="#" onClick={(e) => e.preventDefault()} style={{ color: t.accent, marginLeft: 8 }}>review the plist</a>
      </div>
    );
  }
  if (step === 'register') {
    return (
      <div style={{ margin: '0 16px 14px 50px', fontSize: 11.5, color: t.textDim, fontFamily: FR_MONO }}>
        oracle register --remote-host hoopoe-prod --remote-token *********…
      </div>
    );
  }
  if (step === 'test') {
    return (
      <div style={{
        margin: '0 16px 14px 50px', padding: 12,
        background: t.panelAlt, borderRadius: 8,
        fontFamily: FR_MONO, fontSize: 11.5, color: t.text, lineHeight: 1.6,
      }}>
        <div style={{ color: t.crest }}>&gt; In one word, what color is a hoopoe's crest?</div>
        <div style={{ color: t.success, marginTop: 4 }}>russet</div>
        <div style={{ color: t.textDim, marginTop: 6, fontSize: 11 }}>round-trip 1.4s · GPT-5 Pro · session healthy</div>
      </div>
    );
  }
  return null;
}

// ─────────────────────────────────────────────────────────────
// Step 9 — First project
// ─────────────────────────────────────────────────────────────
function FirstProject({ t, state, update, onNext, onBack }) {
  const [choice, setChoice] = React.useState('import'); // 'import' | 'none' | 'sample'
  const [path, setPath] = React.useState('git@github.com:jeffrey/atlas-notes.git');
  const [cloning, setCloning] = React.useState(false);
  const [progress, setProgress] = React.useState(0);
  const [done, setDone] = React.useState(false);

  const startClone = () => {
    setCloning(true); setProgress(0); setDone(false);
    const id = setInterval(() => {
      setProgress((p) => {
        if (p >= 100) { clearInterval(id); setCloning(false); setDone(true); update({ importedRepo: 'atlas-notes' }); return 100; }
        return p + 8;
      });
    }, 120);
  };

  return (
    <>
      <Body maxWidth={780}>
        <Eyebrow t={t}>Step 9 of 10 · Optional</Eyebrow>
        <H1 t={t} sub="You can import an existing repo now, or skip this and add projects later from the Projects sidebar.">
          Import your first project
        </H1>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
          <ProjectCard t={t} selected={choice === 'import'} onClick={() => setChoice('import')}
            icon="↘" title="Import an existing repo"
            desc="Clone a Git repo onto the VPS, or point at a path that's already there."
          />
          <ProjectCard t={t} selected={choice === 'none'} onClick={() => setChoice('none')}
            icon="○" title="Start with no project"
            desc="Open Hoopoe empty. You can add projects any time."
          />
        </div>

        {choice === 'import' && (
          <Card t={t} padding={16}>
            <Field t={t} label="Repo URL or VPS path">
              <input value={path} onChange={(e) => setPath(e.target.value)}
                style={{ ...input(t), fontFamily: FR_MONO, fontSize: 12.5 }}
              />
            </Field>
            <div style={{ marginTop: 12, display: 'flex', alignItems: 'center', gap: 10 }}>
              <button onClick={startClone} disabled={cloning} style={{
                ...btn(t, 'default'), opacity: cloning ? 0.55 : 1,
              }}>
                {cloning ? 'Cloning…' : done ? 'Re-clone' : 'Clone to VPS'}
              </button>
              {(cloning || done) && (
                <div style={{ flex: 1 }}>
                  <div style={{ height: 4, background: t.panelAlt, borderRadius: 2, overflow: 'hidden' }}>
                    <div style={{ width: `${progress}%`, height: '100%', background: done ? t.success : t.crest, transition: 'width 100ms' }} />
                  </div>
                  <div style={{ fontSize: 11, color: done ? t.success : t.textDim, marginTop: 4, fontFamily: FR_MONO }}>
                    {done ? `✓ Cloned · 1,247 files · ${path.split('/').pop().replace('.git','')} ready` : `${progress}% · resolving deltas…`}
                  </div>
                </div>
              )}
            </div>
          </Card>
        )}

        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <button onClick={() => { setChoice('sample'); update({ sample: true, importedRepo: 'hoopoe-example' }); }} style={{
            ...btn(t, 'ghost'), color: t.accent,
          }}>
            ↗ Import a sample project
          </button>
          <span style={{ fontSize: 11.5, color: t.textDim }}>· clones a small Hoopoe example repo</span>
        </div>
      </Body>
      <Footer t={t} onBack={onBack} onNext={onNext} />
    </>
  );
}

function ProjectCard({ t, selected, onClick, icon, title, desc }) {
  return (
    <div onClick={onClick} style={{
      background: t.panel,
      border: `1px solid ${selected ? t.crest : t.borderSoft}`,
      borderRadius: 10, padding: 14,
      cursor: 'pointer',
      boxShadow: selected ? `0 0 0 3px ${t.crest}22` : 'none',
      display: 'flex', alignItems: 'flex-start', gap: 12,
    }}>
      <div style={{
        width: 32, height: 32, borderRadius: 8,
        background: selected ? t.crest : t.panelAlt,
        color: selected ? '#fff' : t.crest,
        fontSize: 16, fontWeight: 700,
        display: 'flex', alignItems: 'center', justifyContent: 'center',
      }}>{icon}</div>
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 13, fontWeight: 600 }}>{title}</div>
        <div style={{ fontSize: 12, color: t.textDim, marginTop: 4, lineHeight: 1.5 }}>{desc}</div>
      </div>
    </div>
  );
}

// ─────────────────────────────────────────────────────────────
// Step 10 — Ready
// ─────────────────────────────────────────────────────────────
function ReadyScreen({ t, state, update, onFinish, onBack }) {
  return (
    <Body maxWidth={760}>
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', textAlign: 'center', gap: 14, marginBottom: 4 }}>
        <div style={{
          width: 64, height: 64, borderRadius: 18,
          background: `${t.success}22`, color: t.success,
          display: 'flex', alignItems: 'center', justifyContent: 'center',
          fontSize: 30, fontWeight: 700,
        }}>✓</div>
        <div style={{ fontSize: 24, fontWeight: 700, letterSpacing: -0.4 }}>You're ready to fly</div>
        <div style={{ fontSize: 13, color: t.textDim, maxWidth: 480, lineHeight: 1.55 }}>
          Hoopoe is paired with your VPS, all tools are installed, and your subscriptions are live.
          Open Hoopoe to start your first plan.
        </div>
      </div>

      <Card t={t} padding={18}>
        <Eyebrow t={t}>Setup summary</Eyebrow>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 0, marginTop: 10 }}>
          <SummaryRow t={t} label="VPS" value={state.sshHost || 'jeffrey@hoopoe.farm:22'} />
          <SummaryRow t={t} label="Daemon" value="hoopoed v0.4.2 · systemd healthy" />
          <SummaryRow t={t} label="Tunnel" value="localhost:54231 ↔ 127.0.0.1:8800" />
          <SummaryRow t={t} label="Tools" value="16 of 16 installed and capable" />
          <SummaryRow t={t} label="Subscriptions" value={state.oracleEnabled ? 'Claude Max · ChatGPT Pro · Gemini Ultra · Oracle' : 'Claude Max · ChatGPT Pro · Gemini Ultra'} />
          <SummaryRow t={t} label="Skills" value="vibing-with-ntm 2.1.0 · ntm 1.8.3 · jsm 2.1.0" />
          <SummaryRow t={t} label="First project" value={state.importedRepo || 'none yet'} />
        </div>
      </Card>

      <div style={{ display: 'flex', justifyContent: 'center', marginTop: 4 }}>
        <button onClick={onFinish} style={{
          ...btn(t, 'primary'), height: 42, padding: '0 28px', fontSize: 14,
        }}>
          Open Hoopoe<span style={{ fontSize: 12 }}>→</span>
        </button>
      </div>

      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: 16, fontSize: 11.5 }}>
        <a href="#" onClick={(e) => e.preventDefault()} style={{ color: t.textDim }}>View setup log</a>
        <span style={{ color: t.textMute }}>·</span>
        <a href="#" onClick={(e) => e.preventDefault()} style={{ color: t.textDim }}>Export setup report</a>
      </div>

      <label style={{
        display: 'flex', alignItems: 'flex-start', gap: 8,
        padding: '10px 14px', background: t.panelAlt, borderRadius: 8,
        fontSize: 11.5, color: t.textDim, lineHeight: 1.5,
        cursor: 'pointer', marginTop: 4,
      }}>
        <input
          type="checkbox" checked={state.telemetry}
          onChange={(e) => update({ telemetry: e.target.checked })}
          style={{ marginTop: 2 }}
        />
        <span>
          <strong style={{ color: t.text }}>Send anonymous setup telemetry.</strong>{' '}
          Step durations and success/failure counts only — no paths, repo names, or content.
          Off by default. You can change this any time in Settings.
        </span>
      </label>

      <div style={{ height: 8 }} />
      <div style={{ display: 'flex' }}>
        <button onClick={onBack} style={btn(t, 'ghost')}>← Back</button>
      </div>
    </Body>
  );
}

function SummaryRow({ t, label, value }) {
  return (
    <div style={{
      padding: '8px 0', borderBottom: `0.5px solid ${t.borderSoft}`,
      display: 'flex', alignItems: 'baseline', gap: 12,
    }}>
      <div style={{ width: 130, fontSize: 11, color: t.textMute, fontWeight: 600, letterSpacing: 0.3, textTransform: 'uppercase' }}>{label}</div>
      <div style={{ flex: 1, fontSize: 12.5, color: t.text, fontFamily: FR_MONO }}>{value}</div>
      <StatusIcon kind="ok" t={t} size={12} />
    </div>
  );
}

Object.assign(window, { FirstRun });
