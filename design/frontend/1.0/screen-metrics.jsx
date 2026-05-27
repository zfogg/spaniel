// Spaniel — Metrics page (Drift direction).  Time-series view for OTel
// metrics: gauge / counter / histogram.  Picks a metric from the left
// sidebar (grouped by service), renders a chart sized to the metric kind,
// and overlays trace markers so you can see which run produced the spike.

const { useState: mState, useMemo: mMemo } = React;
const { Chrome, Sidebar, SbGroup, SbItem, SvcChip, Badge, Frame } = window.UI;

// ── Dataset ──────────────────────────────────────────────────────────────
// 60-point series (one per minute, last hour).  Each metric is shaped to be
// visually distinct: histogram has 3 percentile lines, counter is a
// monotonically-increasing curve we display as a rate, gauge is a wandering
// value.  All values are baked rather than randomised so the chart looks
// the same across reloads.
function gen(seed, base, amp, drift = 0, spike = null) {
  // Tiny deterministic prng.
  let x = seed;
  const rand = () => { x = (x * 9301 + 49297) % 233280; return x / 233280; };
  const out = [];
  for (let i = 0; i < 60; i++) {
    const t = i / 59;
    let v = base + Math.sin(t * Math.PI * 3 + seed) * amp * 0.7
                  + (rand() - 0.5) * amp * 0.4
                  + drift * t;
    if (spike && i >= spike.from && i <= spike.to) {
      v += spike.amount * Math.sin((i - spike.from) / (spike.to - spike.from) * Math.PI);
    }
    out.push(Math.max(0, v));
  }
  return out;
}

const METRICS = [
  // — api-gateway
  { id:'http_req_total', svc:'api-gateway',
    name:'http.server.request.count',
    desc:'Number of inbound HTTP requests received',
    unit:'req', type:'counter',
    series: gen(11, 320, 60, 80, { from:42, to:50, amount:120 }) },
  { id:'http_req_dur',   svc:'api-gateway',
    name:'http.server.duration',
    desc:'p50/p95/p99 wall-clock duration of inbound requests',
    unit:'ms', type:'histogram',
    p50: gen(20, 84,  10, 0),
    p95: gen(21, 320, 40, 0, { from:42, to:50, amount:280 }),
    p99: gen(22, 612, 90, 0, { from:42, to:50, amount:420 }) },
  { id:'gw_inflight',    svc:'api-gateway',
    name:'http.server.active_requests',
    desc:'Live count of in-flight requests',
    unit:'req', type:'gauge',
    series: gen(33, 24, 8) },

  // — cart
  { id:'cart_items',     svc:'cart-service',
    name:'cart.items.added',
    desc:'Items added to any cart',
    unit:'item', type:'counter',
    series: gen(44, 12, 4, 6) },
  { id:'cart_size',      svc:'cart-service',
    name:'cart.size.observed',
    desc:'Number of items in cart at time of write',
    unit:'item', type:'histogram',
    p50: gen(55, 3,  1, 0),
    p95: gen(56, 8,  2, 0),
    p99: gen(57, 14, 3, 0) },

  // — pricing
  { id:'pricing_cache_hit', svc:'pricing-service',
    name:'pricing.cache.hit_ratio',
    desc:'Fraction of price lookups served from cache',
    unit:'%', type:'gauge',
    series: gen(66, 0.62, 0.15, -0.1).map(v => Math.max(0, Math.min(1, v))) },
  { id:'pricing_quote_dur', svc:'pricing-service',
    name:'pricing.quote.duration',
    desc:'Wall-clock duration of pricing.Quote',
    unit:'ms', type:'histogram',
    p50: gen(70, 38, 6, 0, { from:42, to:50, amount:18 }),
    p95: gen(71, 82, 14, 0, { from:42, to:50, amount:42 }),
    p99: gen(72, 138, 24, 0, { from:42, to:50, amount:84 }) },
  { id:'pricing_dbcalls',   svc:'pricing-service',
    name:'pricing.db.queries',
    desc:'DB queries issued per pricing call (n+1 detector)',
    unit:'qry', type:'gauge',
    series: gen(73, 4.8, 1.4, 0, { from:42, to:50, amount:2.4 }) },

  // — payment
  { id:'pay_charges',       svc:'payment-service',
    name:'payment.charges.total',
    desc:'Successful charges processed',
    unit:'charge', type:'counter',
    series: gen(88, 6, 2, 4) },
  { id:'pay_amount',        svc:'payment-service',
    name:'payment.amount.usd',
    desc:'Total amount charged in USD cents',
    unit:'$', type:'counter',
    series: gen(89, 18000, 6400, 24000) },
  { id:'pay_stripe_dur',    svc:'payment-service',
    name:'payment.stripe.duration',
    desc:'Round-trip to Stripe',
    unit:'ms', type:'histogram',
    p50: gen(90, 180, 30, 0),
    p95: gen(91, 308, 50, 0, { from:42, to:50, amount:60 }),
    p99: gen(92, 480, 80, 0, { from:42, to:50, amount:140 }) },

  // — db
  { id:'pg_pool_in_use',    svc:'postgres',
    name:'pg.pool.in_use',
    desc:'Connections currently leased from the pool',
    unit:'conn', type:'gauge',
    series: gen(101, 8, 4, 0, { from:42, to:50, amount:8 }) },
  { id:'pg_query_dur',      svc:'postgres',
    name:'pg.query.duration',
    desc:'Wall-clock duration of every SQL statement',
    unit:'ms', type:'histogram',
    p50: gen(110, 4, 1, 0),
    p95: gen(111, 18, 4, 0),
    p99: gen(112, 42, 8, 0, { from:42, to:50, amount:18 }) },

  // — redis
  { id:'redis_hit_ratio',   svc:'redis',
    name:'redis.cache.hit_ratio',
    desc:'Fraction of GETs that hit',
    unit:'%', type:'gauge',
    series: gen(120, 0.78, 0.06, 0).map(v => Math.max(0, Math.min(1, v))) },
];

