// Spaniel — Field direction.  Terminal printout aesthetic.  Everything in
// monospace, chrome drawn with ASCII box characters, bars rendered as block
// characters or sharp rectangles.  No drop shadows, no rounded corners.

const { fmtMs } = window.UI;
const F = {};

// ── Helpers ──────────────────────────────────────────────────────────────
function pad(s, n) { return (s + ' '.repeat(n)).slice(0, n); }
function blocks(width) {
  // Render a duration bar using the unicode 1/8 block series.
  const full = Math.floor(width);
  const frac = width - full;
  const eighths = '▏▎▍▌▋▊▉█';
  let s = '█'.repeat(full);
  if (frac > 0.05) s += eighths[Math.min(7, Math.floor(frac * 8))];
  return s;
}

F.SvcTag = function ({ svc, theme }) {
  const c = theme.svc[svc] || { fg:'var(--ink2)' };
  return (
    <span style={{
      fontFamily:'var(--mono)', fontSize:10, color:c.fg,
      whiteSpace:'nowrap',
    }}>[{svc}]</span>
  );
};

F.Bracket = function ({ tone = 'neutral', children }) {
  const tones = {
    neutral: 'var(--ink2)', accent:'var(--accent)', ok:'var(--ok)',
    warn:'var(--warnInk)', danger:'var(--danger)',
  };
  return (
    <span style={{
      fontFamily:'var(--mono)', fontSize:10, fontWeight:700,
      color:tones[tone], letterSpacing:'0.04em',
    }}>[{children}]</span>
  );
};

// Border-line built out of repeated dash + label inserts.
F.Rule = function ({ left, right, color = 'var(--line)' }) {
  return (
    <div style={{
      display:'flex', alignItems:'center', gap:6,
      fontFamily:'var(--mono)', fontSize:11, color,
      whiteSpace:'nowrap', overflow:'hidden',
    }}>
      <span>{'┌─'}</span>
      {left ? <span style={{ color:'var(--ink)' }}>{left}</span> : null}
      <span style={{ flex:1, borderBottom:`1px solid ${color}`, height:0, alignSelf:'center', marginTop:1 }} />
      {right ? <span style={{ color:'var(--ink2)' }}>{right}</span> : null}
      <span>{'─┐'}</span>
    </div>
  );
};

F.Chrome = function ({ active = 'traces' }) {
  const navs = ['traces','services','lints','sessions','settings'];
  return (
    <div style={{
      padding:'10px 16px',
      borderBottom:'1px dashed var(--line)',
      background:'var(--surface)',
      display:'flex', alignItems:'center', gap:12,
      fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
    }}>
      {/* Mark: ◉ spaniel v0.4 */}
      <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
        <span style={{
          width:14, height:14, borderRadius:14,
          background:'var(--accent)', display:'inline-block',
        }} />
        <strong style={{ fontWeight:700, letterSpacing:'-0.01em' }}>spaniel</strong>
        <span style={{ color:'var(--ink3)' }}>v0.4</span>
      </span>
      <span style={{ color:'var(--ink3)' }}>│</span>
      <nav style={{ display:'flex', gap:14 }}>
        {navs.map(n => (
          <span key={n} style={{
            color: n === active ? 'var(--accentInk)' : 'var(--ink3)',
            fontWeight: n === active ? 700 : 500,
          }}>{n === active ? '> ' : '  '}{n}</span>
        ))}
      </nav>
      <span style={{ flex:1 }} />
      <span style={{ color:'var(--ok)' }}>●</span>
      <span style={{ color:'var(--ink2)' }}>otlp 4317/4318</span>
      <span style={{ color:'var(--ink3)' }}>│</span>
      <span style={{ color:'var(--ink2)' }}>session=<span style={{ color:'var(--ink)', fontWeight:700 }}>feat/checkout</span></span>
      <span style={{ color:'var(--ink3)' }}>│</span>
      <span style={{ color:'var(--ink2)' }}>zf@local</span>
    </div>
  );
};

