// Spaniel — Coverage page (Drift direction).  "What percentage of my API
// surface area has any tracing at all?"  Istanbul/nyc for OTel.
//
// Dataset is shaped to mirror the issue's example: per-service observed vs.
// total, with explicit dark routes.  The page has three modes:
//   · summary   — per-service coverage bars + overall %
//   · routes    — heatmap grid of every route, click-to-inspect
//   · dark only — flattened list of dark routes across all services
// An OpenAPI import banner sits on top so devs see how to raise their %.

const { useState: covState, useMemo: covMemo } = React;
const { Chrome, Sidebar, SbGroup, SbItem, SvcChip, Badge, Frame } = window.UI;

// ── Dataset ──────────────────────────────────────────────────────────────
//
// Each service has a list of route entries: { method, path, hits, p95Ms }.
// hits = 0 means dark.  total = observed + dark.
// "source" = how spaniel learned about the route ('openapi' from imported
// spec, 'observed' from spans only, 'manual' from --routes-file).
const COV_DATA = [
  { svc:'api-gateway',  source:'openapi', spec:'openapi.yaml', kind:'http',
    routes:[
      { m:'POST', p:'/api/checkout',        h:34,  p95:612 },
      { m:'POST', p:'/api/login',           h:18,  p95:142 },
      { m:'GET',  p:'/api/products',        h:241, p95:78  },
      { m:'GET',  p:'/api/products/:id',    h:67,  p95:84  },
      { m:'GET',  p:'/api/cart',            h:88,  p95:34  },
      { m:'POST', p:'/api/cart',            h:46,  p95:46  },
      { m:'DELETE', p:'/api/cart/:sku',     h:11,  p95:38  },
      { m:'GET',  p:'/api/orders',          h:14,  p95:62  },
      { m:'GET',  p:'/api/orders/:id',      h:7,   p95:54  },
      { m:'POST', p:'/api/webhook',         h:3,   p95:1840 },
      { m:'POST', p:'/api/v1/export',       h:0,   p95:null },
      { m:'DELETE', p:'/api/v1/users/:id',  h:0,   p95:null },
      { m:'POST', p:'/api/v1/refunds',      h:0,   p95:null },
      { m:'POST', p:'/api/v1/admin/promote', h:0,  p95:null },
      { m:'GET',  p:'/api/v1/admin/audit',  h:0,   p95:null },
      { m:'GET',  p:'/api/health',          h:0,   p95:null },
      { m:'GET',  p:'/api/ready',           h:0,   p95:null },
      { m:'GET',  p:'/api/metrics',         h:0,   p95:null },
    ]
  },
  { svc:'cart-service',   source:'openapi', spec:'openapi.yaml', kind:'http',
    routes:[
      { m:'RPC',  p:'cart.v1.GetCart',      h:122, p95:118 },
      { m:'RPC',  p:'cart.v1.AddItem',      h:46,  p95:42  },
      { m:'RPC',  p:'cart.v1.RemoveItem',   h:11,  p95:30  },
      { m:'RPC',  p:'cart.v1.MergeCarts',   h:0,   p95:null },
      { m:'RPC',  p:'cart.v1.Clear',        h:0,   p95:null },
    ]
  },
  { svc:'pricing-service', source:'observed', kind:'rpc',
    routes:[
      { m:'RPC', p:'pricing.v1.Quote',      h:34, p95:80 },
      { m:'RPC', p:'pricing.v1.List',       h:62, p95:62 },
      { m:'RPC', p:'pricing.v1.Bulk',       h:8,  p95:140 },
    ]
  },
  { svc:'payment-service', source:'openapi', spec:'payment.proto', kind:'rpc',
    routes:[
      { m:'RPC', p:'payment.v1.Charge',     h:34, p95:308 },
      { m:'RPC', p:'payment.v1.Refund',     h:0,  p95:null },
      { m:'RPC', p:'payment.v1.Void',       h:0,  p95:null },
      { m:'RPC', p:'payment.v1.Status',     h:12, p95:24  },
      { m:'RPC', p:'payment.v1.Webhook',    h:3,  p95:1820 },
    ]
  },
  { svc:'inventory-service', source:'observed', kind:'rpc',
    routes:[
      { m:'RPC', p:'inventory.v1.Reserve',  h:34, p95:92 },
      { m:'RPC', p:'inventory.v1.Release',  h:5,  p95:42 },
      { m:'RPC', p:'auth.v1.Login',         h:18, p95:128 },
    ]
  },
  { svc:'postgres',  source:'observed', kind:'db', skip:true,
    routes:[
      { m:'SQL', p:'SELECT', h:412, p95:18 },
      { m:'SQL', p:'INSERT', h:84,  p95:14 },
      { m:'SQL', p:'UPDATE', h:24,  p95:28 },
    ]
  },
];

