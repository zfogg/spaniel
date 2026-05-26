// Remaining Spaniel screens: empty state, trace list, service map, session
// diff, lint warnings, command palette.

const { Chrome, Sidebar, SbGroup, SbItem, SvcChip, Badge, PanelHead, Frame, fmtMs } = window.UI;

// ─── EmptyState — first-run, "send your first trace" ─────────────────────
function EmptyState({ theme }) {
  const langs = [
    { name:'Go',     snip:`go get spaniel.dev/sdk\nexport OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` },
    { name:'Node',   snip:`npm i @spaniel/otel\nrequire('@spaniel/otel').start()` },
    { name:'Python', snip:`pip install spaniel-otel\nspaniel.instrument()` },
    { name:'Rust',   snip:`cargo add spaniel-tracing` },
  ];
  return (
    <Frame>
      <Chrome active="traces" theme={theme} />
      <div style={{
        flex:1, padding:'28px 32px', display:'flex', flexDirection:'column',
        gap:18, alignItems:'center', justifyContent:'center',
      }}>
        <div style={{ width:'100%', maxWidth:560 }}>
          <div style={{
            display:'flex', alignItems:'center', gap:8,
            fontFamily:'var(--mono)', fontSize:10, textTransform:'uppercase',
            letterSpacing:'0.18em', color:'var(--ink3)', marginBottom:10,
          }}>
            <span style={{ width:6, height:6, borderRadius:6, background:'var(--ok)' }} />
            listening — no traces yet
          </div>
          <h1 style={{
            fontFamily:'var(--serif)', fontSize:30, fontWeight:600,
            color:'var(--ink)', margin:0, lineHeight:1.1, letterSpacing:'-0.02em',
          }}>
            Point your app at <span style={{ fontFamily:'var(--mono)', fontSize:23,
              background:'var(--surface2)', padding:'2px 8px', borderRadius:6,
              color:'var(--accentInk)' }}>localhost:4318</span>
          </h1>
          <p style={{
            fontFamily:'var(--sans)', fontSize:13.5, color:'var(--ink2)',
            marginTop:10, marginBottom:18, lineHeight:1.5, maxWidth:480,
          }}>
            Spaniel speaks OTLP over gRPC and HTTP — protobuf and JSON.  Spans
            stream in live.  Nothing leaves your machine.
          </p>

          {/* Language tabs */}
          <div style={{
            background:'var(--surface)', border:'1px solid var(--line)',
            borderRadius:10, boxShadow:'var(--shadow)', overflow:'hidden',
          }}>
            <div style={{ display:'flex', borderBottom:'1px solid var(--line)',
              background:'var(--surface2)' }}>
              {langs.map((l, i) => (
                <span key={l.name} style={{
                  padding:'8px 14px',
                  fontFamily:'var(--mono)', fontSize:11, fontWeight:600,
                  color: i === 0 ? 'var(--ink)' : 'var(--ink3)',
                  background: i === 0 ? 'var(--surface)' : 'transparent',
                  borderRight: i < langs.length-1 ? '1px solid var(--line)' : 'none',
                  borderBottom: i === 0 ? '1px solid var(--surface)' : 'none',
                  marginBottom: i === 0 ? -1 : 0,
                }}>{l.name}</span>
              ))}
            </div>
            <pre style={{
              margin:0, padding:'14px 16px',
              fontFamily:'var(--mono)', fontSize:11.5, color:'var(--ink)',
              background:'var(--surface)', lineHeight:1.6,
              whiteSpace:'pre',
            }}>
              <span style={{ color:'var(--ink3)' }}># 1. install</span>{'\n'}
              <span>{langs[0].snip.split('\n')[0]}</span>{'\n\n'}
              <span style={{ color:'var(--ink3)' }}># 2. point OTLP at spaniel</span>{'\n'}
              <span>{langs[0].snip.split('\n')[1]}</span>{'\n\n'}
              <span style={{ color:'var(--ink3)' }}># 3. run your app — traces appear here</span>
            </pre>
          </div>

          <div style={{
            marginTop:16, display:'flex', alignItems:'center', gap:10,
            fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)',
          }}>
            <span style={{
              padding:'4px 8px', border:'1px solid var(--line)', borderRadius:5,
              color:'var(--ink2)', background:'var(--surface)',
            }}>spaniel docs</span>
            <span>or</span>
            <span style={{
              padding:'4px 8px', border:'1px dashed var(--line)', borderRadius:5,
              color:'var(--ink2)',
            }}>spaniel demo --load checkout</span>
          </div>
        </div>
      </div>
    </Frame>
  );
}

