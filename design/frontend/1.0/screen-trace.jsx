// Spaniel — Drift trace detail screen.  Hero screen for the Drift direction
// with an interactive flame-graph view.  Stateful: tab toggle between
// Waterfall and Flame, clickable spans in both views, click-to-zoom on the
// flame graph, hover tooltip, reset-zoom breadcrumb.

const { useState, useRef, useCallback } = React;
const { Chrome, SvcChip, Badge, PanelHead, Frame, fmtMs } = window.UI;

// ── A single waterfall row ──────────────────────────────────────────────
function SpanRow({ span, traceDur, theme, selected, hovered, onSelect, onHover }) {
  const svc = (theme.svc && theme.svc[span.svc]) || { fg:'var(--ink2)', bg:'var(--chipBg)' };
  const left  = (span.start / traceDur) * 100;
  const width = Math.max(0.6, (span.dur / traceDur) * 100);
  const tagTone = span.tag === 'n+1' ? 'danger' : span.tag === 'slow' ? 'warn' : span.tag === 'lint' ? 'warn' : 'neutral';

  return (
    <div
      onClick={() => onSelect(span.id)}
      onMouseEnter={() => onHover(span.id)}
      onMouseLeave={() => onHover(null)}
      style={{
        display:'grid', gridTemplateColumns:'minmax(0, 320px) 1fr 56px',
        alignItems:'center', padding:'4px 0', cursor:'pointer',
        background: selected
          ? 'color-mix(in oklch, var(--accent) 14%, var(--surface))'
          : hovered ? 'color-mix(in oklch, var(--accent) 6%, var(--surface))' : 'transparent',
        borderLeft: selected ? '2px solid var(--accent)' : '2px solid transparent',
        borderBottom:'1px solid var(--line2)',
        transition:'background .08s',
      }}
    >
      <div style={{
        paddingLeft: 14 + span.depth * 16,
        display:'flex', alignItems:'center', gap:7, minWidth:0,
      }}>
        {span.depth > 0 ? <span style={{
          width:8, height:1, background:'var(--line)', flex:'0 0 auto', marginLeft:-7,
        }} /> : null}
        <span style={{
          width:9, height:9, borderRadius:2,
          background: svc.fg, opacity:0.85, flex:'0 0 auto',
        }} />
        <span style={{
          fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)',
          overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
          flex:'1 1 auto', minWidth:0,
        }}>{span.name}</span>
        {span.tag ? <Badge tone={tagTone}>{span.tag}</Badge> : null}
      </div>
      <div style={{ position:'relative', height:18, marginLeft:8, marginRight:8 }}>
        <div style={{ position:'absolute', inset:0, display:'flex' }}>
          {[0,1,2,3].map(i => <div key={i} style={{
            flex:1, borderLeft: i === 0 ? 'none' : '1px dashed var(--line2)',
          }} />)}
        </div>
        <div style={{
          position:'absolute', top:4, height:10,
          left: left + '%', width: width + '%',
          background: svc.bg, borderRadius:3,
          boxShadow: `inset 0 0 0 1px ${svc.fg}30, inset 2px 0 0 ${svc.fg}`,
        }} />
        {(span.tag === 'slow' || span.tag === 'n+1') ? <div style={{
          position:'absolute', top:1, height:16,
          left: left + '%', width: width + '%',
          borderRadius:3, border:'1px solid var(--danger)',
          boxShadow:'0 0 0 2px color-mix(in oklch, var(--danger) 18%, transparent)',
          pointerEvents:'none',
        }} /> : null}
      </div>
      <div style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink2)',
        textAlign:'right', paddingRight:10,
      }}>{fmtMs(span.dur)}</div>
    </div>
  );
}

// ── Time ruler above waterfall ──────────────────────────────────────────
function Ruler({ traceDur }) {
  const steps = 6;
  return (
    <div style={{
      display:'grid', gridTemplateColumns:'minmax(0, 320px) 1fr 56px',
      alignItems:'center', borderBottom:'1px solid var(--line)',
      background:'var(--surface2)',
    }}>
      <div style={{
        padding:'7px 14px', fontFamily:'var(--mono)', fontSize:9,
        textTransform:'uppercase', letterSpacing:'0.14em', color:'var(--ink3)',
      }}>span · {window.SPANIEL.SPANS.length} total</div>
      <div style={{ position:'relative', height:24, marginLeft:8, marginRight:8,
        borderLeft:'1px solid var(--line)' }}>
        {Array.from({length: steps + 1}).map((_,i) => (
          <span key={i} style={{
            position:'absolute', left: (i/steps)*100 + '%', top:0, height:'100%',
            borderLeft: i === 0 ? 'none' : '1px solid var(--line2)',
            transform:'translateX(-1px)',
            fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            padding:'7px 4px', whiteSpace:'nowrap',
          }}>{fmtMs((traceDur / steps) * i)}</span>
        ))}
      </div>
      <div style={{
        padding:'7px 10px 7px 0', textAlign:'right',
        fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.14em',
      }}>dur</div>
    </div>
  );
}