// ── Helpers ─────────────────────────────────────────────────────────────
function svcStats(svc) {
  const total = svc.routes.length;
  const observed = svc.routes.filter(r => r.h > 0).length;
  const dark = total - observed;
  const pct = total === 0 ? 0 : Math.round((observed / total) * 1000) / 10;
  return { total, observed, dark, pct };
}

function overallStats() {
  let total = 0, observed = 0;
  for (const s of COV_DATA) {
    if (s.skip) continue;
    total += s.routes.length;
    observed += s.routes.filter(r => r.h > 0).length;
  }
  const pct = total === 0 ? 0 : (observed / total) * 100;
  return { total, observed, dark: total - observed, pct };
}

function methodTone(m) {
  switch (m) {
    case 'GET':    return { fg:'#356a99', bg:'#cfdde9' };
    case 'POST':   return { fg:'#3e6a3e', bg:'#cfe2cf' };
    case 'PUT':    return { fg:'#7a6018', bg:'#ecdba6' };
    case 'PATCH':  return { fg:'#7a6018', bg:'#ecdba6' };
    case 'DELETE': return { fg:'#7a3a23', bg:'#ecc8b3' };
    case 'RPC':    return { fg:'#5a3f6e', bg:'#dccdec' };
    case 'SQL':    return { fg:'#3a5a6e', bg:'#c8dde6' };
    default:       return { fg:'var(--ink2)', bg:'var(--chipBg)' };
  }
}

function coverageColor(pct) {
  if (pct >= 90) return 'var(--ok)';
  if (pct >= 70) return 'color-mix(in oklch, var(--ok) 60%, var(--warn))';
  if (pct >= 40) return 'var(--warn)';
  return 'var(--danger)';
}

function coverageInk(pct) {
  if (pct >= 90) return '#3e6a3e';
  if (pct >= 70) return '#6a7a3d';
  if (pct >= 40) return 'var(--warnInk)';
  return 'var(--dangerInk)';
}

// ── Top "OpenAPI import" banner ─────────────────────────────────────────
function ImportBanner({ specs, onDismiss }) {
  return (
    <div style={{
      display:'flex', alignItems:'center', gap:14,
      padding:'12px 16px', borderRadius:10,
      background:'color-mix(in oklch, var(--accent) 10%, var(--surface))',
      border:'1px solid color-mix(in oklch, var(--accent) 30%, transparent)',
      marginBottom:16,
    }}>
      <div style={{
        width:34, height:34, borderRadius:8, flex:'0 0 auto',
        background:'var(--accent)', color:'var(--surface)',
        display:'inline-flex', alignItems:'center', justifyContent:'center',
      }}>
        <svg width="18" height="18" viewBox="0 0 16 16" fill="none">
          <path d="M3 3h6l4 4v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1z"
            stroke="currentColor" strokeWidth="1.4" fill="none" />
          <path d="M9 3v4h4" stroke="currentColor" strokeWidth="1.4" fill="none" />
          <path d="M5 9h6M5 11h6" stroke="currentColor" strokeWidth="1.4"
            strokeLinecap="round" />
        </svg>
      </div>
      <div style={{ flex:1, minWidth:0 }}>
        <div style={{
          fontFamily:'var(--sans)', fontSize:13, fontWeight:600, color:'var(--ink)',
        }}>
          {specs.length} OpenAPI / proto spec{specs.length === 1 ? '' : 's'} loaded — coverage % includes <strong>all</strong> declared routes
        </div>
        <div style={{
          fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)', marginTop:3,
          display:'flex', gap:8, flexWrap:'wrap',
        }}>
          {specs.map(s => (
            <span key={s} style={{
              padding:'1px 6px', borderRadius:4,
              background:'var(--surface2)', border:'1px solid var(--line)',
            }}>{s}</span>
          ))}
          <span style={{ color:'var(--ink3)' }}>
            services without a spec only count <em>observed</em> operations as 100%
          </span>
        </div>
      </div>
      <button type="button" style={{
        padding:'0 12px', height:30, borderRadius:6, cursor:'pointer',
        background:'var(--surface)', border:'1px solid var(--line)',
        fontFamily:'var(--sans)', fontSize:12, fontWeight:500, color:'var(--ink)',
        outline:'none',
        display:'inline-flex', alignItems:'center', gap:6,
      }}>+ import spec…</button>
    </div>
  );
}

