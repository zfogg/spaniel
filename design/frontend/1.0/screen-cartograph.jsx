// Spaniel — Cartograph direction.  Editorial atlas aesthetic.  Serif
// numerals, hairline + dashed rules, generous whitespace, marginalia, spans
// as thin lines on a chart.  Reads like a Tufte field guide.

const { fmtMs: cfmtMs } = window.UI;
const C = {};

// ── Atoms ────────────────────────────────────────────────────────────────
C.Smallcaps = function ({ children, style }) {
  return (
    <span style={{
      fontFamily:'var(--mono)', fontSize:9.5,
      textTransform:'uppercase', letterSpacing:'0.18em',
      color:'var(--ink3)', ...style,
    }}>{children}</span>
  );
};

C.Stat = function ({ label, value, sub, big = 30 }) {
  return (
    <div>
      <C.Smallcaps>{label}</C.Smallcaps>
      <div style={{
        fontFamily:'var(--serif)', fontSize:big, fontWeight:500,
        color:'var(--ink)', lineHeight:1, marginTop:6,
        fontFeatureSettings:'"lnum","tnum"', letterSpacing:'-0.02em',
      }}>{value}</div>
      {sub ? <div style={{
        fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11,
        color:'var(--ink3)', marginTop:4,
      }}>{sub}</div> : null}
    </div>
  );
};

C.Plate = function ({ children, n }) {
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:6,
      fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11,
      color:'var(--ink3)',
    }}>
      <span style={{ letterSpacing:'0.04em' }}>fig.</span>
      <span style={{ fontWeight:600, fontStyle:'normal' }}>{n}</span>
      {children ? <span>· {children}</span> : null}
    </span>
  );
};

C.SvcLabel = function ({ svc, theme, italic = false }) {
  const c = theme.svc[svc] || {};
  return (
    <span style={{ display:'inline-flex', alignItems:'center', gap:5 }}>
      <span style={{ width:6, height:6, background:c.fg, transform:'rotate(45deg)' }} />
      <span style={{
        fontFamily:'var(--mono)', fontSize:9.5, color:c.fg,
        textTransform:'uppercase', letterSpacing:'0.14em',
        fontStyle: italic ? 'italic' : 'normal',
      }}>{svc.replace(/-service$/,'')}</span>
    </span>
  );
};

C.Tag = function ({ tone = 'neutral', children }) {
  const tones = {
    accent: { fg:'var(--accentInk)', bg:'transparent', border:'var(--accent)' },
    ok:     { fg:'#3e6a3e',          bg:'transparent', border:'#88b29a' },
    warn:   { fg:'var(--warnInk)',   bg:'transparent', border:'var(--warn)' },
    danger: { fg:'var(--danger)',    bg:'transparent', border:'var(--danger)' },
    neutral:{ fg:'var(--ink2)',      bg:'transparent', border:'var(--line)' },
  };
  const t = tones[tone];
  return (
    <span style={{
      fontFamily:'var(--mono)', fontSize:9, fontWeight:600,
      textTransform:'uppercase', letterSpacing:'0.14em',
      color:t.fg, border:`1px solid ${t.border}`,
      padding:'1px 6px', whiteSpace:'nowrap',
    }}>{children}</span>
  );
};

// Dotted leader row: label .................. value
C.LeaderRow = function ({ k, v, italic }) {
  return (
    <div style={{ display:'flex', alignItems:'baseline', gap:6, padding:'4px 0' }}>
      <span style={{
        fontFamily: italic ? 'var(--serif)' : 'var(--mono)',
        fontStyle: italic ? 'italic' : 'normal',
        fontSize:11, color:'var(--ink2)', whiteSpace:'nowrap',
      }}>{k}</span>
      <span style={{
        flex:1, borderBottom:'1px dotted var(--line)',
        margin:'0 4px', alignSelf:'baseline', transform:'translateY(-3px)',
      }} />
      <span style={{
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink)',
        textAlign:'right', whiteSpace:'nowrap',
      }}>{v}</span>
    </div>
  );
};