// ── Mini timeline shown above the body — shows zoom region ──────────────
function MiniTimeline({ spans, traceDur, theme, zoom }) {
  const [zStart, zEnd] = zoom || [0, traceDur];
  const zLeft  = (zStart / traceDur) * 100;
  const zWidth = ((zEnd - zStart) / traceDur) * 100;
  return (
    <div style={{
      height:54, padding:'10px 16px',
      background:'var(--surface2)', borderBottom:'1px solid var(--line)',
      position:'relative',
    }}>
      <div style={{
        position:'absolute', left:16, right:16, top:14, bottom:10,
        background:'var(--surface3)', borderRadius:3, overflow:'hidden',
      }}>
        {spans.map(s => {
          const svc = (theme.svc && theme.svc[s.svc]) || { bg:'var(--chipBg)' };
          return (
            <div key={s.id} style={{
              position:'absolute',
              left:  (s.start/traceDur)*100 + '%',
              width: Math.max(0.3,(s.dur/traceDur)*100) + '%',
              top: s.depth * 4, height: 3,
              background: svc.bg,
            }} />
          );
        })}
        <div style={{
          position:'absolute', left: zLeft + '%', width: zWidth + '%',
          top:0, bottom:0,
          border:'1px solid var(--accent)', borderRadius:3,
          boxShadow:'0 0 0 3px color-mix(in oklch, var(--accent) 18%, transparent)',
          background:'color-mix(in oklch, var(--accent) 6%, transparent)',
          pointerEvents:'none',
        }} />
      </div>
    </div>
  );
}