// ── Summary bar (single coverage line, like nyc) ────────────────────────
function CoverageBar({ pct, observed, total, big = false, compact = false }) {
  return (
    <div style={{ display:'flex', flexDirection:'column', gap:5, width:'100%' }}>
      <div style={{
        height: big ? 12 : 8, borderRadius:8,
        background:'var(--surface3)',
        border:'1px solid var(--line)',
        overflow:'hidden', position:'relative',
      }}>
        <div style={{
          position:'absolute', left:0, top:0, bottom:0,
          width: pct + '%',
          background: coverageColor(pct),
          transition:'width .2s',
        }} />
      </div>
      {compact ? null : (
        <div style={{
          display:'flex', justifyContent:'space-between', gap:8,
          fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
        }}>
          <span style={{ whiteSpace:'nowrap' }}>
            <strong style={{ color: coverageInk(pct) }}>{pct.toFixed(1)}%</strong> covered
          </span>
          <span style={{ whiteSpace:'nowrap' }}>{observed.toLocaleString()} / {total.toLocaleString()} routes</span>
        </div>
      )}
    </div>
  );
}

// ── Heatmap cell ─────────────────────────────────────────────────────────
function HeatCell({ r, selected, onClick }) {
  const dark = r.h === 0;
  // hits → opacity ramp (log scale).  Dark = striped red.
  const intensity = dark ? 0 : Math.min(1, 0.18 + Math.log10(r.h + 1) / 3);
  const m = methodTone(r.m);
  return (
    <button type="button" onClick={onClick}
      title={`${r.m} ${r.p}  ·  ${r.h} traces`}
      style={{
        position:'relative', cursor:'pointer', textAlign:'left',
        padding:'7px 9px 7px 11px', borderRadius:6,
        background: dark
          ? `repeating-linear-gradient(45deg,
              color-mix(in oklch, var(--danger) 18%, var(--surface)) 0 5px,
              color-mix(in oklch, var(--danger) 8%, var(--surface)) 5px 10px)`
          : `color-mix(in oklch, ${m.fg} ${intensity * 100}%, var(--surface))`,
        border: '1px solid ' + (dark ? 'var(--danger)' : selected ? m.fg : 'var(--line)'),
        boxShadow: selected ? `inset 0 0 0 1px ${dark ? 'var(--danger)' : m.fg}` : 'none',
        fontFamily:'var(--mono)', fontSize:11, color: dark ? 'var(--dangerInk)' : 'var(--ink)',
        outline:'none',
        display:'flex', alignItems:'center', gap:7,
        minWidth:0,
      }}>
      {/* method tag */}
      <span style={{
        fontFamily:'var(--mono)', fontSize:9, fontWeight:700,
        color: m.fg, background: dark ? 'var(--surface)' : 'transparent',
        border: '1px solid ' + m.fg, borderRadius:3, padding:'0 4px',
        letterSpacing:'0.05em', whiteSpace:'nowrap', flex:'0 0 auto',
      }}>{r.m}</span>
      <span style={{
        flex:1, minWidth:0, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
        fontWeight: dark ? 600 : 500,
        color: dark ? 'var(--dangerInk)' : 'var(--ink)',
      }}>{r.p}</span>
      <span style={{
        fontFamily:'var(--mono)', fontSize:9.5,
        color: dark ? 'var(--dangerInk)' : 'var(--ink2)',
        fontWeight: dark ? 700 : 500,
      }}>{dark ? 'DARK' : r.h.toLocaleString()}</span>
    </button>
  );
}