C.Chrome = function ({ active = 'traces' }) {
  const navs = ['traces','services','lints','sessions','settings'];
  return (
    <div style={{
      padding:'14px 22px', borderBottom:'1px solid var(--ink)',
      background:'var(--surface)',
      display:'flex', alignItems:'center', gap:18,
    }}>
      <div style={{ display:'flex', alignItems:'center', gap:8 }}>
        <svg width="22" height="22" viewBox="0 0 28 28">
          <circle cx="14" cy="14" r="13" fill="none" stroke="var(--ink)" strokeWidth="1" />
          <circle cx="14" cy="14" r="9" fill="none" stroke="var(--ink)" strokeWidth="1"
            strokeDasharray="2 2" />
          <circle cx="14" cy="14" r="2" fill="var(--accent)" />
          <line x1="14" y1="1" x2="14" y2="5" stroke="var(--ink)" strokeWidth="1" />
          <line x1="14" y1="23" x2="14" y2="27" stroke="var(--ink)" strokeWidth="1" />
          <line x1="1" y1="14" x2="5" y2="14" stroke="var(--ink)" strokeWidth="1" />
          <line x1="23" y1="14" x2="27" y2="14" stroke="var(--ink)" strokeWidth="1" />
        </svg>
        <span style={{
          fontFamily:'var(--serif)', fontSize:22, fontWeight:500,
          color:'var(--ink)', letterSpacing:'-0.02em',
        }}>Spaniel</span>
        <span style={{
          fontFamily:'var(--serif)', fontStyle:'italic', fontSize:12,
          color:'var(--ink3)', marginLeft:2,
        }}>vol. 0.4</span>
      </div>
      <span style={{ flex:1 }} />
      <nav style={{ display:'flex', gap:18 }}>
        {navs.map(n => (
          <span key={n} style={{
            fontFamily:'var(--mono)', fontSize:10,
            textTransform:'uppercase', letterSpacing:'0.18em',
            color: n === active ? 'var(--ink)' : 'var(--ink3)',
            fontWeight: n === active ? 700 : 500,
            borderBottom: n === active ? '1.5px solid var(--accent)' : '1.5px solid transparent',
            paddingBottom:2,
          }}>{n}</span>
        ))}
      </nav>
      <span style={{ flex:1 }} />
      <div style={{ display:'flex', alignItems:'center', gap:10 }}>
        <span style={{
          fontFamily:'var(--mono)', fontSize:10, color:'#3e6a3e',
          textTransform:'uppercase', letterSpacing:'0.14em',
        }}>
          <span style={{ width:6, height:6, borderRadius:6, background:'#88b29a',
            display:'inline-block', marginRight:6, verticalAlign:'middle' }} />
          listening · 4317 / 4318
        </span>
        <span style={{ width:1, height:18, background:'var(--line)' }} />
        <span style={{
          fontFamily:'var(--serif)', fontStyle:'italic', fontSize:13,
          color:'var(--ink)',
        }}>feat / checkout</span>
      </div>
    </div>
  );
};

C.Frame = function ({ children }) {
  return (
    <div style={{
      width:'100%', height:'100%', background:'var(--bg)',
      display:'flex', flexDirection:'column',
      fontFamily:'var(--sans)', color:'var(--ink)',
      // Subtle paper grain via a tiny dot pattern
      backgroundImage:`radial-gradient(circle at 1px 1px, rgba(80,50,20,0.05) 1px, transparent 1px)`,
      backgroundSize:'12px 12px',
      overflow:'hidden',
    }}>{children}</div>
  );
};