// ─── TraceList — main inbox of recent traces ─────────────────────────────
function TraceList({ theme }) {
  const { TRACES } = window.SPANIEL;
  const maxDur = Math.max(...TRACES.map(t => t.dur));
  return (
    <Frame>
      <Chrome active="traces" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <Sidebar>
          <SbGroup title="sessions">
            <SbItem active dot="var(--accent)" count="34">feat/checkout</SbItem>
            <SbItem dot="var(--ink3)" count="11">main · baseline</SbItem>
            <SbItem dot="var(--warn)" count="2">scratch</SbItem>
          </SbGroup>
          <SbGroup title="services">
            <SbItem count="9">api-gateway</SbItem>
            <SbItem count="6">cart-service</SbItem>
            <SbItem count="6">pricing-service</SbItem>
            <SbItem count="4">inventory-service</SbItem>
            <SbItem count="3">payment-service</SbItem>
            <SbItem count="9">postgres</SbItem>
          </SbGroup>
          <SbGroup title="filters">
            <SbItem dot="var(--danger)">has lint warnings · 7</SbItem>
            <SbItem dot="var(--warn)">{`> 250ms · 4`}</SbItem>
            <SbItem dot="var(--accent)">errors · 1</SbItem>
          </SbGroup>
        </Sidebar>

        <div style={{ flex:1, display:'flex', flexDirection:'column', overflow:'hidden' }}>
          <PanelHead
            title="Recent traces"
            sub="live · last 5 minutes · 34 traces"
            right={
              <div style={{ display:'flex', gap:6 }}>
                <span style={{
                  padding:'5px 10px', borderRadius:6,
                  fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
                  background:'var(--surface2)', border:'1px solid var(--line)',
                }}>op:checkout dur:&gt;100ms</span>
                <span style={{
                  padding:'5px 10px', borderRadius:6,
                  fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
                  background:'var(--surface2)', border:'1px solid var(--line)',
                }}>⌘K</span>
              </div>
            }
          />
          <div style={{
            display:'grid',
            gridTemplateColumns:'minmax(0,1fr) 90px 80px 280px 70px',
            gap:0, padding:'8px 18px', borderBottom:'1px solid var(--line)',
            fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
            letterSpacing:'0.14em', color:'var(--ink3)', background:'var(--surface2)',
          }}>
            <div>operation</div><div style={{ textAlign:'right' }}>dur</div>
            <div style={{ textAlign:'right' }}>spans</div>
            <div>shape</div><div style={{ textAlign:'right' }}>ago</div>
          </div>

          <div style={{ flex:1, overflow:'hidden', background:'var(--surface)' }}>
            {TRACES.map((t, i) => (
              <div key={t.id} style={{
                display:'grid',
                gridTemplateColumns:'minmax(0,1fr) 90px 80px 280px 70px',
                alignItems:'center', padding:'10px 18px',
                borderBottom: i < TRACES.length-1 ? '1px solid var(--line2)' : 'none',
                background: i === 0 ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))' : 'transparent',
                borderLeft: i === 0 ? '2px solid var(--accent)' : '2px solid transparent',
              }}>
                <div style={{ minWidth:0 }}>
                  <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:3 }}>
                    <span style={{
                      fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)', fontWeight:600,
                      overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                    }}>{t.op}</span>
                    {t.tag === 'n+1' ? <Badge tone="danger">n+1</Badge> : null}
                    {t.tag === 'slow' ? <Badge tone="warn">slow</Badge> : null}
                    {t.tag === 'baseline' ? <Badge tone="accent">baseline</Badge> : null}
                    {t.status === 'error' ? <Badge tone="danger">error</Badge> : null}
                  </div>
                  <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>
                    {t.id}
                  </div>
                </div>
                <div style={{ fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
                  fontWeight:600, textAlign:'right' }}>{fmtMs(t.dur)}</div>
                <div style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
                  textAlign:'right' }}>{t.spans}</div>
                <div>
                  <DurBar dur={t.dur} max={maxDur} theme={theme} hot={t.tag === 'slow' || t.tag === 'n+1'} />
                </div>
                <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
                  textAlign:'right' }}>{t.t}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </Frame>
  );
}
function DurBar({ dur, max, theme, hot }) {
  return (
    <div style={{ position:'relative', height:8, background:'var(--surface2)', borderRadius:8 }}>
      <div style={{
        position:'absolute', left:0, top:0, bottom:0,
        width: (dur/max)*100 + '%',
        background: hot ? 'var(--danger)' : 'var(--accent)',
        borderRadius:8, opacity:0.8,
      }} />
    </div>
  );
}

