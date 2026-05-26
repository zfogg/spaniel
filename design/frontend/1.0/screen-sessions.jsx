// Spaniel — Sessions page (Drift direction).  Lists all named sessions,
// shows which is active and which is baseline, and makes the path to a diff
// explicit: pick a baseline (★), pick a comparison, click Compare.

const { useState: useSessState } = React;
const { Chrome, Sidebar, SbGroup, SbItem, Badge, Frame, fmtMs: _fms } = window.UI;

// ── Sample sessions ──────────────────────────────────────────────────────
const SAMPLE_SESSIONS = [
  { id:'main',                name:'main',                kind:'branch', traces:11,  spans:264,  p95:498, slow:0, n1:0, errs:0,
    created:'2d ago', last:'2h ago', size:'48 MB',  note:'last green build · auto-snapshot' },
  { id:'feat-checkout',       name:'feat/checkout',       kind:'branch', traces:34,  spans:818,  p95:612, slow:3, n1:1, errs:0,
    created:'5h ago', last:'just now', size:'112 MB', note:'live · 7 lint warnings, 1 n+1' },
  { id:'scratch',             name:'scratch',             kind:'adhoc',  traces:2,   spans:18,   p95:74,  slow:0, n1:0, errs:0,
    created:'3d ago', last:'3d ago', size:'2 MB',   note:'one-off · auto-prune in 21h' },
  { id:'hotfix-redis-pool',   name:'hotfix/redis-pool',   kind:'branch', traces:8,   spans:184,  p95:188, slow:0, n1:0, errs:1,
    created:'6h ago', last:'5h ago', size:'29 MB',  note:'stashed · reproduces redis P0' },
  { id:'perf-batch-pricing',  name:'perf/batch-pricing',  kind:'branch', traces:14,  spans:288,  p95:432, slow:1, n1:0, errs:0,
    created:'1d ago', last:'18h ago', size:'52 MB', note:'in-progress · batching attempt' },
];

// Sample recent diffs the user has run from this machine.
const RECENT_DIFFS = [
  { a:'main', b:'feat/checkout',      when:'12m ago', deltaMs:+114, deltaSpans:-4, summary:'+1 n+1 found · stripe regressed' },
  { a:'main', b:'perf/batch-pricing', when:'1h ago',  deltaMs:-66,  deltaSpans:-3, summary:'pricing batched · 6→1 db calls' },
  { a:'main', b:'hotfix/redis-pool',  when:'5h ago',  deltaMs:-310, deltaSpans:0,  summary:'redis stampede gone' },
];

// ── Atoms ────────────────────────────────────────────────────────────────
function StarButton({ on, onClick, title }) {
  return (
    <button type="button" onClick={onClick} title={title}
      style={{
        width:28, height:28, borderRadius:6, padding:0,
        background: on ? 'color-mix(in oklch, var(--warn) 30%, var(--surface))' : 'transparent',
        border: on ? '1px solid var(--warn)' : '1px solid var(--line)',
        cursor:'pointer', outline:'none',
        display:'inline-flex', alignItems:'center', justifyContent:'center',
      }}>
      <svg width="14" height="14" viewBox="0 0 14 14">
        <path d="M7 1.5l1.7 3.5 3.8.5-2.8 2.7.7 3.8L7 10.2 3.6 12l.7-3.8L1.5 5.5l3.8-.5L7 1.5z"
          fill={on ? 'var(--warn)' : 'none'}
          stroke={on ? 'var(--warnInk)' : 'var(--ink3)'} strokeWidth="1" strokeLinejoin="round" />
      </svg>
    </button>
  );
}