F.Frame = function ({ children }) {
  return (
    <div style={{
      width:'100%', height:'100%', background:'var(--bg)',
      display:'flex', flexDirection:'column',
      fontFamily:'var(--mono)', color:'var(--ink)',
      backgroundImage:`repeating-linear-gradient(
        0deg, transparent 0, transparent 23px,
        rgba(0,0,0,0.025) 23px, rgba(0,0,0,0.025) 24px)`,
      overflow:'hidden',
    }}>{children}</div>
  );
};

// ── System card ──────────────────────────────────────────────────────────
F.SystemCard = function ({ theme }) {
  return (
    <div style={{
      background:'var(--bg)', width:'100%', height:'100%',
      padding:'20px 22px',
      fontFamily:'var(--mono)', color:'var(--ink)',
      display:'flex', flexDirection:'column', gap:16,
      backgroundImage:`repeating-linear-gradient(
        0deg, transparent 0, transparent 23px,
        rgba(0,0,0,0.025) 23px, rgba(0,0,0,0.025) 24px)`,
    }}>
      <div style={{ display:'flex', alignItems:'baseline', gap:10 }}>
        <span style={{ fontSize:11, color:'var(--ink3)' }}># ./spaniel/ui</span>
        <span style={{ flex:1 }} />
        <span style={{ fontSize:24, fontWeight:700, letterSpacing:'-0.02em' }}>FIELD</span>
      </div>
      <div style={{ fontSize:11, color:'var(--ink2)', lineHeight:1.5 }}>
        Terminal printout.  Monospace everywhere, ASCII box chars for chrome,<br/>
        block characters for span bars.  One accent (forest green), one danger<br/>
        (rust).  For developers who live in <span style={{ color:'var(--ink)', fontWeight:700 }}>tmux</span>.
      </div>

      {/* swatch row */}
      <div>
        <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:5 }}>┌─ palette</div>
        <pre style={{ margin:0, fontSize:11, color:'var(--ink2)', lineHeight:1.5 }}>
{`bg        surface   surface2  ink       accent    warn      danger`}
        </pre>
        <div style={{ display:'flex', gap:0 }}>
          {['bg','surface','surface2','ink','accent','warn','danger'].map(t => (
            <div key={t} style={{
              flex:1, height:28, background:`var(--${t})`,
              borderRight:'1px solid rgba(0,0,0,0.08)',
            }} />
          ))}
        </div>
      </div>

      {/* type ramp */}
      <div>
        <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:5 }}>┌─ type · one family, sized</div>
        <div style={{ background:'var(--surface)', border:'1px solid var(--line)',
          padding:'12px 14px', display:'flex', flexDirection:'column', gap:4 }}>
          <div style={{ fontSize:22, fontWeight:700, letterSpacing:'-0.02em' }}>postman, but for traces</div>
          <div style={{ fontSize:13, color:'var(--ink2)' }}>612ms p95 · 24 spans across 6 services</div>
          <div style={{ fontSize:11, color:'var(--ink3)' }}>SELECT price WHERE sku = $1</div>
        </div>
      </div>

      {/* span bar examples */}
      <div>
        <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:5 }}>┌─ span bar · unicode blocks</div>
        <pre style={{ margin:0, fontSize:11, lineHeight:1.55,
          background:'var(--surface)', border:'1px solid var(--line)',
          padding:'10px 12px', whiteSpace:'pre' }}>
{`  POST /api/checkout       `}<span style={{ color:theme.svc['api-gateway'].fg }}>{blocks(48)}</span>{`   612ms\n`}
{`  ├─ rpc.cart.GetCart      `}<span style={{ color:theme.svc['cart-service'].fg }}>{blocks(9.6)}</span>{`                                     124ms\n`}
{`  │  └─ SELECT * FROM carts`}<span style={{ color:theme.svc['postgres'].fg }}> {blocks(1)}</span>{`                                             12ms\n`}
{`  └─ rpc.payment.Charge    `}<span style={{ color:theme.svc['payment-service'].fg }}>{'                      ' + blocks(24)}</span>{`            `}<span style={{ color:'var(--danger)' }}>[SLOW]</span>{` 312ms`}
        </pre>
      </div>

      {/* status brackets */}
      <div>
        <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:5 }}>┌─ status</div>
        <div style={{ display:'flex', gap:14, fontSize:12 }}>
          <span style={{ color:'var(--ok)', fontWeight:700 }}>[OK]</span>
          <span style={{ color:'var(--warnInk)', fontWeight:700 }}>[SLOW]</span>
          <span style={{ color:'var(--danger)', fontWeight:700 }}>[N+1]</span>
          <span style={{ color:'var(--danger)', fontWeight:700 }}>[ERR]</span>
          <span style={{ color:'var(--accentInk)', fontWeight:700 }}>[BASELINE]</span>
          <span style={{ color:'var(--ink3)', fontWeight:700 }}>[INTERNAL]</span>
        </div>
      </div>

      <div style={{ flex:1 }} />
      <pre style={{ margin:0, fontSize:10, color:'var(--ink3)', lineHeight:1.5 }}>
{`└─ font: JetBrains Mono · everything
   no drop-shadows · no rounded corners · 24px baseline grid`}
      </pre>
    </div>
  );
};