// ─── ServiceMap — DAG of services ────────────────────────────────────────
function ServiceMap({ theme }) {
  const { SERVICES, EDGES } = window.SPANIEL;
  const W = 880, H = 320;
  const nodeIdx = Object.fromEntries(SERVICES.map(s => [s.id, s]));

  return (
    <Frame>
      <Chrome active="services" theme={theme} />
      <PanelHead title="Service map"
        sub="auto-generated from 34 traces · click an edge to filter"
        right={<Badge tone="accent">live</Badge>} />
      <div style={{ flex:1, position:'relative', background:`
        radial-gradient(circle at 20px 20px, var(--line) 1px, transparent 1px)`,
        backgroundSize:'24px 24px', overflow:'hidden' }}>
        <svg viewBox={`0 0 ${W} ${H}`} width="100%" height="100%" preserveAspectRatio="xMidYMid meet"
          style={{ display:'block' }}>
          {/* Edges */}
          {EDGES.map(([a,b,calls,ms], i) => {
            const A = nodeIdx[a], B = nodeIdx[b];
            if (!A || !B) return null;
            const x1 = A.x + 70, y1 = A.y + 24;
            const x2 = B.x,      y2 = B.y + 24;
            const mx = (x1+x2)/2;
            const hot = ms > 200;
            return (
              <g key={i}>
                <path d={`M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`}
                  fill="none" stroke={hot ? 'var(--danger)' : 'var(--ink3)'}
                  strokeWidth={Math.min(4, 0.8 + calls * 0.6)}
                  opacity={hot ? 0.85 : 0.55}
                  strokeDasharray={ms > 200 ? '0' : '0'} />
                <text x={(x1+x2)/2} y={(y1+y2)/2 - 4} textAnchor="middle"
                  fontFamily="var(--mono)" fontSize="9" fill="var(--ink2)">
                  {calls}× · {fmtMs(ms)}
                </text>
              </g>
            );
          })}
          {/* Nodes */}
          {SERVICES.map(s => {
            const c = (theme.svc && theme.svc[s.id]) || { fg:'var(--ink2)', bg:'var(--chipBg)' };
            return (
              <g key={s.id} transform={`translate(${s.x}, ${s.y})`}>
                <rect width="140" height="48" rx="10" ry="10"
                  fill={c.bg} stroke={c.fg} strokeOpacity="0.35" strokeWidth="1.5" />
                <circle cx="14" cy="24" r="5" fill={c.fg} opacity="0.8" />
                <text x="26" y="22" fontFamily="var(--mono)" fontSize="11"
                  fontWeight="600" fill={c.fg}>{s.id}</text>
                <text x="26" y="36" fontFamily="var(--mono)" fontSize="9"
                  fill={c.fg} opacity="0.7">{s.calls}× this trace</text>
              </g>
            );
          })}
        </svg>
      </div>
      <div style={{
        padding:'10px 16px', borderTop:'1px solid var(--line)',
        background:'var(--surface)',
        display:'flex', alignItems:'center', gap:14,
        fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
      }}>
        <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
          <span style={{ width:14, height:2, background:'var(--danger)' }} /> edge &gt; 200ms
        </span>
        <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
          <span style={{ width:14, height:2, background:'var(--ink3)', opacity:0.6 }} /> normal call
        </span>
        <span style={{ flex:1 }} />
        <span>8 services · 10 edges · 24 spans</span>
      </div>
    </Frame>
  );
}