function DotPill({ tone = 'neutral', children }) {
  const tones = {
    neutral: { bg:'var(--surface2)',     fg:'var(--ink2)',     bd:'var(--line)' },
    ok:      { bg:'color-mix(in oklch, var(--ok) 22%, var(--surface))',
               fg:'#3e6a3e',   bd:'color-mix(in oklch, var(--ok) 40%, transparent)' },
    accent:  { bg:'color-mix(in oklch, var(--accent) 18%, var(--surface))',
               fg:'var(--accentInk)', bd:'color-mix(in oklch, var(--accent) 40%, transparent)' },
    warn:    { bg:'color-mix(in oklch, var(--warn) 28%, var(--surface))',
               fg:'var(--warnInk)',   bd:'color-mix(in oklch, var(--warn) 50%, transparent)' },
    danger:  { bg:'color-mix(in oklch, var(--danger) 28%, var(--surface))',
               fg:'var(--dangerInk)', bd:'color-mix(in oklch, var(--danger) 50%, transparent)' },
  };
  const t = tones[tone];
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:3,
      padding:'1px 6px', borderRadius:10,
      fontFamily:'var(--mono)', fontSize:9.5, fontWeight:600,
      background:t.bg, color:t.fg, border:`1px solid ${t.bd}`,
      whiteSpace:'nowrap', lineHeight:1.4,
    }}>{children}</span>
  );
}

function Btn({ tone = 'default', icon, kbd, disabled, onClick, children, small }) {
  const tones = {
    default:  { bg:'var(--surface)',   fg:'var(--ink)',         bd:'var(--line)' },
    primary:  { bg:'var(--accent)',    fg:'var(--surface)',     bd:'var(--accent)' },
    ghost:    { bg:'transparent',       fg:'var(--ink2)',        bd:'var(--line)' },
    danger:   { bg:'transparent',       fg:'var(--dangerInk)',   bd:'var(--danger)' },
  };
  const t = tones[tone];
  return (
    <button type="button" onClick={onClick} disabled={disabled}
      style={{
        display:'inline-flex', alignItems:'center', gap:7,
        padding: small ? '0 10px' : '0 14px', height: small ? 26 : 30, borderRadius:6,
        background: disabled ? 'var(--surface2)' : t.bg,
        color: disabled ? 'var(--ink3)' : t.fg,
        border: `1px solid ${disabled ? 'var(--line)' : t.bd}`,
        fontFamily:'var(--sans)', fontSize:12, fontWeight:600,
        cursor: disabled ? 'not-allowed' : 'pointer', outline:'none',
        whiteSpace:'nowrap',
      }}>
      {icon}
      <span>{children}</span>
      {kbd ? <span style={{
        fontFamily:'var(--mono)', fontSize:10, fontWeight:500,
        background:'color-mix(in oklch, currentColor 14%, transparent)',
        padding:'1px 5px', borderRadius:4, marginLeft:2,
      }}>{kbd}</span> : null}
    </button>
  );
}