// ── Service section ──────────────────────────────────────────────────────
function ServiceSection({ svc, theme, expanded, onToggle, selected, onSelect, filter }) {
  const stats = svcStats(svc);
  const filteredRoutes = svc.routes.filter(r => {
    if (filter === 'dark') return r.h === 0;
    if (filter === 'observed') return r.h > 0;
    return true;
  });
  return (
    <section style={{
      background:'var(--surface)', border:'1px solid var(--line)',
      borderRadius:10, marginBottom:14, overflow:'hidden',
    }}>
      <button type="button" onClick={onToggle}
        style={{
          width:'100%', padding:'12px 16px', cursor:'pointer',
          display:'grid',
          gridTemplateColumns:'auto minmax(120px, 1fr) 32px 220px 90px 80px 64px',
          gap:14, alignItems:'center',
          background:'transparent', border:'none', outline:'none',
          textAlign:'left',
        }}>
        <span style={{
          display:'inline-flex', alignItems:'center', justifyContent:'center',
          width:18, height:18,
          color:'var(--ink3)',
          transform: expanded ? 'rotate(90deg)' : 'rotate(0)',
          transition:'transform .15s',
        }}>
          <svg width="10" height="10" viewBox="0 0 10 10">
            <path d="M3 1.5l4 3.5-4 3.5" stroke="currentColor" strokeWidth="1.4"
              fill="none" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
        <div style={{ display:'flex', alignItems:'center', gap:10, minWidth:0, overflow:'hidden' }}>
          <SvcChip svc={svc.svc} theme={theme} size="md" />
          {svc.source === 'openapi' ? (
            <span style={{
              padding:'2px 7px', borderRadius:5,
              background:'color-mix(in oklch, var(--accent) 14%, var(--surface))',
              color:'var(--accentInk)',
              border:'1px solid color-mix(in oklch, var(--accent) 30%, transparent)',
              fontFamily:'var(--mono)', fontSize:9, fontWeight:600,
              whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
              maxWidth:'100%',
            }}>★ {svc.spec}</span>
          ) : (
            <span style={{
              padding:'2px 7px', borderRadius:5,
              background:'var(--surface2)', color:'var(--ink3)',
              border:'1px solid var(--line)',
              fontFamily:'var(--mono)', fontSize:9, fontWeight:600,
              whiteSpace:'nowrap', overflow:'hidden', textOverflow:'ellipsis',
              maxWidth:'100%',
            }}>observed only · no spec</span>
          )}
        </div>
        <span /> {/* spacer so the bar + stats float to the right */}
        <CoverageBar pct={stats.pct} observed={stats.observed} total={stats.total} compact />
        <div style={{ display:'flex', flexDirection:'column', gap:2 }}>
          <span style={{
            fontFamily:'var(--serif)', fontSize:18, fontWeight:600, lineHeight:1,
            color: coverageInk(stats.pct),
          }}>{stats.pct.toFixed(1)}<span style={{ fontSize:11, marginLeft:1 }}>%</span></span>
          <span style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em' }}>coverage</span>
        </div>
        <div style={{ display:'flex', flexDirection:'column', gap:2 }}>
          <span style={{
            fontFamily:'var(--serif)', fontSize:18, fontWeight:600, lineHeight:1,
            color: stats.dark > 0 ? 'var(--dangerInk)' : 'var(--ink3)',
          }}>{stats.dark}</span>
          <span style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em' }}>dark</span>
        </div>
        <div style={{ display:'flex', flexDirection:'column', gap:2 }}>
          <span style={{
            fontFamily:'var(--serif)', fontSize:18, fontWeight:600, lineHeight:1,
            color:'var(--ink)',
          }}>{stats.total}</span>
          <span style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em' }}>total</span>
        </div>
      </button>
      {expanded ? (
        <div style={{
          padding:'4px 14px 14px',
          display:'grid',
          gridTemplateColumns:'repeat(auto-fill, minmax(280px, 1fr))',
          gap:8,
          borderTop:'1px solid var(--line2)',
          background:'var(--surface2)',
        }}>
          {filteredRoutes.length === 0 ? (
            <div style={{
              gridColumn:'1 / -1', padding:'14px 6px',
              fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)',
              textAlign:'center',
            }}>no routes match this filter</div>
          ) : filteredRoutes.map((r, i) => (
            <HeatCell key={`${r.m}-${r.p}-${i}`}
              r={r}
              selected={selected && selected.svc === svc.svc && selected.path === r.p && selected.m === r.m}
              onClick={() => onSelect({ ...r, svc: svc.svc, source: svc.source, spec: svc.spec })} />
          ))}
        </div>
      ) : null}
    </section>
  );
}