// ─── SessionDiff — A/B trace comparison ──────────────────────────────────
function SessionDiff({ theme }) {
  const { SPANS, TRACE } = window.SPANIEL;
  // Simulate "after" by speeding up the N+1 cluster and slowing one call.
  const after = SPANS.map(s => {
    if (s.svc === 'postgres' && s.depth === 5)
      return { ...s, dur:1, name:'SELECT prices WHERE sku IN (?)' };
    if (s.id === 's18') return { ...s, dur: 340 };
    return s;
  }).filter(s => !['s10','s11','s12','s13'].includes(s.id));
  const afterDur = 472;
  const beforeDur = TRACE.durationMs;
  const maxDur = Math.max(beforeDur, afterDur);

  const renderColumn = (label, spans, dur, isBaseline) => (
    <div style={{ flex:1, display:'flex', flexDirection:'column', overflow:'hidden',
      background:'var(--surface)' }}>
      <div style={{
        padding:'10px 14px', borderBottom:'1px solid var(--line)',
        background: isBaseline
          ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))'
          : 'color-mix(in oklch, var(--ok) 18%, var(--surface))',
        display:'flex', alignItems:'center', gap:10,
      }}>
        <Badge tone={isBaseline ? 'accent' : 'ok'}>{isBaseline ? 'baseline' : 'after'}</Badge>
        <span style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)' }}>
          {label}
        </span>
        <div style={{ flex:1 }} />
        <span style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600, color:'var(--ink)' }}>
          {fmtMs(dur)}
        </span>
      </div>
      <div style={{ flex:1, overflow:'hidden' }}>
        {spans.map((s,i) => {
          const svc = (theme.svc && theme.svc[s.svc]) || { fg:'var(--ink2)', bg:'var(--chipBg)' };
          const left = (s.start/maxDur)*100, width = Math.max(0.6,(s.dur/maxDur)*100);
          return (
            <div key={s.id} style={{
              display:'grid', gridTemplateColumns:'94px minmax(0,1fr) 50px',
              gap:8, alignItems:'center', padding:'2px 8px',
              borderBottom:'1px solid var(--line2)',
              background: i === spans.length-1 ? 'transparent' : 'transparent',
            }}>
              <div style={{
                display:'flex', alignItems:'center', gap:5,
                paddingLeft: s.depth * 8,
                minWidth:0,
              }}>
                <span style={{ width:6, height:6, borderRadius:6,
                  background:svc.fg, opacity:0.85, flex:'0 0 auto' }} />
                <span style={{
                  fontFamily:'var(--mono)', fontSize:9.5, color:svc.fg,
                  fontWeight:600, letterSpacing:'-0.01em',
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                }}>{s.svc.replace(/-service$/,'')}</span>
              </div>
              <div style={{ position:'relative', height:14, marginLeft: s.depth*8 }}>
                <div style={{
                  position:'absolute', top:3, height:8,
                  left: left + '%', width: width + '%',
                  background: svc.bg, borderRadius:2,
                  boxShadow: `inset 2px 0 0 ${svc.fg}`,
                }} />
                <span style={{
                  position:'absolute', top:0, left: Math.min(left + width + 0.6, 70) + '%',
                  fontFamily:'var(--mono)', fontSize:9.5, color:'var(--ink2)',
                  whiteSpace:'nowrap', maxWidth:'100%',
                  overflow:'hidden', textOverflow:'ellipsis',
                }}>{s.name}</span>
              </div>
              <div style={{ textAlign:'right', fontFamily:'var(--mono)', fontSize:9.5,
                color:'var(--ink2)' }}>{fmtMs(s.dur)}</div>
            </div>
          );
        })}
      </div>
    </div>
  );

  return (
    <Frame>
      <Chrome active="sessions" theme={theme} />
      {/* Summary strip */}
      <div style={{
        padding:'14px 18px', borderBottom:'1px solid var(--line)',
        background:'var(--surface)',
        display:'flex', alignItems:'center', gap:18,
      }}>
        <div style={{ flex:1 }}>
          <div style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600,
            color:'var(--ink)' }}>POST /api/checkout</div>
          <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>
            baseline @ <span style={{ color:'var(--ink2)' }}>main · 88d1…5f24b</span>{' '}
            ↔ after @ <span style={{ color:'var(--ink2)' }}>feat/checkout · 9ef3…d19eb</span>
          </div>
        </div>
        <DiffStat label="total" before={beforeDur} after={afterDur} unit="ms" />
        <DiffStat label="spans" before={24} after={20} unit="" raw />
        <DiffStat label="db calls" before={9} after={5} unit="" raw />
        <DiffStat label="p95 span" before={287} after={340} unit="ms" />
      </div>

      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        {renderColumn('main · 88d1…5f24b', SPANS, beforeDur, true)}
        <div style={{ width:1, background:'var(--line)' }} />
        {renderColumn('feat/checkout · 9ef3…d19eb', after, afterDur, false)}
      </div>

      <div style={{
        padding:'10px 16px', borderTop:'1px solid var(--line)',
        background:'var(--surface2)',
        display:'flex', alignItems:'center', gap:18,
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
      }}>
        <span><strong style={{ color:'#3e6a3e' }}>−4</strong> spans removed (N+1 collapse)</span>
        <span><strong style={{ color:'var(--dangerInk)' }}>+53ms</strong> stripe.charge regressed</span>
        <span style={{ flex:1 }} />
        <span style={{ padding:'4px 8px', border:'1px solid var(--line)', borderRadius:5,
          background:'var(--surface)' }}>spaniel diff --baseline main</span>
      </div>
    </Frame>
  );
}
function DiffStat({ label, before, after, unit, raw }) {
  const delta = after - before;
  const better = delta < 0;
  return (
    <div style={{ minWidth:96 }}>
      <div style={{ fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
        letterSpacing:'0.14em', color:'var(--ink3)' }}>{label}</div>
      <div style={{ display:'flex', alignItems:'baseline', gap:6, marginTop:2 }}>
        <span style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600, color:'var(--ink)' }}>
          {raw ? after : fmtMs(after)}
        </span>
        <span style={{
          fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
          color: better ? '#3e6a3e' : 'var(--dangerInk)',
        }}>
          {delta > 0 ? '+' : ''}{raw ? delta : fmtMs(Math.abs(delta))}
        </span>
      </div>
    </div>
  );
}