// Trace overlay markers — pinned to the same x-axis as the chart.  These
// are placed at indices where the example trace ran, so the user can SEE
// the correlation.
const TRACE_MARKERS = [
  { i: 12, id:'7b2a…84c19', op:'GET /api/products', tone:'neutral' },
  { i: 28, id:'a18c…2204e', op:'GET /api/cart',     tone:'neutral' },
  { i: 44, id:'9ef3…d19eb', op:'POST /api/checkout', tone:'danger' },
  { i: 48, id:'c041…77b3d', op:'POST /api/webhook', tone:'danger' },
];

const METRIC_GROUPS = (() => {
  const m = {};
  for (const M of METRICS) (m[M.svc] = m[M.svc] || []).push(M);
  return m;
})();

// ── Atoms ────────────────────────────────────────────────────────────────
function MtIcon({ type }) {
  if (type === 'gauge') {
    return (
      <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
        <path d="M2 10a5 5 0 0 1 10 0" stroke="currentColor" strokeWidth="1.4"
          strokeLinecap="round" fill="none" />
        <line x1="7" y1="10" x2="10" y2="6" stroke="currentColor" strokeWidth="1.4"
          strokeLinecap="round" />
        <circle cx="7" cy="10" r="1" fill="currentColor" />
      </svg>
    );
  }
  if (type === 'counter') {
    return (
      <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
        <path d="M2 11 L5 7 L8 9 L12 3" stroke="currentColor" strokeWidth="1.4"
          fill="none" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M9 3h3v3" stroke="currentColor" strokeWidth="1.4" fill="none"
          strokeLinecap="round" />
      </svg>
    );
  }
  // histogram
  return (
    <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
      <rect x="2" y="8"  width="2" height="4" fill="currentColor" opacity="0.45" />
      <rect x="5" y="5"  width="2" height="7" fill="currentColor" opacity="0.7" />
      <rect x="8" y="3"  width="2" height="9" fill="currentColor" opacity="0.9" />
      <rect x="11" y="6" width="2" height="6" fill="currentColor" opacity="0.6" />
    </svg>
  );
}