// ── Trace detail (hero) ──────────────────────────────────────────────────
F.TraceDetail = function ({ theme }) {
  const { TRACE, SPANS } = window.SPANIEL;
  const D = TRACE.durationMs;
  const selectedId = 's12';
  const sel = SPANS.find(s => s.id === selectedId);

  // Build the ascii indent prefix for each span
  function indentChars(depth) {
    if (depth === 0) return '';
    const parts = [];
    for (let i = 0; i < depth - 1; i++) parts.push('│ ');
    parts.push('├─');
    return parts.join('');
  }

  return (
    <F.Frame>
      <F.Chrome active="traces" />

      {/* Trace meta header — box-drawn */}
      <div style={{ padding:'14px 18px 6px', background:'var(--surface)', borderBottom:'1px dashed var(--line)' }}>
        <pre style={{ margin:0, fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)', lineHeight:1.45 }}>
{`┌──[ POST /api/checkout ]──[ trace `}<span style={{ color:'var(--ink2)' }}>9ef357237e8c…d19eb</span>{` ]──[ 14:22:08.421 ]──┐\n`}
{`│                                                                                       │\n`}
{`│   `}<span style={{ fontSize:22, fontWeight:700, letterSpacing:'-0.02em' }}>612ms</span>{`    total       `}<span style={{ fontSize:22, fontWeight:700 }}>24</span>{`  spans       `}<span style={{ fontSize:22, fontWeight:700 }}>6</span>{`  services       `}<span style={{ color:'var(--ok)', fontWeight:700 }}>[OK]</span>{`  `}<span style={{ color:'var(--danger)', fontWeight:700 }}>[3 LINT]</span>{`     │\n`}
{`│                                                                                       │\n`}
{`└───────────────────────────────────────────────────────────────────────────────────────┘`}
        </pre>
      </div>

      {/* Ruler */}
      <div style={{
        display:'grid', gridTemplateColumns:'320px 1fr 60px',
        padding:'8px 14px', borderBottom:'1px solid var(--line)',
        background:'var(--surface2)',
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
      }}>
        <div>span · 24 total</div>
        <div style={{ position:'relative', height:14 }}>
          {[0,1,2,3,4,5,6].map(i => (
            <span key={i} style={{
              position:'absolute', left: (i/6)*100 + '%',
              fontSize:9, color:'var(--ink3)',
              transform:'translateX(-50%)',
            }}>{fmtMs((D/6)*i)}</span>
          ))}
        </div>
        <div style={{ textAlign:'right' }}>dur</div>
      </div>

      {/* Waterfall */}
      <div style={{ flex:1, overflow:'hidden', background:'var(--surface)' }}>
        {SPANS.map(s => {
          const svc = theme.svc[s.svc] || {};
          const left  = (s.start / D) * 100;
          const width = Math.max(0.4, (s.dur / D) * 100);
          const isSel = s.id === selectedId;
          return (
            <div key={s.id} style={{
              display:'grid', gridTemplateColumns:'320px 1fr 60px',
              padding:'2px 14px',
              background: isSel ? 'color-mix(in oklch, var(--accent) 12%, var(--surface))' : 'transparent',
              borderLeft: isSel ? '2px solid var(--accent)' : '2px solid transparent',
              alignItems:'center',
              borderBottom:'1px dotted var(--line2)',
            }}>
              <div style={{ display:'flex', alignItems:'center', gap:6, minWidth:0,
                fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)' }}>
                <span style={{ color:'var(--ink3)' }}>{indentChars(s.depth)}</span>
                <F.SvcTag svc={s.svc} theme={theme} />
                <span style={{
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap', flex:1, minWidth:0,
                }}>{s.name}</span>
                {s.tag ? <F.Bracket tone={s.tag === 'n+1' || s.tag === 'slow' ? 'danger' : 'warn'}>
                  {s.tag.toUpperCase()}</F.Bracket> : null}
              </div>
              <div style={{ position:'relative', height:14, marginLeft:8, marginRight:8 }}>
                {/* dotted ruler */}
                <div style={{ position:'absolute', inset:0, display:'flex' }}>
                  {[0,1,2,3,4,5].map(i => <div key={i} style={{
                    flex:1, borderLeft: i === 0 ? 'none' : '1px dotted var(--line)',
                  }} />)}
                </div>
                <div style={{
                  position:'absolute', top:3, height:8,
                  left: left + '%', width: width + '%',
                  background: svc.fg, opacity:0.85,
                }} />
                {(s.tag === 'n+1' || s.tag === 'slow') ? <div style={{
                  position:'absolute', top:1, height:12,
                  left: left + '%', width: width + '%',
                  border:`1px solid var(--danger)`, pointerEvents:'none',
                }} /> : null}
              </div>
              <div style={{
                textAlign:'right', fontFamily:'var(--mono)', fontSize:10,
                color:'var(--ink2)',
              }}>{fmtMs(s.dur)}</div>
            </div>
          );
        })}
      </div>

      {/* Inspector strip BELOW the waterfall — terminal-style detail pane */}
      <div style={{
        borderTop:'1px dashed var(--line)',
        background:'var(--surface)',
        padding:'12px 18px',
        display:'grid', gridTemplateColumns:'1fr 1fr 1fr',
        gap:18, alignItems:'flex-start',
      }}>
        <div>
          <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:6 }}>┌─ inspect span ──────────────</div>
          <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:6 }}>
            <F.SvcTag svc={sel.svc} theme={theme} />
            <span style={{ color:'var(--ink3)', fontSize:10 }}>·</span>
            <span style={{ fontSize:10, color:'var(--ink3)' }}>client · +{sel.start}ms · 8ms</span>
          </div>
          <div style={{ fontSize:13, color:'var(--ink)', fontWeight:700, lineHeight:1.3, marginBottom:8 }}>
            SELECT price WHERE sku=?
          </div>
          <pre style={{ margin:0, fontSize:10.5, color:'var(--ink2)', lineHeight:1.6 }}>
{`db.system        postgresql
db.statement     SELECT price WHERE sku = $1
db.name          pricing
net.peer.name    pg-primary.local
net.peer.port    5432
otel.library     spaniel/sdk-go 0.4.2`}
          </pre>
        </div>

        <div>
          <div style={{ fontSize:10, color:'var(--ink3)', marginBottom:6 }}>┌─ events ────────────────────</div>
          <pre style={{ margin:0, fontSize:10.5, color:'var(--ink2)', lineHeight:1.6 }}>
{`+0.4ms  pg.connection.acquired
+6.1ms  pg.row.fetched rows=1
+7.8ms  pg.connection.released`}
          </pre>
          <div style={{ fontSize:10, color:'var(--ink3)', margin:'14px 0 6px' }}>┌─ correlated logs ──────────</div>
          <pre style={{ margin:0, fontSize:10.5, color:'var(--ink2)', lineHeight:1.6 }}>
{`INFO  pricing.quote sku=A234 hit=miss
INFO  pricing.quote sku=B118 hit=miss
INFO  pricing.quote sku=C901 hit=miss`}
          </pre>
        </div>

        <div>
          <div style={{ fontSize:10, color:'var(--danger)', marginBottom:6, fontWeight:700 }}>
            ┌─ ! N+1 DETECTED ────────────
          </div>
          <pre style={{ margin:0, fontSize:11, color:'var(--ink)', lineHeight:1.5,
            background:'color-mix(in oklch, var(--danger) 14%, var(--surface))',
            padding:'10px 12px', border:'1px solid var(--danger)' }}>
{`5 sibling spans share fingerprint
  SELECT price WHERE sku=?
within 50ms.

fix: batch with
  SELECT price WHERE sku IN (?)

est. savings ≈ 32ms`}
          </pre>
          <div style={{ marginTop:10, fontSize:10.5, color:'var(--ink2)' }}>
            <span style={{ color:'var(--ink3)' }}>→ </span>
            <span style={{ color:'var(--accentInk)', fontWeight:700 }}>spaniel pin s12 --as=n+1-fix</span>
          </div>
        </div>
      </div>
    </F.Frame>
  );
};