// ── Detail rail ─────────────────────────────────────────────────────────
function RouteInspect({ route, theme }) {
  if (!route) return null;
  const dark = route.h === 0;
  const m = methodTone(route.m);
  return (
    <aside style={{
      width:340, borderLeft:'1px solid var(--line)',
      background:'var(--surface)',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <div style={{ padding:'14px 16px', borderBottom:'1px solid var(--line)' }}>
        <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:8 }}>
          <SvcChip svc={route.svc} theme={theme} size="md" />
          {dark
            ? <Badge tone="danger" size="md">DARK</Badge>
            : <Badge tone="ok" size="md">observed</Badge>}
        </div>
        <div style={{ display:'flex', alignItems:'baseline', gap:8 }}>
          <span style={{
            fontFamily:'var(--mono)', fontSize:11, fontWeight:700,
            color: m.fg, border:`1px solid ${m.fg}`,
            padding:'1px 6px', borderRadius:4, letterSpacing:'0.05em',
          }}>{route.m}</span>
          <span style={{
            fontFamily:'var(--mono)', fontSize:14, color:'var(--ink)',
            fontWeight:600, wordBreak:'break-all',
          }}>{route.p}</span>
        </div>
      </div>

      <div style={{
        display:'grid', gridTemplateColumns:'1fr 1fr', gap:0,
        borderBottom:'1px solid var(--line)',
      }}>
        <DetailStat label="traces seen" value={route.h.toLocaleString()}
          tone={dark ? 'danger' : 'default'} />
        <DetailStat label="p95" value={route.p95 != null ? route.p95 + 'ms' : '—'} />
      </div>

      <div style={{ padding:'14px 16px', borderBottom:'1px solid var(--line)' }}>
        <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
          textTransform:'uppercase', letterSpacing:'0.14em', marginBottom:8 }}>
          source
        </div>
        {route.source === 'openapi' ? (
          <div style={{ fontFamily:'var(--sans)', fontSize:12, color:'var(--ink2)', lineHeight:1.5 }}>
            Declared in <code style={{
              fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)',
              background:'var(--surface2)', padding:'1px 5px', borderRadius:3,
            }}>{route.spec}</code>. Counted toward the denominator regardless of whether Spaniel has observed a trace.
          </div>
        ) : (
          <div style={{ fontFamily:'var(--sans)', fontSize:12, color:'var(--ink2)', lineHeight:1.5 }}>
            Learned from incoming spans only — no spec is loaded for this service.  Import a spec to discover routes Spaniel has never seen.
          </div>
        )}
      </div>

      {dark ? (
        <div style={{ padding:'14px 16px', display:'flex', flexDirection:'column', gap:10 }}>
          <div style={{
            padding:'10px 12px', borderRadius:8,
            background:'color-mix(in oklch, var(--danger) 14%, var(--surface))',
            border:'1px solid color-mix(in oklch, var(--danger) 35%, transparent)',
          }}>
            <div style={{
              fontFamily:'var(--mono)', fontSize:10, fontWeight:700,
              color:'var(--dangerInk)', letterSpacing:'0.08em', marginBottom:5,
            }}>● no traces — instrument this route</div>
            <div style={{ fontFamily:'var(--sans)', fontSize:12, color:'var(--ink)', lineHeight:1.5 }}>
              Spaniel hasn't seen <em>any</em> span whose <code style={{
                fontFamily:'var(--mono)', fontSize:11,
                background:'var(--surface)', padding:'1px 4px', borderRadius:3,
              }}>http.route</code> matches this path.
              Either no one is exercising the endpoint, or the handler isn't wrapped with{' '}
              <code style={{ fontFamily:'var(--mono)', fontSize:11 }}>otelhttp</code>.
            </div>
          </div>
          <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em' }}>suggested fix</div>
          <pre style={{
            margin:0, fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)',
            background:'var(--surface2)', border:'1px solid var(--line)',
            padding:'10px 12px', borderRadius:6, lineHeight:1.5,
            overflow:'auto',
          }}>{`mux.Handle(
  "${route.p}",
  otelhttp.NewHandler(
    handler, "${route.m} ${route.p}",
  ),
)`}</pre>
          <button type="button" style={{
            height:30, padding:'0 12px', borderRadius:6, cursor:'pointer',
            background:'var(--accent)', border:'1px solid var(--accent)',
            color:'var(--surface)', fontFamily:'var(--sans)', fontSize:12, fontWeight:600,
            outline:'none',
          }}>copy snippet</button>
        </div>
      ) : (
        <div style={{ padding:'14px 16px', display:'flex', flexDirection:'column', gap:10 }}>
          <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em' }}>recent traces</div>
          <div style={{ display:'flex', flexDirection:'column', gap:6 }}>
            {[1,2,3].map(i => (
              <div key={i} style={{
                display:'flex', alignItems:'center', gap:8,
                padding:'7px 10px', borderRadius:6,
                background:'var(--surface2)', border:'1px solid var(--line)',
              }}>
                <span style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)' }}>
                  {(['9ef3','7b2a','a18c'])[i-1]}…
                </span>
                <span style={{ flex:1 }} />
                <span style={{
                  fontFamily:'var(--mono)', fontSize:11, fontWeight:600, color:'var(--ink)',
                }}>{Math.round((route.p95 || 50) * (0.6 + (i*0.2)))}ms</span>
                <span style={{
                  fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
                }}>{i}m ago</span>
              </div>
            ))}
          </div>
          <button type="button" style={{
            height:30, padding:'0 12px', borderRadius:6, cursor:'pointer',
            background:'var(--surface)', border:'1px solid var(--line)',
            color:'var(--ink)', fontFamily:'var(--sans)', fontSize:12, fontWeight:500,
            outline:'none',
          }}>open in traces →</button>
        </div>
      )}
    </aside>
  );
}