// ── Flame graph view ─────────────────────────────────────────────────────
// Layout: each depth is one row, span widths scale by duration in the
// current zoom window.  Click a span to select & zoom into it; click empty
// space to clear hover; the reset-zoom breadcrumb is rendered by the parent.
function FlameView({ spans, traceDur, theme, selectedId, hoveredId, onSelect,
                     onHover, zoom, onZoom }) {
  const [zStart, zEnd] = zoom;
  const zDur = zEnd - zStart;
  const maxDepth = Math.max(...spans.map(s => s.depth));
  const rowH = 26;
  const totalH = (maxDepth + 1) * rowH;

  // Tick positions in current zoom window
  const tickStep = (() => {
    if (zDur > 400) return 100;
    if (zDur > 200) return 50;
    if (zDur > 80)  return 20;
    if (zDur > 20)  return 10;
    return 5;
  })();
  const tickStart = Math.ceil(zStart / tickStep) * tickStep;
  const ticks = [];
  for (let t = tickStart; t <= zEnd; t += tickStep) ticks.push(t);

  // Filter spans that intersect the visible window
  const visible = spans.filter(s => s.start + s.dur >= zStart && s.start <= zEnd);

  return (
    <div style={{ flex:1, overflow:'hidden', background:'var(--surface)',
      display:'flex', flexDirection:'column', position:'relative' }}>
      {/* Top ticks */}
      <div style={{
        position:'relative', height:24, borderBottom:'1px solid var(--line)',
        background:'var(--surface2)',
      }}>
        {ticks.map(t => {
          const left = ((t - zStart) / zDur) * 100;
          return (
            <span key={t} style={{
              position:'absolute', left: `${left}%`, top:0, bottom:0,
              borderLeft:'1px solid var(--line2)',
              fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              padding:'7px 4px', whiteSpace:'nowrap',
              transform:'translateX(-1px)',
            }}>{fmtMs(t)}</span>
          );
        })}
      </div>

      {/* Flame canvas */}
      <div
        onMouseLeave={() => onHover(null)}
        style={{
          position:'relative', flex:1, overflow:'hidden',
          minHeight: totalH + 12, padding:'6px 0',
          backgroundImage:`repeating-linear-gradient(
            0deg, transparent 0, transparent ${rowH - 1}px,
            var(--line2) ${rowH - 1}px, var(--line2) ${rowH}px)`,
          backgroundPosition:'0 6px',
        }}
      >
        {visible.map(s => {
          const svc = (theme.svc && theme.svc[s.svc]) || { fg:'var(--ink2)', bg:'var(--chipBg)' };
          // Clip to visible window
          const start = Math.max(s.start, zStart);
          const end = Math.min(s.start + s.dur, zEnd);
          const left = ((start - zStart) / zDur) * 100;
          const width = Math.max(0.2, ((end - start) / zDur) * 100);
          const top = 6 + s.depth * rowH;
          const isSel = s.id === selectedId;
          const isHov = s.id === hoveredId;
          const hot = s.tag === 'n+1' || s.tag === 'slow';
          // Show label if width > ~6%
          const showLabel = width > 6;
          return (
            <button
              key={s.id}
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onSelect(s.id);
                // Click-to-zoom: zoom into this span's time window
                onZoom([s.start, s.start + s.dur]);
              }}
              onMouseEnter={() => onHover(s.id)}
              style={{
                position:'absolute',
                left: `${left}%`, width: `${width}%`,
                top, height: rowH - 4,
                background: isSel || isHov
                  ? `color-mix(in oklch, ${svc.fg} 35%, ${svc.bg})`
                  : svc.bg,
                color: svc.fg,
                border: hot
                  ? '1px solid var(--danger)'
                  : isSel
                    ? `1px solid ${svc.fg}`
                    : '1px solid transparent',
                borderRadius: 3,
                padding:'0 6px',
                fontFamily:'var(--mono)', fontSize: 10.5, fontWeight: 600,
                textAlign:'left', cursor:'pointer',
                overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                boxShadow: isSel ? `inset 0 0 0 1px ${svc.fg}` : 'none',
                transition:'background .08s, transform .08s',
                transform: isHov ? 'translateY(-1px)' : 'none',
                zIndex: isHov || isSel ? 2 : 1,
                outline:'none',
              }}
              title={`${s.name}  ·  ${fmtMs(s.dur)}`}
            >
              {showLabel ? (
                <>
                  <span style={{ opacity:0.7 }}>{s.svc.replace(/-service$/,'')}</span>
                  {' · '}
                  <span>{s.name}</span>
                </>
              ) : null}
            </button>
          );
        })}

        {/* Hover tooltip */}
        {hoveredId ? (() => {
          const h = spans.find(s => s.id === hoveredId);
          if (!h) return null;
          const left = ((h.start - zStart) / zDur) * 100;
          const top = 6 + h.depth * rowH + (rowH - 4) + 4;
          const svc = theme.svc[h.svc] || {};
          return (
            <div style={{
              position:'absolute', left:`max(8px, min(${left}%, calc(100% - 240px)))`,
              top, zIndex:5,
              background:'var(--ink)', color:'var(--bg)',
              fontFamily:'var(--mono)', fontSize:11,
              padding:'8px 10px', borderRadius:6,
              minWidth:220, maxWidth:280,
              boxShadow:'0 6px 18px -6px rgba(0,0,0,.3)',
              pointerEvents:'none',
            }}>
              <div style={{ display:'flex', alignItems:'center', gap:6, marginBottom:4 }}>
                <span style={{ width:6, height:6, background:svc.fg, borderRadius:6 }} />
                <span style={{ fontSize:9, textTransform:'uppercase',
                  letterSpacing:'0.14em', opacity:0.7 }}>{h.svc}</span>
                {h.tag ? <span style={{ marginLeft:'auto', fontSize:9, fontWeight:700,
                  color: h.tag === 'n+1' || h.tag === 'slow' ? '#ff9b8a' : '#ffd78a',
                  textTransform:'uppercase', letterSpacing:'0.12em' }}>{h.tag}</span> : null}
              </div>
              <div style={{ fontSize:12, marginBottom:6,
                overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{h.name}</div>
              <div style={{ display:'flex', gap:10, opacity:0.85 }}>
                <span>dur <strong>{fmtMs(h.dur)}</strong></span>
                <span>at <strong>+{h.start}ms</strong></span>
                <span>depth <strong>{h.depth}</strong></span>
              </div>
            </div>
          );
        })() : null}
      </div>

      {/* Footer hint */}
      <div style={{
        padding:'6px 14px', borderTop:'1px solid var(--line)',
        background:'var(--surface2)',
        fontFamily:'var(--mono)', fontSize:9.5, color:'var(--ink3)',
        display:'flex', gap:18,
      }}>
        <span>click span → zoom & select</span>
        <span>esc → reset zoom</span>
        <span style={{ flex:1 }} />
        <span>showing {fmtMs(zStart)} – {fmtMs(zEnd)} · {visible.length} of {spans.length} spans</span>
      </div>
    </div>
  );
}

