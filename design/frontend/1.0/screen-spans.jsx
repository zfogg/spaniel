// Spaniel — Spans page (Drift direction).  A flat browser over every span
// in the local DuckDB: searchable, filterable, with a full attribute
// inspector for whichever span is selected.

const { useState: useSpansState, useMemo: useSpansMemo } = React;
const { Chrome, Sidebar, SbGroup, SbItem, SvcChip, Badge, Frame, fmtMs: _fm } = window.UI;

// ── Sample dataset: ~30 spans from 5 different traces ───────────────────
const ALL_SPANS = [
  // Trace: POST /api/checkout (9ef3…d19eb)
  { id:'9ef3.s1',  trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'api-gateway',       name:'POST /api/checkout',           kind:'server',   dur:612, start:'14:22:08.421', tag:null,
    attrs:{ 'http.request.method':'POST', 'http.route':'/api/checkout', 'http.response.status_code':200, 'url.path':'/api/checkout', 'user.id':'u_31288', 'service.name':'api-gateway', 'service.version':'2.14.0', 'otel.library.name':'spaniel/sdk-go', 'otel.library.version':'0.4.2' } },
  { id:'9ef3.s4',  trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'cart-service',      name:'cart.GetCart',                 kind:'server',   dur:118, start:'14:22:08.435', tag:null,
    attrs:{ 'rpc.system':'grpc', 'rpc.service':'cart.v1.CartService', 'rpc.method':'GetCart', 'net.peer.name':'cart.local', 'net.peer.port':9091, 'cart.user_id':'u_31288', 'cart.items_count':6 } },
  { id:'9ef3.s5',  trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'postgres',          name:'SELECT * FROM carts',          kind:'client',   dur:12,  start:'14:22:08.439', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.name':'carts', 'db.sql.table':'carts', 'db.statement':'SELECT * FROM carts WHERE user_id=$1', 'net.peer.name':'pg-primary', 'net.peer.port':5432 } },
  { id:'9ef3.s7',  trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'pricing-service',   name:'pricing.Quote',                kind:'server',   dur:80,  start:'14:22:08.455', tag:'n+1',
    attrs:{ 'rpc.system':'grpc', 'rpc.method':'Quote', 'pricing.cache.hit':false, 'pricing.skus_count':5 } },
  { id:'9ef3.s9',  trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'postgres',          name:'SELECT price WHERE sku=?',     kind:'client',   dur:8,   start:'14:22:08.461', tag:'n+1',
    attrs:{ 'db.system':'postgresql', 'db.statement':'SELECT price WHERE sku = $1', 'db.name':'pricing', 'db.sql.table':'product_prices', 'net.peer.name':'pg-primary' } },
  { id:'9ef3.s10', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'postgres',          name:'SELECT price WHERE sku=?',     kind:'client',   dur:7,   start:'14:22:08.471', tag:'n+1',
    attrs:{ 'db.system':'postgresql', 'db.statement':'SELECT price WHERE sku = $1' } },
  { id:'9ef3.s11', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'postgres',          name:'SELECT price WHERE sku=?',     kind:'client',   dur:8,   start:'14:22:08.481', tag:'n+1',
    attrs:{ 'db.system':'postgresql', 'db.statement':'SELECT price WHERE sku = $1' } },
  { id:'9ef3.s15', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'inventory-service', name:'inventory.Reserve',            kind:'server',   dur:92,  start:'14:22:08.563', tag:'lint',
    attrs:{ 'rpc.system':'grpc', 'inventory.skus_count':5 } },
  { id:'9ef3.s17', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'postgres',          name:'UPDATE stock SET reserved=…', kind:'client',   dur:46,  start:'14:22:08.595', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.sql.table':'stock', 'db.operation':'UPDATE' } },
  { id:'9ef3.s19', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'payment-service',   name:'payment.Charge',               kind:'server',   dur:308, start:'14:22:08.663', tag:'slow',
    attrs:{ 'rpc.system':'grpc', 'payment.amount_cents':24800, 'payment.currency':'USD' } },
  { id:'9ef3.s20', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'stripe',            name:'http.stripe POST /v1/charges', kind:'client',   dur:287, start:'14:22:08.669', tag:'slow',
    attrs:{ 'http.request.method':'POST', 'url.full':'https://api.stripe.com/v1/charges', 'http.response.status_code':200, 'http.request.body.size':542 } },
  { id:'9ef3.s22', trace:'9ef3…d19eb', op:'POST /api/checkout',     svc:'api-gateway',       name:'kafka.publish order.created',  kind:'producer', dur:54,  start:'14:22:08.977', tag:null,
    attrs:{ 'messaging.system':'kafka', 'messaging.destination.name':'order.created', 'messaging.kafka.partition':3 } },

  // Trace: GET /api/products (7b2a…84c19)
  { id:'7b2a.s1', trace:'7b2a…84c19', op:'GET /api/products',     svc:'api-gateway',     name:'GET /api/products',         kind:'server',   dur:78, start:'14:22:04.118', tag:null,
    attrs:{ 'http.request.method':'GET', 'http.route':'/api/products', 'http.response.status_code':200 } },
  { id:'7b2a.s2', trace:'7b2a…84c19', op:'GET /api/products',     svc:'pricing-service', name:'product.List',              kind:'server',   dur:62, start:'14:22:04.122', tag:null,
    attrs:{ 'rpc.system':'grpc', 'rpc.method':'List', 'product.page_size':24 } },
  { id:'7b2a.s3', trace:'7b2a…84c19', op:'GET /api/products',     svc:'redis',           name:'cache.get products:list',   kind:'client',   dur:1,  start:'14:22:04.128', tag:null,
    attrs:{ 'db.system':'redis', 'db.operation':'GET', 'db.statement':'GET products:list:page=1' } },
  { id:'7b2a.s4', trace:'7b2a…84c19', op:'GET /api/products',     svc:'postgres',        name:'SELECT * FROM products',     kind:'client',   dur:38, start:'14:22:04.131', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.sql.table':'products', 'db.statement':'SELECT * FROM products WHERE active=true LIMIT 24' } },

  // Trace: POST /api/login (52ff…91a02)
  { id:'52ff.s1', trace:'52ff…91a02', op:'POST /api/login',       svc:'api-gateway',     name:'POST /api/login',           kind:'server',   dur:142, start:'14:22:00.224', tag:null,
    attrs:{ 'http.request.method':'POST', 'http.route':'/api/login', 'http.response.status_code':200 } },
  { id:'52ff.s2', trace:'52ff…91a02', op:'POST /api/login',       svc:'inventory-service',name:'auth.Login',               kind:'server',   dur:128, start:'14:22:00.228', tag:null,
    attrs:{ 'rpc.system':'grpc', 'rpc.method':'Login', 'enduser.id':'u_31288' } },
  { id:'52ff.s3', trace:'52ff…91a02', op:'POST /api/login',       svc:'postgres',        name:'SELECT user WHERE email=?', kind:'client',   dur:6,   start:'14:22:00.232', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.statement':'SELECT * FROM users WHERE email = $1' } },
  { id:'52ff.s4', trace:'52ff…91a02', op:'POST /api/login',       svc:'inventory-service',name:'bcrypt.compare',           kind:'internal', dur:108, start:'14:22:00.240', tag:'slow',
    attrs:{ 'crypto.algorithm':'bcrypt', 'crypto.cost':12 } },
  { id:'52ff.s5', trace:'52ff…91a02', op:'POST /api/login',       svc:'postgres',        name:'UPDATE users SET last_login',kind:'client',  dur:4,   start:'14:22:00.350', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.operation':'UPDATE', 'db.sql.table':'users' } },

  // Trace: GET /api/cart (a18c…2204e)
  { id:'a18c.s1', trace:'a18c…2204e', op:'GET /api/cart',          svc:'api-gateway',     name:'GET /api/cart',             kind:'server',   dur:34, start:'14:21:50.018', tag:null,
    attrs:{ 'http.request.method':'GET', 'http.route':'/api/cart' } },
  { id:'a18c.s2', trace:'a18c…2204e', op:'GET /api/cart',          svc:'cart-service',    name:'cart.GetCart',              kind:'server',   dur:28, start:'14:21:50.020', tag:null,
    attrs:{ 'rpc.system':'grpc', 'cart.user_id':'u_31288' } },
  { id:'a18c.s3', trace:'a18c…2204e', op:'GET /api/cart',          svc:'postgres',        name:'SELECT * FROM carts',       kind:'client',   dur:6,  start:'14:21:50.024', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.statement':'SELECT * FROM carts WHERE user_id = $1' } },

  // Trace: POST /api/webhook (c041…77b3d) — errored
  { id:'c041.s1', trace:'c041…77b3d', op:'POST /api/webhook',     svc:'api-gateway',     name:'POST /api/webhook',         kind:'server',   dur:1840, start:'14:21:08.000', tag:'slow',
    attrs:{ 'http.request.method':'POST', 'http.route':'/api/webhook', 'http.response.status_code':500, 'error':true } },
  { id:'c041.s2', trace:'c041…77b3d', op:'POST /api/webhook',     svc:'payment-service', name:'webhook.Process',           kind:'server',   dur:1820, start:'14:21:08.005', tag:'slow',
    attrs:{ 'rpc.system':'grpc', 'webhook.source':'stripe', 'webhook.event_type':'invoice.payment_failed' } },
  { id:'c041.s3', trace:'c041…77b3d', op:'POST /api/webhook',     svc:'stripe',          name:'http.external retry',       kind:'client',   dur:1500, start:'14:21:08.020', tag:'slow',
    attrs:{ 'http.request.method':'GET', 'url.full':'https://api.stripe.com/v1/invoices/in_…', 'http.response.status_code':429, 'http.retry_count':3, 'error':true, 'error.type':'rate_limited' } },
  { id:'c041.s4', trace:'c041…77b3d', op:'POST /api/webhook',     svc:'postgres',        name:'INSERT INTO webhook_log',   kind:'client',   dur:14, start:'14:21:09.825', tag:null,
    attrs:{ 'db.system':'postgresql', 'db.operation':'INSERT', 'db.sql.table':'webhook_log' } },
];

// All services + kinds for filter facets
const SVC_FACETS = (() => {
  const m = {};
  ALL_SPANS.forEach(s => { m[s.svc] = (m[s.svc] || 0) + 1; });
  return Object.entries(m).sort((a,b) => b[1] - a[1]);
})();
const KIND_FACETS = (() => {
  const m = {};
  ALL_SPANS.forEach(s => { m[s.kind] = (m[s.kind] || 0) + 1; });
  return Object.entries(m).sort((a,b) => b[1] - a[1]);
})();

// ── The page ────────────────────────────────────────────────────────────
function Spans({ theme }) {
  const [query,    setQuery]    = useSpansState('');
  const [svcSel,   setSvcSel]   = useSpansState(null);   // service filter
  const [kindSel,  setKindSel]  = useSpansState(null);   // kind filter
  const [tagSel,   setTagSel]   = useSpansState(null);   // 'n+1' | 'slow' | 'lint' | 'error' | null
  const [selected, setSelected] = useSpansState('9ef3.s9');
  const [sortBy,   setSortBy]   = useSpansState('time'); // 'time' | 'dur' | 'name'

  const filtered = useSpansMemo(() => {
    const q = query.trim().toLowerCase();
    let out = ALL_SPANS.filter(s => {
      if (svcSel && s.svc !== svcSel) return false;
      if (kindSel && s.kind !== kindSel) return false;
      if (tagSel === 'error' && !s.attrs.error) return false;
      if (tagSel && tagSel !== 'error' && s.tag !== tagSel) return false;
      if (q) {
        const hay = [s.name, s.svc, s.trace, s.op,
          ...Object.keys(s.attrs), ...Object.values(s.attrs).map(String)].join(' ').toLowerCase();
        if (!hay.includes(q)) return false;
      }
      return true;
    });
    if (sortBy === 'dur')  out = [...out].sort((a,b) => b.dur - a.dur);
    if (sortBy === 'name') out = [...out].sort((a,b) => a.name.localeCompare(b.name));
    return out;
  }, [query, svcSel, kindSel, tagSel, sortBy]);

  const sel = ALL_SPANS.find(s => s.id === selected) || filtered[0];

  return (
    <Frame>
      <Chrome active="spans" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <Sidebar>
          <SbGroup title="service">
            <SbItem active={!svcSel} onClick={() => setSvcSel(null)} count={ALL_SPANS.length}>
              all services
            </SbItem>
            {SVC_FACETS.map(([svc, n]) => {
              const c = theme.svc[svc] || {};
              return (
                <SbItem key={svc} active={svcSel === svc}
                  dot={c.fg} count={n}
                  onClick={() => setSvcSel(svcSel === svc ? null : svc)}>
                  {svc}
                </SbItem>
              );
            })}
          </SbGroup>
          <SbGroup title="kind">
            {KIND_FACETS.map(([k, n]) => (
              <SbItem key={k} active={kindSel === k} count={n}
                onClick={() => setKindSel(kindSel === k ? null : k)}>
                {k}
              </SbItem>
            ))}
          </SbGroup>
          <SbGroup title="callouts">
            <SbItem active={tagSel === 'n+1'}   dot="var(--danger)" count={ALL_SPANS.filter(s => s.tag === 'n+1').length}
              onClick={() => setTagSel(tagSel === 'n+1' ? null : 'n+1')}>n+1</SbItem>
            <SbItem active={tagSel === 'slow'}  dot="var(--warn)"   count={ALL_SPANS.filter(s => s.tag === 'slow').length}
              onClick={() => setTagSel(tagSel === 'slow' ? null : 'slow')}>slow ({'>'}250ms)</SbItem>
            <SbItem active={tagSel === 'lint'}  dot="var(--warn)"   count={ALL_SPANS.filter(s => s.tag === 'lint').length}
              onClick={() => setTagSel(tagSel === 'lint' ? null : 'lint')}>lint warnings</SbItem>
            <SbItem active={tagSel === 'error'} dot="var(--danger)" count={ALL_SPANS.filter(s => s.attrs.error).length}
              onClick={() => setTagSel(tagSel === 'error' ? null : 'error')}>errored</SbItem>
          </SbGroup>
          <div style={{ flex:1 }} />
          <SbGroup title="db usage">
            <div style={{
              fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
              padding:'6px 8px', background:'var(--surface)',
              border:'1px solid var(--line)', borderRadius:6, lineHeight:1.5,
            }}>
              <div>12,438 spans · 327 traces</div>
              <div style={{ color:'var(--ink3)' }}>612 MB · last 24h</div>
            </div>
          </SbGroup>
        </Sidebar>

        {/* Table */}
        <div style={{
          flex:1, minWidth:0, display:'flex', flexDirection:'column', overflow:'hidden',
          background:'var(--surface)',
        }}>
          {/* Search + active filters */}
          <div style={{
            padding:'10px 14px', borderBottom:'1px solid var(--line)',
            background:'var(--surface)',
            display:'flex', alignItems:'center', gap:10, flexWrap:'wrap',
          }}>
            <div style={{
              display:'inline-flex', alignItems:'center', gap:7,
              background:'var(--surface2)', border:'1px solid var(--line)',
              borderRadius:8, padding:'0 10px', height:30, flex:1, minWidth:240,
            }}>
              <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
                <circle cx="6" cy="6" r="4" stroke="var(--ink3)" strokeWidth="1.4" />
                <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="var(--ink3)"
                  strokeWidth="1.4" strokeLinecap="round" />
              </svg>
              <input value={query} onChange={(e) => setQuery(e.target.value)}
                placeholder="search by name, attribute key or value, trace id…"
                style={{
                  flex:1, border:'none', outline:'none', background:'transparent',
                  fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
                }} />
              {query ? <button type="button" onClick={() => setQuery('')} style={{
                background:'transparent', border:'none', cursor:'pointer',
                fontFamily:'var(--mono)', color:'var(--ink3)', fontSize:11, padding:0,
              }}>clear</button> : null}
            </div>
            <FilterChip on={!!svcSel}    onClear={() => setSvcSel(null)}>svc: {svcSel || '*'}</FilterChip>
            <FilterChip on={!!kindSel}   onClear={() => setKindSel(null)}>kind: {kindSel || '*'}</FilterChip>
            <FilterChip on={!!tagSel}    onClear={() => setTagSel(null)}>tag: {tagSel || '*'}</FilterChip>
            <div style={{
              display:'inline-flex', alignItems:'center', gap:6,
              fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
              padding:'0 10px', height:30, borderRadius:6,
              background:'var(--surface2)', border:'1px solid var(--line)',
            }}>
              <span style={{ color:'var(--ink3)' }}>sort</span>
              <select value={sortBy} onChange={(e) => setSortBy(e.target.value)}
                style={{
                  border:'none', outline:'none', background:'transparent',
                  fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)',
                }}>
                <option value="time">time ↓</option>
                <option value="dur">duration ↓</option>
                <option value="name">name a→z</option>
              </select>
            </div>
          </div>

          {/* Column header */}
          <div style={{
            display:'grid',
            gridTemplateColumns:'minmax(0,1.6fr) 130px 80px 70px 130px 60px',
            gap:10, padding:'7px 14px', borderBottom:'1px solid var(--line)',
            fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em',
            background:'var(--surface2)',
          }}>
            <div>name</div>
            <div>service</div>
            <div>kind</div>
            <div style={{ textAlign:'right' }}>dur</div>
            <div>trace</div>
            <div style={{ textAlign:'right' }}>tag</div>
          </div>

          {/* Rows */}
          <div style={{ flex:1, overflow:'hidden auto' }}>
            {filtered.map((s) => {
              const isSel = s.id === sel?.id;
              return (
                <button key={s.id} type="button" onClick={() => setSelected(s.id)}
                  style={{
                    display:'grid', textAlign:'left', cursor:'pointer',
                    gridTemplateColumns:'minmax(0,1.6fr) 130px 80px 70px 130px 60px',
                    gap:10, padding:'8px 14px', alignItems:'center',
                    border:'none',
                    width:'100%',
                    background: isSel
                      ? 'color-mix(in oklch, var(--accent) 14%, var(--surface))'
                      : 'transparent',
                    borderLeft: isSel ? '2px solid var(--accent)' : '2px solid transparent',
                    borderBottom:'1px solid var(--line2)',
                    outline:'none',
                  }}>
                  <div style={{
                    fontFamily:'var(--mono)', fontSize:11.5, color:'var(--ink)',
                    overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                  }}>{s.name}</div>
                  <div><SvcChip svc={s.svc} theme={theme} /></div>
                  <div style={{
                    fontFamily:'var(--mono)', fontSize:10, color:'var(--ink2)',
                    textTransform:'uppercase', letterSpacing:'0.08em',
                  }}>{s.kind}</div>
                  <div style={{
                    textAlign:'right', fontFamily:'var(--mono)', fontSize:11.5,
                    color: s.dur > 250 ? 'var(--dangerInk)' : 'var(--ink)', fontWeight:600,
                  }}>{_fm(s.dur)}</div>
                  <div style={{
                    fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
                  }}>{s.trace}</div>
                  <div style={{ textAlign:'right' }}>
                    {s.tag === 'n+1' ? <Badge tone="danger">n+1</Badge>
                      : s.tag === 'slow' ? <Badge tone="warn">slow</Badge>
                      : s.tag === 'lint' ? <Badge tone="warn">lint</Badge>
                      : s.attrs.error ? <Badge tone="danger">err</Badge>
                      : null}
                  </div>
                </button>
              );
            })}
            {filtered.length === 0 ? (
              <div style={{
                padding:'40px 20px', textAlign:'center',
                fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)',
              }}>no spans match — clear a filter or try different query</div>
            ) : null}
          </div>

          {/* Status footer */}
          <div style={{
            padding:'8px 14px', borderTop:'1px solid var(--line)',
            background:'var(--surface2)',
            fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
            display:'flex', alignItems:'center', gap:14,
          }}>
            <span><strong style={{ color:'var(--ink)' }}>{filtered.length.toLocaleString()}</strong> of 12,438 spans</span>
            <span style={{ color:'var(--ink3)' }}>·</span>
            <span>last 24h</span>
            <span style={{ flex:1 }} />
            <span style={{
              padding:'3px 8px', borderRadius:5, border:'1px solid var(--line)',
              background:'var(--surface)', color:'var(--ink3)',
            }}>spaniel spans --tail</span>
          </div>
        </div>

        {/* Attribute inspector */}
        {sel ? <SpanInspect span={sel} theme={theme} /> : null}
      </div>
    </Frame>
  );
}

function FilterChip({ on, onClear, children }) {
  if (!on) return null;
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:5,
      padding:'4px 8px 4px 10px', borderRadius:14,
      fontFamily:'var(--mono)', fontSize:11, fontWeight:500,
      background:'color-mix(in oklch, var(--accent) 14%, var(--surface))',
      color:'var(--accentInk)', border:'1px solid color-mix(in oklch, var(--accent) 30%, transparent)',
    }}>
      {children}
      <button type="button" onClick={onClear} style={{
        background:'transparent', border:'none', cursor:'pointer',
        color:'inherit', fontSize:13, lineHeight:1, padding:0,
        marginLeft:2,
      }}>×</button>
    </span>
  );
}

// ── Detail panel ────────────────────────────────────────────────────────
function SpanInspect({ span, theme }) {
  const entries = Object.entries(span.attrs);
  return (
    <aside style={{
      width:380, borderLeft:'1px solid var(--line)',
      background:'var(--surface)',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <div style={{ padding:'14px 16px', borderBottom:'1px solid var(--line)' }}>
        <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:8 }}>
          <SvcChip svc={span.svc} theme={theme} size="md" />
          <span style={{
            fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em',
          }}>{span.kind}</span>
          <div style={{ flex:1 }} />
          {span.tag === 'n+1' ? <Badge tone="danger" size="md">n+1</Badge>
            : span.tag === 'slow' ? <Badge tone="warn" size="md">slow</Badge>
            : span.tag === 'lint' ? <Badge tone="warn" size="md">lint</Badge>
            : span.attrs.error ? <Badge tone="danger" size="md">err</Badge>
            : null}
        </div>
        <div style={{
          fontFamily:'var(--mono)', fontSize:13, color:'var(--ink)',
          lineHeight:1.4, wordBreak:'break-word',
        }}>{span.name}</div>
        <div style={{
          display:'grid', gridTemplateColumns:'1fr 1fr', gap:12, marginTop:12,
        }}>
          <div>
            <div style={{
              fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.14em',
            }}>duration</div>
            <div style={{
              fontFamily:'var(--serif)', fontSize:22, fontWeight:600,
              color: span.dur > 250 ? 'var(--dangerInk)' : 'var(--ink)',
              lineHeight:1, marginTop:3,
            }}>{_fm(span.dur)}</div>
          </div>
          <div>
            <div style={{
              fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.14em',
            }}>started</div>
            <div style={{
              fontFamily:'var(--mono)', fontSize:13, color:'var(--ink)',
              marginTop:6,
            }}>{span.start}</div>
          </div>
        </div>
      </div>

      <div style={{ padding:'14px 16px', borderBottom:'1px solid var(--line)',
        background:'var(--surface2)' }}>
        <div style={{
          fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
          textTransform:'uppercase', letterSpacing:'0.14em', marginBottom:5,
        }}>belongs to trace</div>
        <div style={{ display:'flex', alignItems:'center', gap:8 }}>
          <span style={{
            fontFamily:'var(--mono)', fontSize:13, fontWeight:600, color:'var(--ink)',
          }}>{span.op}</span>
        </div>
        <div style={{
          fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', marginTop:2,
        }}>{span.trace} · <a href="#" style={{
          color:'var(--accentInk)', textDecoration:'underline', textDecorationStyle:'dotted',
        }}>open waterfall</a></div>
      </div>

      <div style={{ flex:1, overflow:'hidden auto' }}>
        <div style={{
          padding:'12px 16px 4px',
          fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
          textTransform:'uppercase', letterSpacing:'0.14em',
          display:'flex', alignItems:'baseline',
        }}>
          attributes
          <span style={{ flex:1 }} />
          <span style={{ color:'var(--ink2)', textTransform:'none', letterSpacing:0 }}>
            {entries.length} keys
          </span>
        </div>
        <div style={{ padding:'4px 10px 14px' }}>
          {entries.map(([k, v]) => (
            <div key={k} style={{
              display:'grid', gridTemplateColumns:'minmax(120px, 0.9fr) minmax(0, 1.1fr)',
              gap:8, padding:'5px 8px', borderRadius:5,
              alignItems:'baseline',
            }}>
              <div style={{
                fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
                overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
              }}>{k}</div>
              <div style={{
                fontFamily:'var(--mono)', fontSize:10.5,
                color: typeof v === 'boolean'
                  ? (v ? '#3e6a3e' : 'var(--ink2)')
                  : typeof v === 'number' ? 'var(--accentInk)' : 'var(--ink)',
                overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                background: k === 'error' && v ? 'color-mix(in oklch, var(--danger) 14%, transparent)' : 'transparent',
                padding:'1px 4px', borderRadius:3, justifySelf:'start',
              }} title={String(v)}>{
                typeof v === 'string' ? '"' + v + '"' : String(v)
              }</div>
            </div>
          ))}
        </div>
      </div>
    </aside>
  );
}

window.Spans = Spans;