// ── Trace list ───────────────────────────────────────────────────────────
F.TraceList = function ({ theme }) {
  const { TRACES } = window.SPANIEL;
  const maxDur = Math.max(...TRACES.map(t => t.dur));
  return (
    <F.Frame>
      <F.Chrome active="traces" />
      {/* Filter / query line */}
      <div style={{
        padding:'10px 16px', borderBottom:'1px dashed var(--line)',
        background:'var(--surface)',
        display:'flex', alignItems:'center', gap:10,
        fontFamily:'var(--mono)', fontSize:12,
      }}>
        <span style={{ color:'var(--accent)', fontWeight:700 }}>$</span>
        <span style={{ color:'var(--ink2)' }}>spaniel ls --session=feat/checkout</span>
        <span style={{ color:'var(--ink3)' }}>--filter</span>
        <span style={{ color:'var(--ink)', background:'var(--surface2)', padding:'1px 6px' }}>
          op:checkout dur:&gt;100ms
        </span>
        <span style={{ flex:1 }} />
        <span style={{ color:'var(--ink3)' }}>live · 34 results</span>
      </div>
      {/* Column header */}
      <div style={{
        display:'grid',
        gridTemplateColumns:'80px minmax(0,1fr) 280px 70px 70px 70px',
        padding:'6px 16px', borderBottom:'1px solid var(--line)',
        background:'var(--surface2)',
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
      }}>
        <div>STATUS</div><div>OPERATION · TRACE_ID</div><div>SHAPE</div>
        <div style={{ textAlign:'right' }}>DUR</div>
        <div style={{ textAlign:'right' }}>SPANS</div>
        <div style={{ textAlign:'right' }}>AGO</div>
      </div>
      {/* Rows */}
      <div style={{ flex:1, overflow:'hidden', background:'var(--surface)' }}>
        {TRACES.map((t, i) => {
          const isSel = i === 0;
          return (
            <div key={t.id} style={{
              display:'grid',
              gridTemplateColumns:'80px minmax(0,1fr) 280px 70px 70px 70px',
              padding:'8px 16px', alignItems:'center',
              borderBottom:'1px dotted var(--line2)',
              background: isSel ? 'color-mix(in oklch, var(--accent) 10%, var(--surface))' : 'transparent',
              borderLeft: isSel ? '2px solid var(--accent)' : '2px solid transparent',
              fontFamily:'var(--mono)', fontSize:11.5, color:'var(--ink)',
            }}>
              <div>
                {t.status === 'error' ? <F.Bracket tone="danger">ERR</F.Bracket>
                  : t.tag === 'n+1' ? <F.Bracket tone="danger">N+1</F.Bracket>
                  : t.tag === 'slow' ? <F.Bracket tone="warn">SLOW</F.Bracket>
                  : t.tag === 'baseline' ? <F.Bracket tone="accent">BASE</F.Bracket>
                  : <F.Bracket tone="ok">OK</F.Bracket>}
              </div>
              <div style={{ minWidth:0, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>
                <span style={{ fontWeight:700 }}>{t.op}</span>
                <span style={{ color:'var(--ink3)' }}> · {t.id}</span>
              </div>
              <div style={{ position:'relative', height:8, background:'var(--surface2)' }}>
                <div style={{
                  position:'absolute', left:0, top:0, bottom:0,
                  width:(t.dur/maxDur)*100 + '%',
                  background: t.tag === 'slow' || t.tag === 'n+1' || t.status === 'error'
                    ? 'var(--danger)' : 'var(--accent)',
                  opacity:0.85,
                }} />
              </div>
              <div style={{ textAlign:'right' }}>{fmtMs(t.dur)}</div>
              <div style={{ textAlign:'right', color:'var(--ink2)' }}>{t.spans}</div>
              <div style={{ textAlign:'right', color:'var(--ink3)' }}>{t.t}</div>
            </div>
          );
        })}
      </div>
      <div style={{
        padding:'8px 16px', background:'var(--surface2)', borderTop:'1px solid var(--line)',
        fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)',
        display:'flex', gap:16,
      }}>
        <span>↑↓ select</span><span>↵ open</span><span>⌘K search</span><span>/ filter</span>
        <span style={{ flex:1 }} />
        <span>7 / 34</span>
      </div>
    </F.Frame>
  );
};

// ── Lint warnings ────────────────────────────────────────────────────────
F.LintWarnings = function ({ theme }) {
  const { LINTS } = window.SPANIEL;
  const rows = [
    ...LINTS,
    { sev:'warn', code:'SEMCONV.NET.PEER_NAME_MISSING', span:'rpc.payment.Charge',
      msg:'client span missing net.peer.name', fix:'set on the rpc client' },
    { sev:'info', code:'COVERAGE.ROUTE_UNINSTRUMENTED', span:'GET /api/health',
      msg:'no spans observed for this route', fix:'wrap handler with otelhttp' },
    { sev:'warn', code:'SEMCONV.DB.STATEMENT_MISSING', span:'UPDATE stock SET reserved=…',
      msg:'db span missing db.statement attribute', fix:'enable pg.tracer.captureStatement' },
  ];
  return (
    <F.Frame>
      <F.Chrome active="lints" />
      <div style={{ padding:'14px 18px', borderBottom:'1px dashed var(--line)', background:'var(--surface)' }}>
        <pre style={{ margin:0, fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)', lineHeight:1.45 }}>
{`┌──[ spaniel lint ]──[ session feat/checkout ]──────────────┐\n`}
{`│  `}<span style={{ color:'var(--danger)', fontWeight:700 }}>● 1</span>{` ERROR     `}<span style={{ color:'var(--warnInk)', fontWeight:700 }}>● 4</span>{` WARN     `}<span style={{ color:'var(--ink3)', fontWeight:700 }}>● 1</span>{` INFO     78% coverage    │\n`}
{`└──────────────────────────────────────────────────────────┘`}
        </pre>
      </div>
      <div style={{ flex:1, overflow:'hidden', background:'var(--surface)' }}>
        {rows.map((r, i) => {
          const color = r.sev === 'error' ? 'var(--danger)'
            : r.sev === 'warn' ? 'var(--warnInk)' : 'var(--ink3)';
          return (
            <div key={i} style={{
              padding:'10px 18px', borderBottom:'1px dotted var(--line2)',
              fontFamily:'var(--mono)', fontSize:11.5,
            }}>
              <div style={{ display:'flex', alignItems:'center', gap:10, color:'var(--ink)' }}>
                <span style={{ color, fontWeight:700 }}>
                  {r.sev === 'error' ? '✕' : r.sev === 'warn' ? '!' : 'i'}
                </span>
                <span style={{ fontWeight:700 }}>{r.code}</span>
                <span style={{ color:'var(--ink3)' }}>· span={r.span}</span>
              </div>
              <div style={{ paddingLeft:22, color:'var(--ink2)', marginTop:3, lineHeight:1.45 }}>
                {r.msg}
              </div>
              <div style={{ paddingLeft:22, marginTop:5,
                fontSize:11, color:'var(--accent)', fontWeight:700 }}>
                → {r.fix}
              </div>
            </div>
          );
        })}
      </div>
    </F.Frame>
  );
};

// ── Command palette — Field's signature ──────────────────────────────────
F.CommandPalette = function ({ theme }) {
  return (
    <F.Frame>
      <F.Chrome active="traces" />
      <div style={{ flex:1, padding:'40px 36px', display:'flex',
        justifyContent:'center', alignItems:'flex-start' }}>
        <div style={{
          width:'100%', maxWidth:520, background:'var(--surface)',
          border:'1px solid var(--ink)',
          fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
        }}>
          {/* Title bar */}
          <div style={{
            padding:'7px 12px', background:'var(--ink)', color:'var(--bg)',
            display:'flex', alignItems:'center', gap:10, fontSize:11,
          }}>
            <span>● ● ●</span>
            <span style={{ flex:1, textAlign:'center', fontWeight:700, letterSpacing:'0.04em' }}>
              spaniel · find
            </span>
            <span style={{ color:'var(--ink3)' }}>esc</span>
          </div>
          {/* Prompt */}
          <div style={{
            padding:'12px 14px', borderBottom:'1px dashed var(--line)',
            display:'flex', alignItems:'center', gap:8,
          }}>
            <span style={{ color:'var(--accent)', fontWeight:700 }}>›</span>
            <span>checkout</span>
            <span style={{ width:8, height:14, background:'var(--ink)',
              animation:'none', display:'inline-block', marginLeft:1 }} />
            <span style={{ flex:1 }} />
            <span style={{ color:'var(--ink3)', fontSize:10 }}>14 results</span>
          </div>
          {/* Results */}
          {[
            { hdr:'-- traces --' },
            { ic:'T', t:'POST /api/checkout · 612ms', sub:'9ef3…d19eb · [N+1]', sel:true },
            { ic:'T', t:'POST /api/checkout · 498ms', sub:'88d1…5f24b · [BASELINE]' },
            { ic:'T', t:'POST /api/webhook · 1.84s',  sub:'c041…77b3d · [ERR]' },
            { hdr:'-- spans --' },
            { ic:'S', t:'SELECT price WHERE sku=?',           sub:'pricing-service · 6 occurrences' },
            { ic:'S', t:'http.stripe POST /v1/charges',       sub:'payment-service · 287ms' },
            { hdr:'-- actions --' },
            { ic:'!', t:'mark session as baseline',  sub:'spaniel session baseline', sc:'⌘B' },
            { ic:'!', t:'diff against main',         sub:'spaniel diff --baseline main', sc:'⌘D' },
            { ic:'!', t:'open service map',          sub:':services', sc:'⌘2' },
          ].map((it, i) => {
            if (it.hdr) return (
              <div key={i} style={{
                padding:'6px 14px 2px', color:'var(--ink3)', fontSize:10,
                letterSpacing:'0.06em',
              }}>{it.hdr}</div>
            );
            return (
              <div key={i} style={{
                padding:'6px 14px',
                display:'flex', alignItems:'center', gap:10,
                background: it.sel ? 'color-mix(in oklch, var(--accent) 14%, var(--surface))' : 'transparent',
                borderLeft: it.sel ? '2px solid var(--accent)' : '2px solid transparent',
              }}>
                <span style={{ color:'var(--accent)', fontWeight:700, width:14 }}>{it.ic}</span>
                <div style={{ flex:1, minWidth:0 }}>
                  <div style={{ overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{it.t}</div>
                  <div style={{ color:'var(--ink3)', fontSize:10 }}>{it.sub}</div>
                </div>
                {it.sc ? <span style={{ color:'var(--ink3)', fontSize:10 }}>{it.sc}</span> : null}
              </div>
            );
          })}
          {/* Footer */}
          <div style={{
            padding:'7px 14px', borderTop:'1px dashed var(--line)',
            color:'var(--ink3)', fontSize:10,
            display:'flex', gap:14,
          }}>
            <span>↑↓ nav</span><span>↵ open</span><span>⌘↵ split</span>
            <span style={{ flex:1 }} />
            <span>spaniel://find</span>
          </div>
        </div>
      </div>
    </F.Frame>
  );
};

window.Field = F;