function IconCompare() {
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
      <path d="M2.5 4h5M5 2l-2.5 2L5 6" stroke="currentColor" strokeWidth="1.4"
        strokeLinecap="round" strokeLinejoin="round" />
      <path d="M11.5 10h-5M9 8l2.5 2L9 12" stroke="currentColor" strokeWidth="1.4"
        strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// ── Step explainer for the diff workflow ─────────────────────────────────
function HowToDiff() {
  const steps = [
    { n:'1', title:'Mark a baseline',
      body:'Click ★ on the session you trust — usually main.  That run becomes your reference.' },
    { n:'2', title:'Switch and re-run',
      body:'Check out a new branch (or hit the same endpoint after editing).  Spans land in the new session.' },
    { n:'3', title:'Open the diff',
      body:'Select any session ↔ baseline pair and click Compare.  Span shapes, deltas, N+1s — side-by-side.' },
  ];
  return (
    <div style={{
      display:'grid', gridTemplateColumns:'repeat(3, 1fr)',
      gap:14, marginBottom:20,
    }}>
      {steps.map((s, i) => (
        <div key={i} style={{
          background:'var(--surface)', border:'1px solid var(--line)', borderRadius:10,
          padding:'14px 16px', position:'relative',
        }}>
          <div style={{
            display:'inline-flex', alignItems:'center', justifyContent:'center',
            width:24, height:24, borderRadius:24,
            background:'var(--accent)', color:'var(--surface)',
            fontFamily:'var(--mono)', fontSize:12, fontWeight:700,
            marginBottom:8,
          }}>{s.n}</div>
          <div style={{
            fontFamily:'var(--sans)', fontSize:13, fontWeight:600,
            color:'var(--ink)', marginBottom:4,
          }}>{s.title}</div>
          <div style={{
            fontFamily:'var(--sans)', fontSize:12, color:'var(--ink2)', lineHeight:1.5,
          }}>{s.body}</div>
        </div>
      ))}
    </div>
  );
}

// ── Session row ──────────────────────────────────────────────────────────
function SessionRow({ s, isActive, isBaseline, isCompare, onPickBaseline, onPickCompare, onSwitch }) {
  const hot = s.n1 > 0 || s.errs > 0;
  return (
    <div style={{
      display:'grid',
      gridTemplateColumns:'34px minmax(0,1fr) 96px 64px 64px 64px 96px 152px',
      gap:10, alignItems:'center', padding:'12px 14px',
      borderBottom:'1px solid var(--line2)',
      background:
        isBaseline ? 'color-mix(in oklch, var(--warn) 8%, var(--surface))'
        : isCompare ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
        : isActive ? 'var(--surface)'
        : 'transparent',
      borderLeft:
        isBaseline ? '2px solid var(--warn)'
        : isCompare ? '2px solid var(--accent)'
        : isActive ? '2px solid var(--ok)'
        : '2px solid transparent',
    }}>
      {/* Star (baseline) */}
      <StarButton on={isBaseline} onClick={() => onPickBaseline(s.id)}
        title={isBaseline ? 'Baseline (click to unpin)' : 'Mark as baseline'} />

      {/* Name + note */}
      <div style={{ minWidth:0 }}>
        <div style={{ display:'flex', alignItems:'center', gap:6, flexWrap:'wrap' }}>
          {s.kind === 'branch' ? <BranchGlyph /> : <ScratchGlyph />}
          <span style={{
            fontFamily:'var(--mono)', fontSize:13, fontWeight:600, color:'var(--ink)',
            overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
            maxWidth:'100%',
          }}>{s.name}</span>
          {isActive ? <DotPill tone="ok">● active</DotPill> : null}
          {isBaseline ? <DotPill tone="warn">★ baseline</DotPill> : null}
          {hot ? <DotPill tone="danger">{s.n1 > 0 ? `${s.n1} n+1` : `${s.errs} err`}</DotPill> : null}
        </div>
        <div style={{
          fontFamily:'var(--sans)', fontSize:11, color:'var(--ink3)', marginTop:3,
        }}>{s.note}</div>
      </div>

      {/* Created · Last */}
      <div>
        <div style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)' }}>{s.last}</div>
        <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>created {s.created}</div>
      </div>

      <NumCell label="traces" value={s.traces} />
      <NumCell label="spans"  value={s.spans} />
      <NumCell label="p95"    value={s.p95 + 'ms'} hot={s.p95 > 500} />

      {/* Size */}
      <div>
        <div style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)' }}>{s.size}</div>
        <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>duckdb</div>
      </div>

      {/* Actions */}
      <div style={{ display:'flex', gap:6, justifyContent:'flex-end' }}>
        {isCompare ? (
          <Btn small tone="ghost" onClick={() => onPickCompare(null)}>− deselect</Btn>
        ) : isBaseline ? (
          <Btn small tone="ghost" disabled>baseline</Btn>
        ) : (
          <Btn small onClick={() => onPickCompare(s.id)}>+ compare</Btn>
        )}
        {!isActive ? (
          <Btn small tone="ghost" onClick={() => onSwitch(s.id)}>switch</Btn>
        ) : null}
      </div>
    </div>
  );
}