// ── System card ──────────────────────────────────────────────────────────
C.SystemCard = function ({ theme }) {
  return (
    <C.Frame>
      <div style={{ padding:'24px 26px', display:'flex', flexDirection:'column', gap:18, flex:1 }}>
        {/* Heading slab */}
        <div style={{ borderBottom:'1px solid var(--ink)', paddingBottom:14 }}>
          <C.Smallcaps>direction ii</C.Smallcaps>
          <div style={{
            fontFamily:'var(--serif)', fontSize:36, fontWeight:500,
            color:'var(--ink)', letterSpacing:'-0.025em', marginTop:6, lineHeight:1,
          }}>Cartograph</div>
          <div style={{
            fontFamily:'var(--serif)', fontStyle:'italic', fontSize:13,
            color:'var(--ink2)', marginTop:6,
          }}>An editorial atlas of traces — serif numerals, hairline rules, marginalia.</div>
        </div>

        {/* Palette as a chart legend */}
        <div>
          <C.Smallcaps>palette &amp; ink</C.Smallcaps>
          <div style={{ display:'grid', gridTemplateColumns:'repeat(6, 1fr)',
            gap:0, marginTop:10, border:'1px solid var(--line)' }}>
            {['bg','surface','accent','warn','danger','ink'].map((t, i) => (
              <div key={t} style={{
                borderRight: i < 5 ? '1px solid var(--line)' : 'none',
              }}>
                <div style={{ height:42, background:`var(--${t})` }} />
                <div style={{
                  padding:'5px 6px', fontFamily:'var(--mono)', fontSize:9,
                  color:'var(--ink3)', textTransform:'uppercase', letterSpacing:'0.12em',
                  background:'var(--surface)', borderTop:'1px solid var(--line)',
                }}>{t}</div>
              </div>
            ))}
          </div>
        </div>

        {/* Type ramp */}
        <div>
          <C.Smallcaps>type · serif · sans · mono</C.Smallcaps>
          <div style={{ marginTop:8 }}>
            <div style={{
              fontFamily:'var(--serif)', fontSize:30, fontWeight:500,
              letterSpacing:'-0.02em', lineHeight:1.1, color:'var(--ink)',
            }}>612<span style={{ fontStyle:'italic' }}>ms</span></div>
            <div style={{
              fontFamily:'var(--serif)', fontStyle:'italic', fontSize:13,
              color:'var(--ink2)', marginTop:2,
            }}>p95 across twenty-four spans · six services</div>
            <div style={{
              fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)', marginTop:8,
            }}>SELECT price WHERE sku = $1</div>
          </div>
        </div>

        {/* Span line: thin lines on a chart */}
        <div>
          <C.Smallcaps>span line · hairline + tick</C.Smallcaps>
          <div style={{ background:'var(--surface)', border:'1px solid var(--line)',
            padding:'14px 16px', marginTop:8 }}>
            {[
              { svc:'api-gateway',     name:'POST /api/checkout', l:0,  w:100, dur:'612ms' },
              { svc:'cart-service',    name:'rpc.cart.GetCart',    l:2,  w:20,  dur:'124ms' },
              { svc:'pricing-service', name:'pricing.Quote',        l:5,  w:14,  dur:'80ms',  hot:true },
              { svc:'payment-service', name:'rpc.payment.Charge',  l:40, w:52,  dur:'312ms', hot:true },
            ].map((s, i) => {
              const c = theme.svc[s.svc];
              return (
                <div key={i} style={{
                  display:'grid', gridTemplateColumns:'140px 1fr 50px',
                  gap:10, padding:'7px 0', borderBottom: i < 3 ? '1px dotted var(--line)' : 'none',
                  alignItems:'center',
                }}>
                  <div style={{
                    fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11.5,
                    color:'var(--ink)', overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                  }}>{s.name}</div>
                  <div style={{ position:'relative', height:10 }}>
                    {/* Hairline baseline */}
                    <div style={{ position:'absolute', left:0, right:0, top:5, height:1,
                      borderTop:'1px dashed var(--line)' }} />
                    {/* The span: thick(ish) hairline with serif end ticks */}
                    <div style={{
                      position:'absolute', top:4, height:2,
                      left: s.l + '%', width: s.w + '%',
                      background: s.hot ? 'var(--danger)' : c.fg,
                    }} />
                    <div style={{
                      position:'absolute', top:1, height:8, width:1,
                      left: s.l + '%', background: s.hot ? 'var(--danger)' : c.fg,
                    }} />
                    <div style={{
                      position:'absolute', top:1, height:8, width:1,
                      left: `calc(${s.l + s.w}% - 1px)`, background: s.hot ? 'var(--danger)' : c.fg,
                    }} />
                  </div>
                  <div style={{
                    textAlign:'right', fontFamily:'var(--mono)', fontSize:10,
                    color:'var(--ink2)', fontFeatureSettings:'"tnum"',
                  }}>{s.dur}</div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Tags */}
        <div>
          <C.Smallcaps>annotations</C.Smallcaps>
          <div style={{ display:'flex', gap:8, flexWrap:'wrap', marginTop:8 }}>
            <C.Tag tone="accent">baseline</C.Tag>
            <C.Tag tone="ok">ok</C.Tag>
            <C.Tag tone="warn">slow</C.Tag>
            <C.Tag tone="danger">n+1</C.Tag>
            <C.Tag tone="danger">err</C.Tag>
            <C.Tag tone="neutral">internal</C.Tag>
          </div>
        </div>

        <div style={{ flex:1 }} />
        <div style={{
          borderTop:'1px solid var(--line)', paddingTop:10,
          fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11,
          color:'var(--ink3)',
        }}>
          set in Fraunces (serif), Inter Tight (sans), JetBrains Mono (mono). · plate ii / v.
        </div>
      </div>
    </C.Frame>
  );
};

// ── Trace detail (hero) ──────────────────────────────────────────────────
C.TraceDetail = function ({ theme }) {
  const { TRACE, SPANS } = window.SPANIEL;
  const D = TRACE.durationMs;
  const selectedId = 's12';
  const sel = SPANS.find(s => s.id === selectedId);

  return (
    <C.Frame>
      <C.Chrome active="traces" />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        {/* Main column */}
        <div style={{ flex:1, padding:'22px 26px 14px 26px', display:'flex',
          flexDirection:'column', minWidth:0, overflow:'hidden' }}>
          {/* Title */}
          <div style={{ display:'flex', alignItems:'baseline', gap:14, marginBottom:6 }}>
            <C.Smallcaps>trace 9ef357237e8c74950f711b77779d19eb</C.Smallcaps>
            <span style={{ flex:1 }} />
            <C.Tag tone="ok">ok</C.Tag>
            <C.Tag tone="danger">3 lint</C.Tag>
          </div>
          <h1 style={{
            margin:'4px 0 12px', fontFamily:'var(--serif)', fontWeight:500,
            fontSize:34, letterSpacing:'-0.025em', color:'var(--ink)', lineHeight:1.05,
          }}>
            POST /api/checkout
            <span style={{ fontStyle:'italic', color:'var(--ink2)', fontSize:18, marginLeft:10 }}>
              observed at 14:22:08.421
            </span>
          </h1>

          {/* Stats strip */}
          <div style={{
            display:'flex', gap:36, padding:'14px 0 18px',
            borderTop:'1px solid var(--ink)', borderBottom:'1px solid var(--line)',
            marginTop:4,
          }}>
            <C.Stat label="duration" value="612" sub="milliseconds, total" big={34} />
            <div style={{ width:1, background:'var(--line)' }} />
            <C.Stat label="spans" value="24" sub="across six services" big={34} />
            <div style={{ width:1, background:'var(--line)' }} />
            <C.Stat label="deepest" value="5" sub="pricing → postgres" big={34} />
            <div style={{ width:1, background:'var(--line)' }} />
            <C.Stat label="hot path" value="312ms" sub="stripe.charge — fig. ii" big={34} />
          </div>

          {/* Waterfall as a chart */}
          <div style={{ flex:1, overflow:'hidden', marginTop:14, position:'relative' }}>
            <div style={{ display:'flex', alignItems:'baseline', marginBottom:6 }}>
              <C.Plate n="i" >waterfall · time on the abscissa</C.Plate>
              <span style={{ flex:1 }} />
              {/* Mini scale */}
              <div style={{ display:'flex', alignItems:'baseline', gap:4,
                fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)' }}>
                <span>0</span>
                <span style={{ width:60, height:1, background:'var(--ink)' }} />
                <span>{Math.round(D/2)}ms</span>
                <span style={{ width:60, height:1, background:'var(--ink)' }} />
                <span>{D}ms</span>
              </div>
            </div>

            {/* Tick rules */}
            <div style={{ position:'relative', borderTop:'1px solid var(--ink)',
              borderBottom:'1px solid var(--ink)', paddingTop:8, paddingBottom:6 }}>
              {/* Vertical grid */}
              <div style={{
                position:'absolute', inset:0, display:'flex', pointerEvents:'none',
              }}>
                {[1,2,3,4,5].map(i => <div key={i} style={{
                  flex:1, borderRight: i < 5 ? '1px dashed var(--line)' : 'none',
                }} />)}
              </div>
              {/* Spans */}
              {SPANS.map((s, i) => {
                const c = theme.svc[s.svc] || {};
                const left = (s.start / D) * 100;
                const width = Math.max(0.4, (s.dur / D) * 100);
                const isSel = s.id === selectedId;
                const hot = s.tag === 'n+1' || s.tag === 'slow';
                return (
                  <div key={s.id} style={{
                    display:'grid', gridTemplateColumns:'190px 1fr 56px',
                    alignItems:'center', padding:'2px 0',
                    position:'relative',
                    background: isSel ? 'color-mix(in oklch, var(--accent) 8%, transparent)' : 'transparent',
                  }}>
                    <div style={{
                      display:'flex', alignItems:'baseline', gap:6, paddingLeft: s.depth * 12,
                      minWidth:0, overflow:'hidden',
                    }}>
                      <span style={{
                        fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11.5,
                        color:'var(--ink)', overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                      }}>{s.name}</span>
                    </div>
                    <div style={{ position:'relative', height:12 }}>
                      {/* The line itself */}
                      <div style={{
                        position:'absolute', top:5, height:2,
                        left: left + '%', width: width + '%',
                        background: hot ? 'var(--danger)' : c.fg,
                      }} />
                      {/* End ticks */}
                      <div style={{
                        position:'absolute', top:2, width:1, height:8,
                        left: left + '%', background: hot ? 'var(--danger)' : c.fg,
                      }} />
                      <div style={{
                        position:'absolute', top:2, width:1, height:8,
                        left: `calc(${left + width}% - 1px)`, background: hot ? 'var(--danger)' : c.fg,
                      }} />
                      {/* svc label inline next to span if room */}
                      <span style={{
                        position:'absolute', top:-3,
                        left: `calc(${left + width}% + 6px)`,
                        fontFamily:'var(--mono)', fontSize:8.5, color:c.fg,
                        textTransform:'uppercase', letterSpacing:'0.14em', whiteSpace:'nowrap',
                      }}>{s.svc.replace(/-service$/,'')}</span>
                    </div>
                    <div style={{
                      textAlign:'right', fontFamily:'var(--mono)', fontSize:10,
                      color:'var(--ink2)', fontFeatureSettings:'"tnum"',
                    }}>{cfmtMs(s.dur)}</div>
                  </div>
                );
              })}
            </div>
            <div style={{
              marginTop:8, display:'flex', alignItems:'baseline', gap:18,
              fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11,
              color:'var(--ink3)',
            }}>
              <span>fig. i — span durations as hairlines, ordered by depth then start time.</span>
              <span style={{ flex:1 }} />
              <span>n = 22 of 24 shown</span>
            </div>
          </div>
        </div>

        {/* Sidebar — field guide */}
        <aside style={{
          width:300, padding:'22px 22px 22px 22px',
          borderLeft:'1px solid var(--ink)',
          background:'var(--surface)',
          display:'flex', flexDirection:'column', gap:14, overflow:'hidden',
        }}>
          <div>
            <C.Smallcaps>field guide</C.Smallcaps>
            <div style={{
              fontFamily:'var(--serif)', fontSize:20, fontWeight:500, color:'var(--ink)',
              letterSpacing:'-0.01em', lineHeight:1.15, marginTop:4,
            }}>SELECT price WHERE sku=?</div>
            <div style={{
              fontFamily:'var(--serif)', fontStyle:'italic', fontSize:12,
              color:'var(--ink2)', marginTop:4,
            }}>pricing-service → postgres · depth 5 · sibling n. 4 of 5</div>
          </div>

          {/* N+1 marginal note */}
          <div style={{
            border:'1px solid var(--danger)', padding:'10px 12px', position:'relative',
            background:'color-mix(in oklch, var(--danger) 10%, var(--surface))',
          }}>
            <div style={{ position:'absolute', top:-9, left:14, background:'var(--surface)',
              padding:'0 6px', fontFamily:'var(--mono)', fontSize:9,
              color:'var(--danger)', fontWeight:700, letterSpacing:'0.14em' }}>
              ! N+1 SUSPECTED
            </div>
            <div style={{ fontFamily:'var(--serif)', fontSize:12, lineHeight:1.5,
              color:'var(--ink)', marginTop:2 }}>
              Five sibling spans share the fingerprint <span style={{ fontFamily:'var(--mono)',
                background:'var(--surface2)', padding:'0 4px', fontSize:11 }}>SELECT price WHERE sku=?</span>
              {' '}within 50ms.  Batch with{' '}
              <span style={{ fontFamily:'var(--mono)' }}>WHERE sku IN (?)</span> to recover ≈32ms.
            </div>
          </div>

          {/* Attributes — dotted leader rows */}
          <div>
            <C.Smallcaps>attributes · 8</C.Smallcaps>
            <div style={{ marginTop:6 }}>
              <C.LeaderRow k="db.system"       v="postgresql" />
              <C.LeaderRow k="db.name"          v="pricing" />
              <C.LeaderRow k="db.statement"     v="SELECT price …" />
              <C.LeaderRow k="net.peer.name"    v="pg-primary" />
              <C.LeaderRow k="net.peer.port"    v="5432" />
              <C.LeaderRow k="otel.library"     v="sdk-go 0.4.2" />
            </div>
          </div>

          {/* Events */}
          <div>
            <C.Smallcaps>events · 3</C.Smallcaps>
            <div style={{ marginTop:6 }}>
              <C.LeaderRow italic k="+0.4ms — pg.acquired"   v="" />
              <C.LeaderRow italic k="+6.1ms — pg.row.fetched" v="rows=1" />
              <C.LeaderRow italic k="+7.8ms — pg.released"   v="" />
            </div>
          </div>

          <div style={{ flex:1 }} />
          <div style={{
            fontFamily:'var(--serif)', fontStyle:'italic', fontSize:11,
            color:'var(--ink3)', borderTop:'1px solid var(--line)', paddingTop:10,
          }}>
            see fig. ii — service map · &amp; · plate iii — diff vs main
          </div>
        </aside>
      </div>
    </C.Frame>
  );
};

// ── Trace list ───────────────────────────────────────────────────────────
C.TraceList = function ({ theme }) {
  const { TRACES } = window.SPANIEL;
  const maxDur = Math.max(...TRACES.map(t => t.dur));
  return (
    <C.Frame>
      <C.Chrome active="traces" />
      <div style={{ padding:'22px 26px', display:'flex', flexDirection:'column', flex:1, overflow:'hidden' }}>
        <div>
          <C.Smallcaps>index · feat/checkout · last 5 min</C.Smallcaps>
          <h1 style={{
            margin:'4px 0 4px', fontFamily:'var(--serif)', fontWeight:500,
            fontSize:30, letterSpacing:'-0.025em', color:'var(--ink)',
          }}>
            Recent traces
            <span style={{ fontStyle:'italic', color:'var(--ink3)', fontSize:18, marginLeft:10 }}>
              ({TRACES.length})
            </span>
          </h1>
          <div style={{
            fontFamily:'var(--serif)', fontStyle:'italic', fontSize:13,
            color:'var(--ink2)',
          }}>
            Sorted by recency.  Hairlines scaled to the longest trace this window — <span
              style={{ fontFamily:'var(--mono)', fontStyle:'normal' }}>{cfmtMs(maxDur)}</span>.
          </div>
        </div>

        <div style={{ height:1, background:'var(--ink)', margin:'14px 0' }} />

        {/* Header row */}
        <div style={{
          display:'grid',
          gridTemplateColumns:'46px minmax(0,1fr) 80px 80px 300px 80px',
          gap:14, paddingBottom:6, borderBottom:'1px solid var(--line)',
        }}>
          <C.Smallcaps>№</C.Smallcaps>
          <C.Smallcaps>operation · trace</C.Smallcaps>
          <C.Smallcaps style={{ textAlign:'right' }}>dur</C.Smallcaps>
          <C.Smallcaps style={{ textAlign:'right' }}>spans</C.Smallcaps>
          <C.Smallcaps>shape</C.Smallcaps>
          <C.Smallcaps style={{ textAlign:'right' }}>ago</C.Smallcaps>
        </div>

        <div style={{ flex:1, overflow:'hidden' }}>
          {TRACES.map((t, i) => {
            const isFirst = i === 0;
            const hot = t.tag === 'n+1' || t.tag === 'slow' || t.status === 'error';
            return (
              <div key={t.id} style={{
                display:'grid',
                gridTemplateColumns:'46px minmax(0,1fr) 80px 80px 300px 80px',
                gap:14, padding:'12px 0', alignItems:'center',
                borderBottom:'1px dotted var(--line)',
                background: isFirst ? 'color-mix(in oklch, var(--accent) 6%, transparent)' : 'transparent',
              }}>
                <div style={{
                  fontFamily:'var(--serif)', fontSize:14, fontStyle:'italic',
                  color:'var(--ink3)', fontFeatureSettings:'"onum"',
                }}>{String(i+1).padStart(2,'0')}</div>
                <div style={{ minWidth:0 }}>
                  <div style={{ display:'flex', alignItems:'baseline', gap:8 }}>
                    <span style={{
                      fontFamily:'var(--serif)', fontSize:16, fontWeight:500,
                      color:'var(--ink)', overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap',
                    }}>{t.op}</span>
                    {t.tag === 'n+1'      ? <C.Tag tone="danger">n+1</C.Tag> : null}
                    {t.tag === 'slow'     ? <C.Tag tone="warn">slow</C.Tag> : null}
                    {t.tag === 'baseline' ? <C.Tag tone="accent">baseline</C.Tag> : null}
                    {t.status === 'error' ? <C.Tag tone="danger">err</C.Tag> : null}
                  </div>
                  <div style={{
                    fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)', marginTop:2,
                  }}>{t.id}</div>
                </div>
                <div style={{
                  textAlign:'right', fontFamily:'var(--serif)', fontSize:18, fontWeight:500,
                  color:'var(--ink)', fontFeatureSettings:'"tnum"',
                }}>{cfmtMs(t.dur)}</div>
                <div style={{
                  textAlign:'right', fontFamily:'var(--serif)', fontStyle:'italic',
                  fontSize:13, color:'var(--ink2)',
                }}>{t.spans}</div>
                <div style={{ position:'relative', height:10 }}>
                  <div style={{
                    position:'absolute', top:4, left:0, right:0, height:1,
                    borderTop:'1px dashed var(--line)',
                  }} />
                  <div style={{
                    position:'absolute', top:4, left:0,
                    width:(t.dur/maxDur)*100 + '%', height:2,
                    background: hot ? 'var(--danger)' : 'var(--ink)',
                  }} />
                  <div style={{
                    position:'absolute', top:1, left:`calc(${(t.dur/maxDur)*100}% - 1px)`,
                    width:1, height:8, background: hot ? 'var(--danger)' : 'var(--ink)',
                  }} />
                </div>
                <div style={{
                  textAlign:'right', fontFamily:'var(--serif)', fontStyle:'italic',
                  fontSize:12, color:'var(--ink3)',
                }}>{t.t}</div>
              </div>
            );
          })}
        </div>
      </div>
    </C.Frame>
  );
};

// ── Service map — Cartograph's signature ────────────────────────────────
C.ServiceMap = function ({ theme }) {
  const { SERVICES, EDGES } = window.SPANIEL;
  const W = 880, H = 360;
  const idx = Object.fromEntries(SERVICES.map(s => [s.id, s]));

  return (
    <C.Frame>
      <C.Chrome active="services" />
      <div style={{ padding:'18px 26px 8px', display:'flex', alignItems:'baseline', gap:14 }}>
        <div>
          <C.Smallcaps>plate iii · service topology</C.Smallcaps>
          <h1 style={{
            margin:'4px 0 0', fontFamily:'var(--serif)', fontWeight:500,
            fontSize:28, letterSpacing:'-0.02em', color:'var(--ink)',
          }}>
            A map of services
            <span style={{ fontStyle:'italic', color:'var(--ink3)', fontSize:16, marginLeft:10 }}>
              from 34 traces in this session
            </span>
          </h1>
        </div>
        <span style={{ flex:1 }} />
        <C.Tag tone="accent">live</C.Tag>
      </div>

      <div style={{
        flex:1, margin:'10px 26px 18px', position:'relative',
        border:'1px solid var(--ink)', background:'var(--surface)',
        overflow:'hidden',
      }}>
        {/* Compass rose */}
        <svg width="62" height="62" viewBox="0 0 62 62" style={{
          position:'absolute', top:14, right:14, opacity:0.65,
        }}>
          <circle cx="31" cy="31" r="28" fill="none" stroke="var(--ink)" strokeWidth="0.8" />
          <circle cx="31" cy="31" r="20" fill="none" stroke="var(--ink)" strokeWidth="0.5" strokeDasharray="2 2" />
          <line x1="31" y1="3"  x2="31" y2="59" stroke="var(--ink)" strokeWidth="0.8" />
          <line x1="3"  y1="31" x2="59" y2="31" stroke="var(--ink)" strokeWidth="0.8" />
          <polygon points="31,5 28,15 31,12 34,15" fill="var(--accent)" />
          <text x="31" y="14" textAnchor="middle" fontFamily="var(--mono)"
            fontSize="6" fill="var(--ink)" letterSpacing="0.1em">UP</text>
          <text x="31" y="55" textAnchor="middle" fontFamily="var(--mono)"
            fontSize="6" fill="var(--ink2)" letterSpacing="0.1em">DOWN</text>
        </svg>

        <svg viewBox={`0 0 ${W} ${H}`} width="100%" height="100%" preserveAspectRatio="xMidYMid meet">
          {/* Soft grid */}
          <defs>
            <pattern id="cgrid" width="40" height="40" patternUnits="userSpaceOnUse">
              <path d="M40 0 L0 0 0 40" fill="none" stroke="var(--line2)" strokeWidth="0.5" />
            </pattern>
          </defs>
          <rect width={W} height={H} fill="url(#cgrid)" />

          {/* Edges — dashed hairlines, labels in serif italic */}
          {EDGES.map(([a,b,calls,ms], i) => {
            const A = idx[a], B = idx[b];
            if (!A || !B) return null;
            const x1 = A.x + 100, y1 = A.y + 30;
            const x2 = B.x + 20,  y2 = B.y + 30;
            const mx = (x1+x2)/2, my = (y1+y2)/2;
            const hot = ms > 200;
            return (
              <g key={i}>
                <path d={`M ${x1} ${y1} Q ${mx} ${(y1+y2)/2}, ${x2} ${y2}`}
                  fill="none"
                  stroke={hot ? 'var(--danger)' : 'var(--ink2)'}
                  strokeWidth={hot ? 1.6 : 1}
                  strokeDasharray={hot ? '0' : '3 3'} />
                <circle cx={x2} cy={y2} r={hot ? 3 : 2.2}
                  fill={hot ? 'var(--danger)' : 'var(--ink2)'} />
                <text x={mx} y={my - 4} textAnchor="middle"
                  fontFamily="var(--serif)" fontStyle="italic" fontSize="11"
                  fill="var(--ink)">{cfmtMs(ms)}</text>
                <text x={mx} y={my + 10} textAnchor="middle"
                  fontFamily="var(--mono)" fontSize="8.5" fill="var(--ink3)"
                  letterSpacing="0.14em">{calls}×</text>
              </g>
            );
          })}

          {/* Nodes — bordered cards */}
          {SERVICES.map(s => {
            const c = theme.svc[s.id] || {};
            return (
              <g key={s.id} transform={`translate(${s.x}, ${s.y})`}>
                <rect width="120" height="60" fill="var(--surface)" stroke="var(--ink)" strokeWidth="1" />
                <rect width="120" height="14" fill={c.bg} stroke="var(--ink)" strokeWidth="1" />
                <text x="6" y="11" fontFamily="var(--mono)" fontSize="8.5"
                  fill={c.fg} fontWeight="700"
                  letterSpacing="0.16em">
                  {s.id.toUpperCase()}
                </text>
                <text x="60" y="36" textAnchor="middle"
                  fontFamily="var(--serif)" fontSize="20" fontWeight="500"
                  fill="var(--ink)">{s.calls}</text>
                <text x="60" y="50" textAnchor="middle"
                  fontFamily="var(--serif)" fontStyle="italic" fontSize="9"
                  fill="var(--ink3)">spans this trace</text>
              </g>
            );
          })}
        </svg>

        {/* Scale bar in corner */}
        <div style={{
          position:'absolute', left:14, bottom:14,
          display:'flex', alignItems:'center', gap:6,
          fontFamily:'var(--mono)', fontSize:9, color:'var(--ink2)',
          letterSpacing:'0.12em',
        }}>
          <span>0</span>
          <span style={{ width:50, height:1, background:'var(--ink)' }} />
          <span>200ms</span>
          <span style={{ width:50, height:1, background:'var(--ink)' }} />
          <span>400ms</span>
          <span style={{ marginLeft:10, fontFamily:'var(--serif)', fontStyle:'italic' }}>
            edge weight ∝ latency
          </span>
        </div>
      </div>

      <div style={{
        padding:'0 26px 16px',
        fontFamily:'var(--serif)', fontStyle:'italic', fontSize:12,
        color:'var(--ink3)',
        display:'flex', gap:16, alignItems:'baseline',
      }}>
        <span>fig. ii — solid rust hairline indicates ≥ 200ms.</span>
        <span style={{ flex:1 }} />
        <span>8 services · 10 edges · 24 spans</span>
      </div>
    </C.Frame>
  );
};

// ── Lint warnings ────────────────────────────────────────────────────────
C.LintWarnings = function ({ theme }) {
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
    <C.Frame>
      <C.Chrome active="lints" />
      <div style={{ padding:'22px 26px 8px' }}>
        <C.Smallcaps>errata · feat/checkout</C.Smallcaps>
        <h1 style={{
          margin:'4px 0', fontFamily:'var(--serif)', fontWeight:500,
          fontSize:30, letterSpacing:'-0.025em', color:'var(--ink)',
        }}>
          Six points of correction
          <span style={{ fontStyle:'italic', color:'var(--ink3)', fontSize:18, marginLeft:10 }}>
            against OTel semantic conventions
          </span>
        </h1>
        {/* Summary chips */}
        <div style={{
          marginTop:14, display:'grid', gridTemplateColumns:'repeat(4, 1fr)',
          borderTop:'1px solid var(--ink)', borderBottom:'1px solid var(--line)',
        }}>
          {[
            { n:'1',  l:'error',    s:'missing http.request.method', tone:'danger' },
            { n:'1',  l:'N+1',      s:'pricing.Quote · 6 reads',     tone:'danger' },
            { n:'4',  l:'warnings', s:'db.system, db.statement…',    tone:'warn' },
            { n:'78%', l:'coverage', s:'1 route observed zero spans', tone:'neutral' },
          ].map((c,i) => (
            <div key={i} style={{
              padding:'14px 16px',
              borderRight: i < 3 ? '1px solid var(--line)' : 'none',
            }}>
              <C.Stat label={c.l} value={c.n} sub={c.s} big={26} />
            </div>
          ))}
        </div>
      </div>

      <div style={{ flex:1, overflow:'hidden', padding:'0 26px 20px' }}>
        {rows.map((r, i) => {
          const tone = r.sev === 'error' ? 'danger' : r.sev === 'warn' ? 'warn' : 'neutral';
          const color = r.sev === 'error' ? 'var(--danger)'
            : r.sev === 'warn' ? 'var(--warnInk)' : 'var(--ink3)';
          return (
            <div key={i} style={{
              padding:'14px 0', borderBottom:'1px dotted var(--line)',
              display:'grid', gridTemplateColumns:'46px 1fr', gap:18,
            }}>
              <div style={{
                fontFamily:'var(--serif)', fontStyle:'italic', fontSize:14,
                color, paddingTop:2,
              }}>{String(i+1).padStart(2,'0')}.</div>
              <div>
                <div style={{ display:'flex', alignItems:'baseline', gap:10, marginBottom:4 }}>
                  <span style={{
                    fontFamily:'var(--serif)', fontSize:15, fontWeight:600, color:'var(--ink)',
                  }}>{r.code}</span>
                  <C.Tag tone={tone}>{r.sev}</C.Tag>
                  <span style={{ flex:1 }} />
                  <span style={{
                    fontFamily:'var(--serif)', fontStyle:'italic', fontSize:12, color:'var(--ink2)',
                  }}>in <span style={{ fontFamily:'var(--mono)', fontStyle:'normal',
                    background:'var(--surface)', padding:'1px 5px' }}>{r.span}</span></span>
                </div>
                <div style={{
                  fontFamily:'var(--serif)', fontSize:13.5, color:'var(--ink)',
                  lineHeight:1.5, maxWidth:680,
                }}>{r.msg}.</div>
                <div style={{
                  marginTop:6, fontFamily:'var(--serif)', fontStyle:'italic', fontSize:12,
                  color:'var(--ink2)',
                }}>
                  Remedy — <span style={{ fontFamily:'var(--mono)', fontStyle:'normal',
                    color:'var(--accentInk)' }}>{r.fix}</span>.
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </C.Frame>
  );
};

window.Cartograph = C;