function DetailStat({ label, value, tone }) {
  return (
    <div style={{ padding:'14px 16px', borderRight:'1px solid var(--line)' }}>
      <div style={{
        fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em',
      }}>{label}</div>
      <div style={{
        fontFamily:'var(--serif)', fontSize:24, fontWeight:600, lineHeight:1.1, marginTop:4,
        color: tone === 'danger' ? 'var(--dangerInk)' : 'var(--ink)',
      }}>{value}</div>
    </div>
  );
}

// ── Page root ───────────────────────────────────────────────────────────
function Coverage({ theme }) {
  const [filter,   setFilter]   = covState('all');       // 'all' | 'dark' | 'observed'
  const [selected, setSelected] = covState({ svc:'api-gateway', m:'POST', p:'/api/v1/export',
    h:0, p95:null, source:'openapi', spec:'openapi.yaml' });
  const [expanded, setExpanded] = covState(() => ({ 'api-gateway': true, 'payment-service': true }));

  const overall = covMemo(() => overallStats(), []);
  const totals = covMemo(() => {
    let dark = 0;
    for (const s of COV_DATA) {
      if (s.skip) continue;
      dark += s.routes.filter(r => r.h === 0).length;
    }
    return { dark };
  }, []);

  const toggle = (svcName) =>
    setExpanded(prev => ({ ...prev, [svcName]: !prev[svcName] }));

  const visibleSvcs = COV_DATA.filter(s => !s.skip);

  return (
    <Frame>
      <Chrome active="coverage" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <Sidebar>
          <SbGroup title="filter">
            <SbItem active={filter === 'all'} dot="var(--accent)"
              count={overall.total}
              onClick={() => setFilter('all')}>all routes</SbItem>
            <SbItem active={filter === 'dark'} dot="var(--danger)"
              count={overall.dark}
              onClick={() => setFilter('dark')}>dark only</SbItem>
            <SbItem active={filter === 'observed'} dot="var(--ok)"
              count={overall.observed}
              onClick={() => setFilter('observed')}>observed only</SbItem>
          </SbGroup>
          <SbGroup title="sources">
            <SbItem dot="var(--accent)"
              count={COV_DATA.filter(s => !s.skip && s.source === 'openapi').length}>openapi specs</SbItem>
            <SbItem dot="var(--ink3)"
              count={COV_DATA.filter(s => !s.skip && s.source === 'observed').length}>observed-only</SbItem>
          </SbGroup>
          <SbGroup title="actions">
            <SbItem dot="var(--accent)" onClick={() => {}}>+ import spec…</SbItem>
            <SbItem dot="var(--ink3)" onClick={() => {}}>export report.json</SbItem>
            <SbItem dot="var(--ink3)" onClick={() => {}}>copy as markdown</SbItem>
          </SbGroup>
          <div style={{ flex:1 }} />
          <SbGroup title="cli">
            <div style={{
              fontFamily:'var(--mono)', fontSize:10, color:'var(--ink2)',
              padding:'6px 8px', background:'var(--surface)',
              border:'1px solid var(--line)', borderRadius:6, lineHeight:1.5,
            }}>
              <div>spaniel coverage</div>
              <div style={{ color:'var(--ink3)' }}>--routes-file openapi.yaml</div>
            </div>
          </SbGroup>
        </Sidebar>

        <div style={{ flex:1, overflow:'hidden auto',
          display:'flex', flexDirection:'column' }}>
          <div style={{ padding:'22px 24px 0' }}>
            <div style={{
              fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.18em', marginBottom:6,
            }}>instrumentation coverage</div>
            <h1 style={{
              margin:0, fontFamily:'var(--serif)', fontSize:28, fontWeight:600,
              letterSpacing:'-0.02em', color:'var(--ink)',
            }}>Coverage</h1>
            <div style={{
              fontFamily:'var(--sans)', fontSize:13, color:'var(--ink2)',
              marginTop:4, maxWidth:720,
            }}>
              Which routes have we ever seen a trace for?  Spaniel cross-references
              every <code style={{
                fontFamily:'var(--mono)', fontSize:12, background:'var(--surface2)',
                padding:'1px 5px', borderRadius:4,
              }}>http.route</code> and RPC method against your declared specs to find
              the dark ones.
            </div>
          </div>

          <div style={{ padding:'18px 24px 4px' }}>
            <ImportBanner
              specs={[...new Set(COV_DATA.filter(s => !s.skip && s.source === 'openapi').map(s => s.spec))]} />

            {/* Big overall card */}
            <div style={{
              display:'grid', gridTemplateColumns:'1.4fr 1fr 1fr 1fr',
              gap:0, marginBottom:18,
              background:'var(--surface)', border:'1px solid var(--line)',
              borderRadius:10, overflow:'hidden',
            }}>
              <div style={{ padding:'18px 20px', borderRight:'1px solid var(--line)' }}>
                <div style={{
                  fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
                  textTransform:'uppercase', letterSpacing:'0.14em',
                }}>overall</div>
                <div style={{ display:'flex', alignItems:'baseline', gap:6, marginTop:4 }}>
                  <span style={{
                    fontFamily:'var(--serif)', fontSize:40, fontWeight:600,
                    color: coverageInk(overall.pct), lineHeight:1,
                  }}>{overall.pct.toFixed(1)}</span>
                  <span style={{ fontFamily:'var(--serif)', fontSize:20, color:'var(--ink3)' }}>%</span>
                  <span style={{
                    fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)', marginLeft:8,
                  }}>{overall.observed} of {overall.total} routes</span>
                </div>
                <div style={{ marginTop:14 }}>
                  <CoverageBar pct={overall.pct} observed={overall.observed}
                    total={overall.total} big />
                </div>
              </div>
              <BigStat label="dark" value={overall.dark} tone="danger"
                sub={`across ${visibleSvcs.filter(s => svcStats(s).dark > 0).length} services`} />
              <BigStat label="services" value={visibleSvcs.length}
                sub={`${visibleSvcs.filter(s => s.source === 'openapi').length} have specs`} />
              <BigStat label="best service" value={
                visibleSvcs.reduce((best, s) => svcStats(s).pct > svcStats(best).pct ? s : best,
                  visibleSvcs[0]).svc
              } sub="100% covered" mono />
            </div>

            {/* Per-service rows */}
            {visibleSvcs.map(svc => (
              <ServiceSection
                key={svc.svc} svc={svc} theme={theme}
                expanded={!!expanded[svc.svc]}
                onToggle={() => toggle(svc.svc)}
                selected={selected} onSelect={setSelected}
                filter={filter} />
            ))}

            <div style={{ height:30 }} />

            {/* Legend at the bottom */}
            <div style={{
              padding:'12px 14px', borderRadius:8,
              background:'var(--surface)', border:'1px solid var(--line)',
              display:'flex', alignItems:'center', gap:18, flexWrap:'wrap',
              fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
            }}>
              <span style={{ color:'var(--ink3)', textTransform:'uppercase', letterSpacing:'0.14em' }}>
                legend
              </span>
              <LegendCell label="hot · many traces" intensity={0.85} method="GET" />
              <LegendCell label="warm" intensity={0.45} method="POST" />
              <LegendCell label="cold · few" intensity={0.2} method="GET" />
              <LegendCell label="dark · zero" dark method="POST" />
              <span style={{ flex:1 }} />
              <span style={{ color:'var(--ink3)' }}>cell color = log(hits + 1) · darker = more</span>
            </div>
            <div style={{ height:30 }} />
          </div>
        </div>

        <RouteInspect route={selected} theme={theme} />
      </div>
    </Frame>
  );
}