function NumCell({ label, value, hot }) {
  return (
    <div>
      <div style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600,
        color: hot ? 'var(--dangerInk)' : 'var(--ink)', lineHeight:1.1 }}>{value}</div>
      <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em', marginTop:2 }}>{label}</div>
    </div>
  );
}

function BranchGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" style={{ flex:'0 0 auto' }}>
      <circle cx="3" cy="3" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <circle cx="3" cy="11" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <circle cx="11" cy="7" r="1.6" fill="none" stroke="var(--ink2)" strokeWidth="1.2" />
      <path d="M3 4.5v5M3.8 11c2.2-.2 6-.6 6.2-2.6M3.8 3c2.2.2 6 .6 6.2 2.6"
        stroke="var(--ink2)" strokeWidth="1.2" fill="none" strokeLinecap="round" />
    </svg>
  );
}
function ScratchGlyph() {
  return (
    <svg width="13" height="13" viewBox="0 0 14 14" style={{ flex:'0 0 auto' }}>
      <path d="M2 7c2-3 3.5-3 5 0s3 3 5 0" stroke="var(--ink2)" strokeWidth="1.2" fill="none"
        strokeLinecap="round" />
      <circle cx="2" cy="7" r="1" fill="var(--ink2)" />
      <circle cx="12" cy="7" r="1" fill="var(--ink2)" />
    </svg>
  );
}