function MetricKindTag({ type }) {
  const tone =
    type === 'gauge' ? { fg:'#356a99', bd:'#7aa3c5', bg:'#dfe7ef' } :
    type === 'counter' ? { fg:'#3e6a3e', bd:'#88b29a', bg:'#dee9de' } :
    { fg:'#7a3a23', bd:'#c89a86', bg:'#ecd9cf' };
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:4,
      padding:'1px 7px', borderRadius:5,
      fontFamily:'var(--mono)', fontSize:9, fontWeight:700,
      color:tone.fg, background:tone.bg, border:`1px solid ${tone.bd}`,
      letterSpacing:'0.08em', textTransform:'uppercase',
    }}>
      <MtIcon type={type} />
      {type}
    </span>
  );
}

// ── Sparkline cells in the left list ────────────────────────────────────
function Spark({ series, color, h = 22 }) {
  const w = 60, n = series.length;
  const min = Math.min(...series), max = Math.max(...series);
  const span = max - min || 1;
  const pts = series.map((v, i) =>
    `${(i / (n - 1)) * w},${h - ((v - min) / span) * (h - 2) - 1}`).join(' ');
  return (
    <svg width={w} height={h} style={{ flex:'0 0 auto', overflow:'visible' }}>
      <polyline points={pts} fill="none" stroke={color} strokeWidth="1.3"
        strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

// ── Main chart ──────────────────────────────────────────────────────────
function Chart({ metric, theme, hoverI, onHoverI, traces }) {
  const W = 920, H = 280, P = { l: 56, r: 16, t: 24, b: 36 };
  const cw = W - P.l - P.r, ch = H - P.t - P.b;
  const series = metric.type === 'histogram'
    ? [metric.p50, metric.p95, metric.p99]
    : [metric.series];
  const n = series[0].length;
  const all = series.flat();
  const min = Math.min(0, Math.min(...all));
  const max = Math.max(...all) * 1.08;
  const span = max - min || 1;

  const xAt = (i) => P.l + (i / (n - 1)) * cw;
  const yAt = (v) => P.t + ch - ((v - min) / span) * ch;

  function fmt(v) {
    if (metric.unit === '%') return Math.round(v * 100) + '%';
    if (metric.unit === '$') return '$' + Math.round(v).toLocaleString();
    if (Math.abs(v) >= 1000) return (v / 1000).toFixed(1) + 'k';
    if (Math.abs(v) >= 100)  return v.toFixed(0);
    if (Math.abs(v) >= 10)   return v.toFixed(1);
    return v.toFixed(2);
  }
  function timeLabel(i) {
    const ago = 59 - i;
    if (ago === 0) return 'now';
    if (ago % 15 === 0) return `−${ago}m`;
    return null;
  }

  // y-axis ticks (5 nice steps)
  const ticks = [];
  for (let i = 0; i <= 4; i++) ticks.push(min + (span / 4) * i);

  const svcColor = theme.svc[metric.svc] || {};
  const COLORS = metric.type === 'histogram'
    ? ['#7aa3c5', svcColor.fg || 'var(--accent)', '#9a3b3b']
    : [svcColor.fg || 'var(--accent)'];
  const LABELS = metric.type === 'histogram' ? ['p50', 'p95', 'p99'] : ['value'];

  // Path builders
  function linePath(s) {
    return s.map((v, i) => `${i === 0 ? 'M' : 'L'} ${xAt(i)} ${yAt(v)}`).join(' ');
  }
  function areaPath(s) {
    return linePath(s) + ` L ${xAt(n - 1)} ${yAt(min)} L ${xAt(0)} ${yAt(min)} Z`;
  }

  return (
    <div style={{ position:'relative' }}>
      <svg viewBox={`0 0 ${W} ${H}`} width="100%" height={H}
        style={{ display:'block', cursor:'crosshair' }}
        onMouseLeave={() => onHoverI(null)}
        onMouseMove={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          const px = ((e.clientX - rect.left) / rect.width) * W;
          const ratio = Math.max(0, Math.min(1, (px - P.l) / cw));
          onHoverI(Math.round(ratio * (n - 1)));
        }}>
        {/* gridlines + y-axis labels */}
        {ticks.map((v, i) => (
          <g key={i}>
            <line x1={P.l} x2={W - P.r} y1={yAt(v)} y2={yAt(v)}
              stroke="var(--line2)" strokeWidth="1"
              strokeDasharray={i === 0 ? '0' : '2 4'} />
            <text x={P.l - 8} y={yAt(v) + 3} textAnchor="end"
              fontFamily="var(--mono)" fontSize="10" fill="var(--ink3)">{fmt(v)}</text>
          </g>
        ))}
        {/* x-axis labels */}
        {Array.from({ length: n }).map((_, i) => {
          const label = timeLabel(i);
          if (!label) return null;
          return (
            <text key={i} x={xAt(i)} y={H - P.b + 16} textAnchor="middle"
              fontFamily="var(--mono)" fontSize="10" fill="var(--ink3)">{label}</text>
          );
        })}

        {/* trace markers — vertical dotted lines + flag */}
        {traces.map((m, i) => (
          <g key={i}>
            <line x1={xAt(m.i)} x2={xAt(m.i)} y1={P.t} y2={H - P.b}
              stroke={m.tone === 'danger' ? 'var(--danger)' : 'var(--ink3)'}
              strokeWidth="1" strokeDasharray="2 3" opacity="0.5" />
            <g transform={`translate(${xAt(m.i)}, ${P.t - 8})`}>
              <circle r="3.5" fill={m.tone === 'danger' ? 'var(--danger)' : 'var(--ink3)'} />
            </g>
          </g>
        ))}

        {/* series */}
        {series.length === 1 ? (
          <g>
            <path d={areaPath(series[0])}
              fill={COLORS[0]} opacity="0.12" />
            <path d={linePath(series[0])} fill="none"
              stroke={COLORS[0]} strokeWidth="2"
              strokeLinejoin="round" strokeLinecap="round" />
          </g>
        ) : (
          series.map((s, i) => (
            <path key={i} d={linePath(s)} fill="none"
              stroke={COLORS[i]}
              strokeWidth={i === series.length - 1 ? 2.4 : 1.6}
              opacity={i === 0 ? 0.7 : 1}
              strokeLinejoin="round" strokeLinecap="round" />
          ))
        )}

        {/* hover cursor */}
        {hoverI != null ? (
          <g>
            <line x1={xAt(hoverI)} x2={xAt(hoverI)} y1={P.t} y2={H - P.b}
              stroke="var(--ink2)" strokeWidth="1" />
            {series.map((s, i) => (
              <circle key={i} cx={xAt(hoverI)} cy={yAt(s[hoverI])} r="3.5"
                fill="var(--surface)" stroke={COLORS[i]} strokeWidth="2" />
            ))}
          </g>
        ) : null}

        {/* axis box */}
        <line x1={P.l} y1={P.t} x2={P.l} y2={H - P.b}
          stroke="var(--line)" strokeWidth="1" />
      </svg>

      {/* hover tooltip */}
      {hoverI != null ? (
        <div style={{
          position:'absolute',
          left: `calc(${(xAt(hoverI) / W) * 100}% + 14px)`,
          top: 30,
          background:'var(--ink)', color:'var(--bg)',
          fontFamily:'var(--mono)', fontSize:11,
          padding:'8px 10px', borderRadius:6, minWidth:160,
          boxShadow:'0 8px 18px -8px rgba(0,0,0,.32)', pointerEvents:'none',
        }}>
          <div style={{ fontSize:9, opacity:0.6, textTransform:'uppercase',
            letterSpacing:'0.14em', marginBottom:4 }}>
            t = {hoverI === 59 ? 'now' : `−${59 - hoverI}m`}
          </div>
          {series.map((s, i) => (
            <div key={i} style={{ display:'flex', gap:8, alignItems:'baseline',
              padding:'1px 0' }}>
              <span style={{ width:8, height:8, borderRadius:8, background:COLORS[i],
                display:'inline-block' }} />
              <span style={{ flex:1, opacity:0.7 }}>{LABELS[i]}</span>
              <span style={{ fontWeight:700 }}>{fmt(s[hoverI])}{metric.unit && metric.unit.length < 3 ? '' : ` ${metric.unit}`}</span>
            </div>
          ))}
        </div>
      ) : null}

      {/* Legend */}
      <div style={{
        display:'flex', alignItems:'center', gap:18,
        padding:'10px 16px 0', flexWrap:'wrap',
      }}>
        {LABELS.map((L, i) => (
          <span key={i} style={{
            display:'inline-flex', alignItems:'center', gap:6,
            fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
          }}>
            <span style={{
              width:18, height:2.4, background:COLORS[i],
            }} />
            {L} {metric.type === 'histogram' && i === 1 ? '(median latency)' : ''}
          </span>
        ))}
        <span style={{ flex:1 }} />
        <span style={{ display:'inline-flex', alignItems:'center', gap:6,
          fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)' }}>
          <span style={{ width:8, height:8, borderRadius:8, background:'var(--ink3)' }} />
          observed trace within window
        </span>
      </div>
    </div>
  );
}

// ── Stat strip ──────────────────────────────────────────────────────────
function Stat({ label, value, sub, tone }) {
  return (
    <div style={{ padding:'14px 18px', borderRight:'1px solid var(--line)' }}>
      <div style={{
        fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em',
      }}>{label}</div>
      <div style={{
        fontFamily:'var(--serif)', fontSize:26, fontWeight:600, lineHeight:1.05,
        marginTop:4,
        color: tone === 'danger' ? 'var(--dangerInk)' : tone === 'ok' ? '#3e6a3e' : 'var(--ink)',
      }}>{value}</div>
      {sub ? <div style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', marginTop:4,
      }}>{sub}</div> : null}
    </div>
  );
}