// ── Inspector (right rail) ───────────────────────────────────────────────
function Inspector({ span, theme, allSpans }) {
  // Attribute set per span kind; we keep a sensible default and override for
  // DB spans (the N+1 case the user will look at most).
  const isDb = span.svc === 'postgres' || span.kind === 'db';
  const attrs = isDb
    ? [
        ['db.system', 'postgresql'],
        ['db.statement', /price/i.test(span.name) ? 'SELECT price WHERE sku = $1' : span.name],
        ['db.name', 'pricing'],
        ['db.sql.table', 'product_prices'],
        ['net.peer.name', 'pg-primary.local'],
        ['net.peer.port', '5432'],
        ['otel.library.name', 'spaniel/sdk-go'],
        ['otel.library.version', '0.4.2'],
      ]
    : [
        ['rpc.system', 'grpc'],
        ['rpc.service', span.svc],
        ['net.peer.name', `${span.svc}.local`],
        ['net.peer.port', '8080'],
        ['otel.library.name', 'spaniel/sdk-go'],
        ['otel.library.version', '0.4.2'],
      ];

  // Sibling N+1 detection — show callout only on db spans with the n+1 tag
  // (the pinned example), but compute siblings dynamically so any selected
  // db span gets the same treatment.
  const siblings = allSpans.filter(s =>
    s.depth === span.depth && s.svc === span.svc && /SELECT price/i.test(s.name)
  );
  const showN1 = siblings.length >= 3 && /SELECT price/i.test(span.name);

  const events = isDb ? [
    { t: '+0.4ms', name: 'pg.connection.acquired' },
    { t: '+6.1ms', name: 'pg.row.fetched', attr:'rows=1' },
    { t: '+7.8ms', name: 'pg.connection.released' },
  ] : [
    { t: '+0.2ms', name: 'rpc.request.sent' },
    { t: '+'+(span.dur - 1)+'ms', name: 'rpc.response.received' },
  ];

  return (
    <aside style={{
      width:320, borderLeft:'1px solid var(--line)',
      background:'var(--surface)',
      display:'flex', flexDirection:'column', overflow:'hidden',
    }}>
      <div style={{ padding:'14px 16px', borderBottom:'1px solid var(--line)' }}>
        <div style={{ display:'flex', alignItems:'center', gap:8, marginBottom:7 }}>
          <SvcChip svc={span.svc} theme={theme} size="md" />
          <span style={{
            fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em',
          }}>{span.kind}</span>
          <div style={{ flex:1 }} />
          {span.tag ? <Badge tone={span.tag === 'n+1' || span.tag === 'slow' ? 'danger' : 'warn'} size="md">
            {span.tag}
          </Badge> : null}
        </div>
        <div style={{
          fontFamily:'var(--mono)', fontSize:13, color:'var(--ink)',
          lineHeight:1.4, wordBreak:'break-word',
        }}>{span.name}</div>
        <div style={{ display:'flex', alignItems:'baseline', gap:14, marginTop:10 }}>
          <div>
            <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.14em' }}>duration</div>
            <div style={{ fontFamily:'var(--serif)', fontSize:22, fontWeight:600, color:'var(--ink)' }}>
              {fmtMs(span.dur)}
            </div>
          </div>
          <div>
            <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.14em' }}>started</div>
            <div style={{ fontFamily:'var(--mono)', fontSize:13, color:'var(--ink)', paddingTop:4 }}>
              +{span.start}ms
            </div>
          </div>
        </div>
      </div>

      {showN1 ? (
        <div style={{
          margin:'12px 14px 0',
          padding:'10px 12px',
          background:'color-mix(in oklch, var(--danger) 18%, var(--surface))',
          border:'1px solid color-mix(in oklch, var(--danger) 40%, var(--surface))',
          borderRadius:8,
        }}>
          <div style={{ display:'flex', alignItems:'center', gap:6, marginBottom:4 }}>
            <span style={{ width:6, height:6, borderRadius:6, background:'var(--danger)' }} />
            <span style={{ fontFamily:'var(--mono)', fontSize:10, fontWeight:700,
              color:'var(--dangerInk)', letterSpacing:'0.06em' }}>N+1 SUSPECTED</span>
          </div>
          <div style={{ fontSize:11.5, color:'var(--ink)', lineHeight:1.4 }}>
            {siblings.length} sibling spans share fingerprint{' '}
            <span style={{ fontFamily:'var(--mono)', background:'var(--surface)',
              padding:'0 4px', borderRadius:3 }}>SELECT price WHERE sku=?</span>
            {' '}within 50ms.  Batch with{' '}
            <span style={{ fontFamily:'var(--mono)' }}>WHERE sku IN (?)</span>.
          </div>
        </div>
      ) : null}

      <div style={{ flex:1, overflow:'hidden', display:'flex', flexDirection:'column' }}>
        <div style={{
          padding:'14px 14px 6px',
          fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
          letterSpacing:'0.14em', color:'var(--ink3)',
          display:'flex', alignItems:'center',
        }}>
          attributes
          <span style={{ flex:1 }} />
          <span style={{ color:'var(--ink2)', textTransform:'none', letterSpacing:0 }}>
            {attrs.length} keys
          </span>
        </div>
        <div style={{ padding:'0 6px' }}>
          {attrs.map(([k,v]) => (
            <div key={k} style={{
              display:'grid', gridTemplateColumns:'1fr 1.4fr', gap:8,
              padding:'5px 8px', borderRadius:5,
            }}>
              <div style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)' }}>{k}</div>
              <div style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink)',
                overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{v}</div>
            </div>
          ))}
        </div>
        <div style={{
          padding:'14px 14px 6px',
          fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
          letterSpacing:'0.14em', color:'var(--ink3)',
        }}>events</div>
        <div style={{ padding:'0 14px 16px' }}>
          {events.map((e,i) => (
            <div key={i} style={{
              display:'flex', alignItems:'baseline', gap:10,
              padding:'4px 0', borderBottom: i < events.length-1 ? '1px solid var(--line2)' : 'none',
            }}>
              <span style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', width:60 }}>{e.t}</span>
              <span style={{ fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)', flex:1 }}>{e.name}</span>
              {e.attr ? <span style={{ fontFamily:'var(--mono)', fontSize:10, color:'var(--ink2)' }}>{e.attr}</span> : null}
            </div>
          ))}
        </div>
      </div>
    </aside>
  );
}