function LegendCell({ label, intensity, dark, method }) {
  const m = methodTone(method);
  return (
    <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
      <span style={{
        width:42, height:18, borderRadius:4,
        background: dark
          ? `repeating-linear-gradient(45deg,
              color-mix(in oklch, var(--danger) 18%, var(--surface)) 0 4px,
              color-mix(in oklch, var(--danger) 8%, var(--surface)) 4px 8px)`
          : `color-mix(in oklch, ${m.fg} ${intensity * 100}%, var(--surface))`,
        border:'1px solid ' + (dark ? 'var(--danger)' : 'var(--line)'),
      }} />
      <span>{label}</span>
    </span>
  );
}

function BigStat({ label, value, sub, tone, mono }) {
  return (
    <div style={{ padding:'18px 20px', borderRight:'1px solid var(--line)' }}>
      <div style={{
        fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em',
      }}>{label}</div>
      <div style={{
        fontFamily: mono ? 'var(--mono)' : 'var(--serif)',
        fontSize: mono ? 18 : 32, fontWeight:600,
        marginTop:4, lineHeight:1.05,
        color: tone === 'danger' ? 'var(--dangerInk)' : 'var(--ink)',
      }}>{value}</div>
      {sub ? <div style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', marginTop:6,
      }}>{sub}</div> : null}
    </div>
  );
}

window.Coverage = Coverage;