function statsFor(m) {
  if (m.type === 'gauge') {
    const last = m.series[m.series.length - 1];
    const prev = m.series[m.series.length - 6];
    const delta = last - prev;
    const min = Math.min(...m.series), max = Math.max(...m.series);
    return [
      { label:'now',   value: m.unit === '%' ? Math.round(last * 100) + '%' : last.toFixed(2),
        sub:'latest sample' },
      { label:'5m ago', value: m.unit === '%' ? Math.round(prev * 100) + '%' : prev.toFixed(2),
        sub: (delta >= 0 ? '+' : '') + delta.toFixed(2) + ' vs now',
        tone: delta < 0 ? 'danger' : 'ok' },
      { label:'min',   value: m.unit === '%' ? Math.round(min * 100) + '%' : min.toFixed(2), sub:'last hour' },
      { label:'max',   value: m.unit === '%' ? Math.round(max * 100) + '%' : max.toFixed(2), sub:'last hour' },
    ];
  }
  if (m.type === 'counter') {
    const total = m.series.reduce((a, b) => a + b, 0);
    const last  = m.series[m.series.length - 1];
    const rate  = last;
    const peakIdx = m.series.indexOf(Math.max(...m.series));
    return [
      { label:'rate / min', value: rate >= 1000 ? (rate / 1000).toFixed(2) + 'k' : rate.toFixed(1), sub:'last sample' },
      { label:'total',      value: total >= 1000 ? (total / 1000).toFixed(1) + 'k' : total.toFixed(0), sub:'last hour' },
      { label:'peak',       value: m.series[peakIdx].toFixed(1), sub:`at −${59 - peakIdx}m` },
      { label:'unit',       value: m.unit, sub:'cumulative · resets on restart' },
    ];
  }
  // histogram
  return [
    { label:'p50', value: m.p50[m.p50.length - 1].toFixed(0) + 'ms', sub:'latest' },
    { label:'p95', value: m.p95[m.p95.length - 1].toFixed(0) + 'ms', sub:'latest',
      tone: m.p95[m.p95.length - 1] > 300 ? 'danger' : null },
    { label:'p99', value: m.p99[m.p99.length - 1].toFixed(0) + 'ms', sub:'latest',
      tone: m.p99[m.p99.length - 1] > 600 ? 'danger' : null },
    { label:'max p99', value: Math.max(...m.p99).toFixed(0) + 'ms',
      sub:`at −${59 - m.p99.indexOf(Math.max(...m.p99))}m` },
  ];
}