// ── Trace meta header with view tabs ─────────────────────────────────────
function TraceMeta({ trace, theme, view, onView, zoom, onResetZoom }) {
  return (
    <div style={{
      padding:'14px 18px', borderBottom:'1px solid var(--line)',
      background:'var(--surface)',
      display:'flex', alignItems:'center', gap:18,
    }}>
      <div>
        <div style={{ display:'flex', alignItems:'baseline', gap:10, marginBottom:6 }}>
          <span style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600, color:'var(--ink)' }}>
            {trace.root}
          </span>
          <Badge tone="ok">{trace.status}</Badge>
          <Badge tone="danger">{trace.warnings} lint</Badge>
        </div>
        <div style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink3)' }}>
          trace_id <span style={{ color:'var(--ink2)' }}>{trace.id}</span> · started {trace.startedAt}
        </div>
      </div>
      <div style={{ flex:1 }} />
      <Stat label="total" value={fmtMs(trace.durationMs)} big />
      <Stat label="spans" value={trace.spanCount} />
      <Stat label="services" value={trace.serviceCount} />

      {/* View toggle — pill of two tabs */}
      <div style={{
        display:'flex', alignItems:'center',
        background:'var(--surface2)', borderRadius:8,
        padding:3, border:'1px solid var(--line)',
        gap:2,
      }}>
        {[
          { k:'waterfall', label:'Waterfall', icon:<WaterfallIcon /> },
          { k:'flame',     label:'Flame',     icon:<FlameIcon /> },
        ].map(v => {
          const active = view === v.k;
          return (
            <button key={v.k} type="button"
              onClick={() => onView(v.k)}
              style={{
                display:'inline-flex', alignItems:'center', gap:6,
                padding:'5px 11px', borderRadius:6,
                background: active ? 'var(--surface)' : 'transparent',
                color: active ? 'var(--ink)' : 'var(--ink2)',
                border: active ? '1px solid var(--line)' : '1px solid transparent',
                fontFamily:'var(--sans)', fontSize:12, fontWeight:500,
                cursor:'pointer', outline:'none',
                boxShadow: active ? '0 1px 0 rgba(0,0,0,0.02)' : 'none',
              }}>
              {v.icon}{v.label}
            </button>
          );
        })}
      </div>

      {/* Reset zoom — only when zoomed */}
      {zoom && (zoom[0] > 0 || zoom[1] < trace.durationMs) ? (
        <button type="button" onClick={onResetZoom} style={{
          padding:'5px 10px', borderRadius:6,
          background:'color-mix(in oklch, var(--accent) 14%, var(--surface))',
          border:'1px solid var(--accent)', color:'var(--accentInk)',
          fontFamily:'var(--mono)', fontSize:11, fontWeight:600,
          cursor:'pointer', outline:'none',
          display:'inline-flex', alignItems:'center', gap:6,
        }}>
          ↺ reset zoom <span style={{ opacity:0.7 }}>esc</span>
        </button>
      ) : null}
    </div>
  );
}