// ── The Sessions screen ──────────────────────────────────────────────────
function Sessions({ theme }) {
  const [baselineId, setBaselineId] = useSessState('main');
  const [compareId,  setCompareId]  = useSessState('feat-checkout');
  const [activeId,   setActiveId]   = useSessState('feat-checkout');
  const [filter,     setFilter]     = useSessState('all');

  const onPickBaseline = (id) => {
    if (baselineId === id) { setBaselineId(null); return; }
    if (compareId === id) setCompareId(null); // can't be both
    setBaselineId(id);
  };
  const onPickCompare = (id) => {
    if (id === baselineId) return;
    setCompareId(id);
  };
  const onSwitch = (id) => setActiveId(id);

  const baseline = SAMPLE_SESSIONS.find(s => s.id === baselineId);
  const compare  = SAMPLE_SESSIONS.find(s => s.id === compareId);
  const canDiff  = baseline && compare && baseline.id !== compare.id;

  return (
    <Frame>
      <Chrome active="sessions" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <Sidebar>
          <SbGroup title="filter">
            <SbItem active={filter==='all'}      dot="var(--ink3)" count={SAMPLE_SESSIONS.length}
              onClick={() => setFilter('all')}>all sessions</SbItem>
            <SbItem active={filter==='branches'} dot="var(--accent)" count="4"
              onClick={() => setFilter('branches')}>branches</SbItem>
            <SbItem active={filter==='adhoc'}    dot="var(--warn)" count="1"
              onClick={() => setFilter('adhoc')}>scratch / ad-hoc</SbItem>
            <SbItem active={filter==='hot'}      dot="var(--danger)" count="2"
              onClick={() => setFilter('hot')}>with warnings</SbItem>
          </SbGroup>
          <SbGroup title="actions">
            <SbItem dot="var(--accent)" onClick={() => {}}>+ new session</SbItem>
            <SbItem dot="var(--ink3)" onClick={() => {}}>import from prod…</SbItem>
            <SbItem dot="var(--ink3)" onClick={() => {}}>prune unused</SbItem>
          </SbGroup>
          <div style={{ flex:1 }} />
          <SbGroup title="diff selection">
            <DiffSelectionMini baseline={baseline} compare={compare} />
          </SbGroup>
        </Sidebar>

        <div style={{ flex:1, overflow:'hidden auto',
          display:'flex', flexDirection:'column' }}>
          {/* Page header */}
          <div style={{ padding:'22px 24px 0 24px' }}>
            <div style={{
              fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.18em', marginBottom:6,
            }}>spaniel sessions</div>
            <div style={{ display:'flex', alignItems:'baseline', gap:14 }}>
              <h1 style={{
                margin:0, fontFamily:'var(--serif)', fontSize:28, fontWeight:600,
                letterSpacing:'-0.02em', color:'var(--ink)',
              }}>Sessions</h1>
              <span style={{ fontFamily:'var(--sans)', fontSize:13, color:'var(--ink2)' }}>
                Named windows of telemetry.  Each branch usually gets its own.
              </span>
            </div>
          </div>

          {/* Workflow card */}
          <div style={{ padding:'18px 24px 4px' }}>
            <HowToDiff />
          </div>

          {/* Sessions table */}
          <div style={{ padding:'4px 24px' }}>
            {/* Column header */}
            <div style={{
              display:'grid',
              gridTemplateColumns:'34px minmax(0,1fr) 96px 64px 64px 64px 96px 152px',
              gap:10, padding:'8px 14px', borderBottom:'1px solid var(--line)',
              fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.14em',
              background:'var(--surface2)', borderRadius:'10px 10px 0 0',
              border:'1px solid var(--line)',
            }}>
              <div title="Mark baseline">★</div>
              <div>session · note</div>
              <div>activity</div>
              <div>traces</div>
              <div>spans</div>
              <div>p95</div>
              <div>size</div>
              <div style={{ textAlign:'right' }}>actions</div>
            </div>
            <div style={{
              background:'var(--surface)',
              border:'1px solid var(--line)', borderTop:'none',
              borderRadius:'0 0 10px 10px', overflow:'hidden',
            }}>
              {SAMPLE_SESSIONS.map((s) => (
                <SessionRow key={s.id} s={s}
                  isActive={s.id === activeId}
                  isBaseline={s.id === baselineId}
                  isCompare={s.id === compareId}
                  onPickBaseline={onPickBaseline}
                  onPickCompare={onPickCompare}
                  onSwitch={onSwitch} />
              ))}
            </div>
          </div>

          {/* Recent diffs */}
          <div style={{ padding:'18px 24px 4px' }}>
            <div style={{
              display:'flex', alignItems:'baseline', gap:10, marginBottom:8,
            }}>
              <h2 style={{
                margin:0, fontFamily:'var(--serif)', fontSize:16, fontWeight:600, color:'var(--ink)',
              }}>Recent diffs</h2>
              <span style={{ fontFamily:'var(--sans)', fontSize:11, color:'var(--ink2)' }}>
                cached locally · re-open any to compare again
              </span>
            </div>
            <div style={{
              background:'var(--surface)', border:'1px solid var(--line)', borderRadius:10,
              overflow:'hidden',
            }}>
              {RECENT_DIFFS.map((d, i) => (
                <div key={i} style={{
                  display:'grid', gridTemplateColumns:'minmax(0,1fr) 96px 96px 88px 96px',
                  gap:12, padding:'10px 16px', alignItems:'center',
                  borderBottom: i < RECENT_DIFFS.length-1 ? '1px solid var(--line2)' : 'none',
                }}>
                  <div style={{ display:'flex', alignItems:'center', gap:8, minWidth:0 }}>
                    <span style={{
                      fontFamily:'var(--mono)', fontSize:11.5, color:'var(--ink2)',
                    }}>{d.a}</span>
                    <span style={{ color:'var(--ink3)' }}>→</span>
                    <span style={{
                      fontFamily:'var(--mono)', fontSize:11.5, fontWeight:600, color:'var(--ink)',
                    }}>{d.b}</span>
                  </div>
                  <Delta n={d.deltaMs} suffix="ms" lowerIsBetter />
                  <Delta n={d.deltaSpans} suffix=" spans" lowerIsBetter />
                  <div style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)' }}>
                    {d.when}
                  </div>
                  <div style={{ fontFamily:'var(--sans)', fontSize:12, color:'var(--ink2)',
                    overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>
                    {d.summary}
                  </div>
                </div>
              ))}
            </div>
          </div>

          <div style={{ height:80 }} />
        </div>
      </div>

      {/* Sticky compare bar */}
      <CompareBar baseline={baseline} compare={compare} canDiff={canDiff} />
    </Frame>
  );
}

function Delta({ n, suffix, lowerIsBetter }) {
  const better = lowerIsBetter ? n < 0 : n > 0;
  const isZero = n === 0;
  return (
    <span style={{
      fontFamily:'var(--mono)', fontSize:12, fontWeight:600,
      color: isZero ? 'var(--ink3)' : better ? '#3e6a3e' : 'var(--dangerInk)',
    }}>{n > 0 ? '+' : ''}{n}{suffix}</span>
  );
}

function DiffSelectionMini({ baseline, compare }) {
  return (
    <div style={{
      padding:'8px 10px', borderRadius:8, border:'1px solid var(--line)',
      background:'var(--surface)', display:'flex', flexDirection:'column', gap:6,
    }}>
      <div style={{ display:'flex', alignItems:'center', gap:6 }}>
        <span style={{
          width:16, height:16, borderRadius:4, display:'inline-flex',
          alignItems:'center', justifyContent:'center',
          background:'color-mix(in oklch, var(--warn) 30%, var(--surface))',
          color:'var(--warnInk)', fontSize:10, fontWeight:700,
        }}>★</span>
        <span style={{
          fontFamily:'var(--mono)', fontSize:11, color: baseline ? 'var(--ink)' : 'var(--ink3)',
          overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', flex:1,
        }}>{baseline ? baseline.name : 'no baseline pinned'}</span>
      </div>
      <div style={{ display:'flex', alignItems:'center', gap:6 }}>
        <span style={{
          width:16, height:16, borderRadius:4, display:'inline-flex',
          alignItems:'center', justifyContent:'center',
          background:'color-mix(in oklch, var(--accent) 30%, var(--surface))',
          color:'var(--accentInk)', fontSize:10, fontWeight:700,
        }}>+</span>
        <span style={{
          fontFamily:'var(--mono)', fontSize:11, color: compare ? 'var(--ink)' : 'var(--ink3)',
          overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', flex:1,
        }}>{compare ? compare.name : 'pick a session'}</span>
      </div>
    </div>
  );
}

function CompareBar({ baseline, compare, canDiff }) {
  return (
    <div style={{
      borderTop:'1px solid var(--line)',
      background:'var(--surface)',
      padding:'10px 18px',
      display:'flex', alignItems:'center', gap:14,
    }}>
      <div style={{
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em',
      }}>diff</div>
      <span style={{
        fontFamily:'var(--mono)', fontSize:12, fontWeight:600,
        padding:'4px 10px', borderRadius:6,
        background:'color-mix(in oklch, var(--warn) 18%, var(--surface))',
        color:'var(--warnInk)', border:'1px solid color-mix(in oklch, var(--warn) 35%, transparent)',
      }}>★ {baseline ? baseline.name : '— pick baseline —'}</span>
      <span style={{ fontFamily:'var(--mono)', color:'var(--ink3)' }}>→</span>
      <span style={{
        fontFamily:'var(--mono)', fontSize:12, fontWeight:600,
        padding:'4px 10px', borderRadius:6,
        background:'color-mix(in oklch, var(--accent) 16%, var(--surface))',
        color:'var(--accentInk)', border:'1px solid color-mix(in oklch, var(--accent) 35%, transparent)',
      }}>+ {compare ? compare.name : '— pick comparison —'}</span>
      <div style={{ flex:1 }} />
      <span style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
      }}>or run <span style={{
        background:'var(--surface2)', padding:'1px 6px', borderRadius:4, color:'var(--ink2)',
      }}>spaniel diff --baseline {baseline ? baseline.name : 'main'}</span></span>
      <Btn tone="primary" disabled={!canDiff} icon={<IconCompare />} kbd="↵">
        Compare sessions
      </Btn>
    </div>
  );
}

window.Sessions = Sessions;