// ─── LintWarnings — semconv linter + N+1 detector ────────────────────────
function LintWarnings({ theme }) {
  const { LINTS } = window.SPANIEL;
  const rows = [
    ...LINTS,
    { sev:'warn',  code:'SEMCONV.NET.PEER_NAME_MISSING', span:'rpc.payment.Charge',
      msg:'client span missing net.peer.name', fix:'set on the rpc client' },
    { sev:'info',  code:'COVERAGE.ROUTE_UNINSTRUMENTED', span:'GET /api/health',
      msg:'no spans observed for this route', fix:'wrap handler with otelhttp' },
    { sev:'warn',  code:'SEMCONV.DB.STATEMENT_MISSING', span:'UPDATE stock SET reserved=…',
      msg:'db span missing db.statement attribute', fix:'enable pg.tracer.captureStatement' },
  ];
  return (
    <Frame>
      <Chrome active="lints" theme={theme} />
      <PanelHead title="Lint warnings"
        sub="OTel semantic conventions · N+1 detector · coverage"
        right={
          <div style={{ display:'flex', gap:6 }}>
            <Badge tone="danger">1 error</Badge>
            <Badge tone="warn">4 warn</Badge>
            <Badge tone="neutral">1 info</Badge>
          </div>
        } />

      {/* Summary band */}
      <div style={{
        padding:'14px 18px', display:'flex', gap:18,
        background:'var(--surface)', borderBottom:'1px solid var(--line)',
      }}>
        <SummaryStat icon="●" tone="danger" big="1" label="missing required attr" sub="http.request.method" />
        <SummaryStat icon="⟳" tone="danger" big="1" label="N+1 detected"             sub="pricing.Quote · 6 reads" />
        <SummaryStat icon="◌" tone="warn"   big="3" label="semconv warnings"         sub="db.system, db.statement…" />
        <SummaryStat icon="◐" tone="neutral" big="78%" label="route coverage"        sub="1 route has 0 spans" />
      </div>

      <div style={{ flex:1, overflow:'hidden', background:'var(--surface)' }}>
        {rows.map((r, i) => {
          const tone = r.sev === 'error' ? 'danger' : r.sev === 'warn' ? 'warn' : 'neutral';
          const dotColor = r.sev === 'error' ? 'var(--danger)'
            : r.sev === 'warn' ? 'var(--warn)' : 'var(--ink3)';
          return (
            <div key={i} style={{
              padding:'12px 18px',
              borderBottom: i < rows.length-1 ? '1px solid var(--line2)' : 'none',
              display:'flex', gap:12, alignItems:'flex-start',
            }}>
              <span style={{
                width:9, height:9, borderRadius:9, marginTop:5,
                background: dotColor, flex:'0 0 auto',
                boxShadow: `0 0 0 3px color-mix(in oklch, ${dotColor} 22%, transparent)`,
              }} />
              <div style={{ flex:1, minWidth:0 }}>
                <div style={{ display:'flex', alignItems:'baseline', gap:10, marginBottom:3 }}>
                  <span style={{ fontFamily:'var(--mono)', fontSize:11, fontWeight:700, color:'var(--ink)' }}>
                    {r.code}
                  </span>
                  <Badge tone={tone}>{r.sev}</Badge>
                  <span style={{ flex:1 }} />
                  <span style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>
                    span: <span style={{ color:'var(--ink2)' }}>{r.span}</span>
                  </span>
                </div>
                <div style={{ fontSize:12.5, color:'var(--ink)', lineHeight:1.45 }}>{r.msg}</div>
                <div style={{
                  fontFamily:'var(--mono)', fontSize:11, color:'var(--accentInk)',
                  background:'var(--surface2)', borderRadius:5,
                  padding:'4px 8px', marginTop:6, display:'inline-block',
                }}>
                  <span style={{ color:'var(--ink3)' }}>fix → </span>{r.fix}
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </Frame>
  );
}
function SummaryStat({ tone, big, label, sub }) {
  const tones = {
    danger:  'var(--danger)',
    warn:    'var(--warn)',
    neutral: 'var(--ink3)',
  };
  return (
    <div style={{
      flex:1, padding:'10px 12px',
      border:'1px solid var(--line)', borderRadius:8,
      background:'var(--surface2)',
    }}>
      <div style={{ display:'flex', alignItems:'baseline', gap:8 }}>
        <span style={{ width:8, height:8, borderRadius:8, background:tones[tone],
          alignSelf:'center' }} />
        <span style={{ fontFamily:'var(--serif)', fontSize:22, fontWeight:600, color:'var(--ink)' }}>
          {big}
        </span>
      </div>
      <div style={{ fontFamily:'var(--sans)', fontSize:11, color:'var(--ink2)', marginTop:4 }}>
        {label}
      </div>
      <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', marginTop:2 }}>
        {sub}
      </div>
    </div>
  );
}

// ─── CommandPalette (⌘K) ─────────────────────────────────────────────────
function CommandPalette({ theme }) {
  const groups = [
    { label:'traces', items:[
      { ic:'⟶', t:'POST /api/checkout · 612ms', sub:'9ef3…d19eb · n+1', shortcut:'↵' },
      { ic:'⟶', t:'POST /api/checkout · 498ms', sub:'88d1…5f24b · baseline' },
      { ic:'⟶', t:'POST /api/webhook · 1.84s',  sub:'c041…77b3d · error' },
    ]},
    { label:'spans', items:[
      { ic:'⌗', t:'SELECT price WHERE sku=?', sub:'pricing-service · 6 occurrences' },
      { ic:'⌗', t:'http.stripe POST /v1/charges', sub:'payment-service · slow' },
    ]},
    { label:'actions', items:[
      { ic:'▶', t:'Mark current session as baseline', sub:'spaniel session baseline', shortcut:'⌘B' },
      { ic:'▶', t:'Diff against main',                sub:'spaniel diff --baseline main', shortcut:'⌘D' },
      { ic:'▶', t:'Open service map',                  sub:'⌘2' },
    ]},
  ];
  return (
    <Frame style={{ padding:'56px 36px', background:'var(--bg)', justifyContent:'flex-start' }}>
      <div style={{ flex:1, display:'flex', alignItems:'flex-start', justifyContent:'center' }}>
        <div style={{
          width:'100%', maxWidth:500,
          background:'var(--surface)',
          border:'1px solid var(--line)', borderRadius:14,
          boxShadow:'var(--shadow)',
          overflow:'hidden',
        }}>
          <div style={{
            padding:'14px 16px', borderBottom:'1px solid var(--line)',
            display:'flex', alignItems:'center', gap:10,
            background:'var(--surface2)',
          }}>
            <span style={{ fontFamily:'var(--mono)', fontSize:14, color:'var(--ink3)' }}>⌘K</span>
            <input style={{
              flex:1, border:'none', background:'transparent', outline:'none',
              fontFamily:'var(--mono)', fontSize:14, color:'var(--ink)',
            }} defaultValue="checkout" />
            <span style={{
              padding:'2px 7px', borderRadius:4, fontFamily:'var(--mono)', fontSize:10,
              color:'var(--ink3)', border:'1px solid var(--line)',
            }}>esc</span>
          </div>
          <div style={{ maxHeight:380, overflow:'hidden' }}>
            {groups.map(g => (
              <div key={g.label}>
                <div style={{
                  padding:'8px 16px 4px', fontFamily:'var(--mono)', fontSize:9,
                  textTransform:'uppercase', letterSpacing:'0.14em', color:'var(--ink3)',
                }}>{g.label}</div>
                {g.items.map((it, i) => {
                  const isFirst = g.label === 'traces' && i === 0;
                  return (
                    <div key={i} style={{
                      padding:'8px 16px', display:'flex', alignItems:'center', gap:12,
                      background: isFirst ? 'color-mix(in oklch, var(--accent) 14%, var(--surface))' : 'transparent',
                      borderLeft: isFirst ? '2px solid var(--accent)' : '2px solid transparent',
                    }}>
                      <span style={{
                        width:22, height:22, borderRadius:6,
                        display:'inline-flex', alignItems:'center', justifyContent:'center',
                        background:'var(--surface2)', color:'var(--ink2)',
                        fontFamily:'var(--mono)', fontSize:11,
                      }}>{it.ic}</span>
                      <div style={{ flex:1, minWidth:0 }}>
                        <div style={{ fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
                          overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>
                          {it.t}
                        </div>
                        <div style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)' }}>{it.sub}</div>
                      </div>
                      {it.shortcut ? <span style={{
                        padding:'2px 6px', borderRadius:4, fontFamily:'var(--mono)', fontSize:10,
                        color:'var(--ink3)', border:'1px solid var(--line)', background:'var(--surface2)',
                      }}>{it.shortcut}</span> : null}
                    </div>
                  );
                })}
              </div>
            ))}
          </div>
          <div style={{
            padding:'8px 14px', borderTop:'1px solid var(--line)',
            background:'var(--surface2)',
            display:'flex', gap:14,
            fontFamily:'var(--mono)', fontSize:9.5, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.12em',
          }}>
            <span>↑↓ navigate</span>
            <span>↵ open</span>
            <span>⌘↵ open in split</span>
            <span style={{ flex:1 }} />
            <span>14 results</span>
          </div>
        </div>
      </div>
    </Frame>
  );
}

window.EmptyState = EmptyState;
window.TraceList = TraceList;
window.ServiceMap = ServiceMap;
window.SessionDiff = SessionDiff;
window.LintWarnings = LintWarnings;
window.CommandPalette = CommandPalette;