function Stat({ label, value, big }) {
  return (
    <div>
      <div style={{ fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
        letterSpacing:'0.14em', color:'var(--ink3)' }}>{label}</div>
      <div style={{ fontFamily:'var(--serif)', fontWeight:600,
        fontSize: big ? 22 : 16, color:'var(--ink)', lineHeight:1.1, marginTop:2 }}>{value}</div>
    </div>
  );
}

function WaterfallIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12">
      <rect x="1" y="1.5" width="9" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="2.5" y="4.5" width="6" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="4" y="7.5" width="4" height="1.6" fill="currentColor" opacity="0.85" />
      <rect x="5.5" y="10" width="2" height="1.4" fill="currentColor" opacity="0.85" />
    </svg>
  );
}
function FlameIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 12 12">
      <rect x="1" y="1" width="10" height="2.6" fill="currentColor" opacity="0.45" />
      <rect x="1" y="4.2" width="5" height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="6.5" y="4.2" width="4" height="2.6" fill="currentColor" opacity="0.7" />
      <rect x="1.5" y="7.4" width="3" height="2.6" fill="currentColor" opacity="0.9" />
      <rect x="7" y="7.4" width="3" height="2.6" fill="currentColor" opacity="0.9" />
    </svg>
  );
}

// ── The full trace detail screen — stateful root ─────────────────────────
function TraceDetail({ theme }) {
  const { TRACE, SPANS } = window.SPANIEL;
  const [view, setView] = useState('waterfall');
  const [selectedId, setSelectedId] = useState('s12');
  const [hoveredId, setHoveredId] = useState(null);
  const [zoom, setZoom] = useState([0, TRACE.durationMs]);

  const selected = SPANS.find(s => s.id === selectedId) || SPANS[0];
  const resetZoom = useCallback(() => setZoom([0, TRACE.durationMs]), [TRACE.durationMs]);

  // Keyboard: esc → reset zoom
  React.useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') resetZoom(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [resetZoom]);

  return (
    <Frame>
      <Chrome active="traces" theme={theme} />
      <TraceMeta trace={TRACE} theme={theme} view={view} onView={setView}
        zoom={zoom} onResetZoom={resetZoom} />
      <MiniTimeline spans={SPANS} traceDur={TRACE.durationMs} theme={theme} zoom={zoom} />

      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <div style={{ flex:1, display:'flex', flexDirection:'column', overflow:'hidden',
          background:'var(--surface)' }}>
          {view === 'waterfall' ? (
            <>
              <Ruler traceDur={TRACE.durationMs} />
              <div style={{ flex:1, overflow:'auto' }}>
                {SPANS.map(s => (
                  <SpanRow key={s.id} span={s} traceDur={TRACE.durationMs}
                    theme={theme}
                    selected={s.id === selectedId}
                    hovered={s.id === hoveredId}
                    onSelect={setSelectedId}
                    onHover={setHoveredId} />
                ))}
              </div>
            </>
          ) : (
            <FlameView spans={SPANS} traceDur={TRACE.durationMs} theme={theme}
              selectedId={selectedId} hoveredId={hoveredId}
              onSelect={setSelectedId} onHover={setHoveredId}
              zoom={zoom} onZoom={setZoom} />
          )}
        </div>
        <Inspector span={selected} theme={theme} allSpans={SPANS} />
      </div>
    </Frame>
  );
}

window.TraceDetail = TraceDetail;