// ── Page root ───────────────────────────────────────────────────────────
function Metrics({ theme }) {
  const [selected, setSelected] = mState('pricing_quote_dur');
  const [query,    setQuery]    = mState('');
  const [hoverI,   setHoverI]   = mState(null);
  const [typeSel,  setTypeSel]  = mState(null);

  const filtered = mMemo(() => {
    const q = query.trim().toLowerCase();
    return METRICS.filter(m => {
      if (typeSel && m.type !== typeSel) return false;
      if (!q) return true;
      return (m.name + ' ' + m.svc + ' ' + (m.desc || '')).toLowerCase().includes(q);
    });
  }, [query, typeSel]);

  const groups = mMemo(() => {
    const m = {};
    for (const M of filtered) (m[M.svc] = m[M.svc] || []).push(M);
    return m;
  }, [filtered]);

  const cur = METRICS.find(m => m.id === selected) || METRICS[0];
  const stats = statsFor(cur);
  const sampleSeries = cur.type === 'histogram' ? cur.p95 : cur.series;

  return (
    <Frame>
      <Chrome active="metrics" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        {/* Left rail — full metric catalog */}
        <div style={{
          width:320, borderRight:'1px solid var(--line)',
          background:'var(--surface)',
          display:'flex', flexDirection:'column', overflow:'hidden',
        }}>
          <div style={{
            padding:'10px 12px', borderBottom:'1px solid var(--line)',
            display:'flex', flexDirection:'column', gap:8,
          }}>
            <span style={{
              display:'inline-flex', alignItems:'center', gap:7,
              background:'var(--surface2)', border:'1px solid var(--line)',
              borderRadius:6, padding:'0 10px', height:28,
            }}>
              <svg width="12" height="12" viewBox="0 0 14 14" fill="none">
                <circle cx="6" cy="6" r="4" stroke="var(--ink3)" strokeWidth="1.4" />
                <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="var(--ink3)"
                  strokeWidth="1.4" strokeLinecap="round" />
              </svg>
              <input value={query} onChange={(e) => setQuery(e.target.value)}
                placeholder="search metrics…"
                style={{
                  flex:1, border:'none', outline:'none', background:'transparent',
                  fontFamily:'var(--mono)', fontSize:11.5, color:'var(--ink)',
                }} />
            </span>
            <div style={{ display:'flex', gap:6, flexWrap:'wrap' }}>
              {['gauge', 'counter', 'histogram'].map(t => {
                const on = typeSel === t;
                return (
                  <button key={t} type="button"
                    onClick={() => setTypeSel(on ? null : t)}
                    style={{
                      cursor:'pointer', outline:'none',
                      padding:'3px 9px', borderRadius:5,
                      fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
                      letterSpacing:'0.06em', textTransform:'uppercase',
                      background: on ? 'color-mix(in oklch, var(--accent) 18%, var(--surface))' : 'var(--surface2)',
                      color: on ? 'var(--accentInk)' : 'var(--ink2)',
                      border: '1px solid ' + (on ? 'var(--accent)' : 'var(--line)'),
                      display:'inline-flex', alignItems:'center', gap:5,
                    }}>
                    <MtIcon type={t} />{t}
                  </button>
                );
              })}
              <span style={{ flex:1 }} />
              <span style={{
                fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
                alignSelf:'center',
              }}>{filtered.length} metrics</span>
            </div>
          </div>

          <div style={{ flex:1, overflow:'hidden auto' }}>
            {Object.entries(groups).map(([svc, list]) => {
              const c = theme.svc[svc] || {};
              return (
                <div key={svc}>
                  <div style={{
                    padding:'10px 12px 4px',
                    fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
                    textTransform:'uppercase', letterSpacing:'0.14em',
                    background:'var(--surface)', position:'sticky', top:0,
                    display:'flex', alignItems:'center', gap:6,
                  }}>
                    <span style={{ width:6, height:6, borderRadius:6, background:c.fg }} />
                    {svc}
                    <span style={{ flex:1 }} />
                    <span style={{ color:'var(--ink2)' }}>{list.length}</span>
                  </div>
                  {list.map(M => {
                    const isSel = M.id === selected;
                    const sparkS = M.type === 'histogram' ? M.p95 : M.series;
                    return (
                      <button key={M.id} type="button" onClick={() => setSelected(M.id)}
                        style={{
                          width:'100%', padding:'8px 12px',
                          border:'none', cursor:'pointer', outline:'none',
                          textAlign:'left',
                          background: isSel
                            ? 'color-mix(in oklch, var(--accent) 14%, var(--surface))'
                            : 'transparent',
                          borderLeft: isSel ? '2px solid var(--accent)' : '2px solid transparent',
                          borderBottom:'1px solid var(--line2)',
                          display:'grid', gridTemplateColumns:'minmax(0,1fr) 60px',
                          gap:10, alignItems:'center',
                        }}>
                        <div style={{ minWidth:0 }}>
                          <div style={{
                            fontFamily:'var(--mono)', fontSize:11, fontWeight:600,
                            color:'var(--ink)',
                            overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                          }}>{M.name}</div>
                          <div style={{ marginTop:3 }}>
                            <MetricKindTag type={M.type} />
                          </div>
                        </div>
                        <Spark series={sparkS} color={c.fg || 'var(--accent)'} />
                      </button>
                    );
                  })}
                </div>
              );
            })}
          </div>

          <div style={{
            padding:'8px 12px', borderTop:'1px solid var(--line)',
            background:'var(--surface2)',
            fontFamily:'var(--mono)', fontSize:10, color:'var(--ink2)',
            display:'flex', gap:10, alignItems:'center',
          }}>
            <span style={{ width:6, height:6, borderRadius:6, background:'var(--ok)',
              boxShadow:'0 0 0 3px color-mix(in oklch, var(--ok) 30%, transparent)' }} />
            <span>otlp/metrics · 18 streams · last 1h</span>
          </div>
        </div>

        {/* Main panel */}
        <div style={{ flex:1, overflow:'hidden auto',
          display:'flex', flexDirection:'column', background:'var(--surface)' }}>
          {/* Header */}
          <div style={{
            padding:'18px 24px 14px', borderBottom:'1px solid var(--line)',
            display:'flex', alignItems:'flex-start', gap:14,
          }}>
            <div style={{ flex:1, minWidth:0 }}>
              <div style={{ display:'flex', alignItems:'center', gap:10 }}>
                <SvcChip svc={cur.svc} theme={theme} size="md" />
                <MetricKindTag type={cur.type} />
                <span style={{
                  padding:'2px 7px', borderRadius:5,
                  background:'var(--surface2)', color:'var(--ink2)',
                  border:'1px solid var(--line)',
                  fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
                }}>unit · {cur.unit}</span>
              </div>
              <h1 style={{
                margin:'8px 0 4px', fontFamily:'var(--mono)', fontSize:20, fontWeight:700,
                letterSpacing:'-0.01em', color:'var(--ink)', wordBreak:'break-all',
              }}>{cur.name}</h1>
              <div style={{
                fontFamily:'var(--serif)', fontStyle:'italic', fontSize:13,
                color:'var(--ink2)', maxWidth:720,
              }}>{cur.desc}</div>
            </div>

            {/* Range picker */}
            <div style={{
              display:'inline-flex', alignItems:'center', gap:2,
              background:'var(--surface2)', borderRadius:8,
              padding:3, border:'1px solid var(--line)',
            }}>
              {['15m', '1h', '6h', '24h'].map(r => (
                <button key={r} type="button"
                  style={{
                    padding:'4px 10px', borderRadius:6,
                    background: r === '1h' ? 'var(--surface)' : 'transparent',
                    color: r === '1h' ? 'var(--ink)' : 'var(--ink2)',
                    border: r === '1h' ? '1px solid var(--line)' : '1px solid transparent',
                    fontFamily:'var(--mono)', fontSize:11, fontWeight: r === '1h' ? 700 : 500,
                    cursor:'pointer', outline:'none',
                  }}>{r}</button>
              ))}
            </div>
          </div>

          {/* Stat strip */}
          <div style={{
            display:'grid', gridTemplateColumns:'repeat(4, 1fr)',
            background:'var(--surface)', borderBottom:'1px solid var(--line)',
          }}>
            {stats.map((s, i) => <Stat key={i} {...s} />)}
          </div>

          {/* Chart */}
          <div style={{ padding:'18px 16px 22px' }}>
            <Chart metric={cur} theme={theme}
              hoverI={hoverI} onHoverI={setHoverI}
              traces={TRACE_MARKERS} />
          </div>

          {/* Correlated traces */}
          <div style={{ padding:'4px 24px 22px' }}>
            <div style={{
              display:'flex', alignItems:'baseline', gap:10, marginBottom:8,
            }}>
              <h2 style={{
                margin:0, fontFamily:'var(--serif)', fontSize:15, fontWeight:600, color:'var(--ink)',
              }}>Traces during this window</h2>
              <span style={{
                fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)',
              }}>service.name = {cur.svc} · session = feat/checkout</span>
            </div>
            <div style={{
              background:'var(--surface)', border:'1px solid var(--line)', borderRadius:10,
              overflow:'hidden',
            }}>
              {TRACE_MARKERS.map((m, i) => (
                <div key={i} style={{
                  display:'grid',
                  gridTemplateColumns:'18px 130px 1fr 120px 80px 80px',
                  gap:10, padding:'10px 14px', alignItems:'center',
                  borderBottom: i < TRACE_MARKERS.length - 1 ? '1px solid var(--line2)' : 'none',
                  background: m.tone === 'danger'
                    ? 'color-mix(in oklch, var(--danger) 7%, var(--surface))'
                    : 'transparent',
                }}>
                  <span style={{
                    width:8, height:8, borderRadius:8,
                    background: m.tone === 'danger' ? 'var(--danger)' : 'var(--ink2)',
                  }} />
                  <span style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)' }}>
                    −{59 - m.i}m
                  </span>
                  <span style={{
                    fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)', fontWeight:600,
                    overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                  }}>{m.op}</span>
                  <span style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)' }}>
                    {m.id}
                  </span>
                  <span style={{
                    fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
                    textAlign:'right',
                  }}>
                    {cur.type === 'histogram'
                      ? cur.p95[m.i].toFixed(0) + 'ms p95'
                      : cur.type === 'gauge'
                        ? (cur.unit === '%' ? Math.round(cur.series[m.i]*100) + '%' : cur.series[m.i].toFixed(1) + ' ' + cur.unit)
                        : cur.series[m.i].toFixed(0) + ' ' + cur.unit + '/min'}
                  </span>
                  <button type="button" style={{
                    height:24, padding:'0 8px', borderRadius:5, cursor:'pointer',
                    background:'var(--surface2)', border:'1px solid var(--line)',
                    color:'var(--ink)', fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
                    outline:'none',
                  }}>open →</button>
                </div>
              ))}
            </div>
            <div style={{
              padding:'10px 0 0',
              fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)',
            }}>
              correlated by timestamp · same service.name as the metric · same active session
            </div>
          </div>

          <div style={{ height:18 }} />
        </div>
      </div>
    </Frame>
  );
}

window.Metrics = Metrics;
