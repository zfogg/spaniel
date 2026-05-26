// Spaniel — design system card.  One per theme, shows the tokens that
// produce the screens: colors, type ramp, chips, badges, atoms.

const { Brand, SvcChip, Badge, fmtMs: _fmtMs } = window.UI;

function SystemCard({ theme }) {
  return (
    <div style={{
      background:'var(--bg)', width:'100%', height:'100%',
      fontFamily:'var(--sans)', color:'var(--ink)',
      padding:'18px 20px',
      display:'flex', flexDirection:'column', gap:14,
      overflow:'hidden',
    }}>
      {/* Top */}
      <div style={{ display:'flex', alignItems:'center', gap:10 }}>
        <Brand tag={null} />
        <div style={{ flex:1 }} />
        <div>
          <div style={{ fontFamily:'var(--serif)', fontSize:18, fontWeight:600,
            color:'var(--ink)', textAlign:'right', letterSpacing:'-0.01em' }}>
            {theme.name}
          </div>
          <div style={{ fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
            textTransform:'uppercase', letterSpacing:'0.14em', textAlign:'right' }}>
            {theme.tag}
          </div>
        </div>
      </div>

      {/* Color swatches */}
      <div>
        <Label>palette</Label>
        <div style={{ display:'grid', gridTemplateColumns:'repeat(6, 1fr)', gap:6 }}>
          <Swatch token="bg" />
          <Swatch token="surface" />
          <Swatch token="surface2" />
          <Swatch token="accent" />
          <Swatch token="warn" />
          <Swatch token="danger" />
        </div>
      </div>

      {/* Type ramp */}
      <div>
        <Label>type</Label>
        <div style={{
          background:'var(--surface)', border:'1px solid var(--line)', borderRadius:10,
          padding:'12px 14px',
        }}>
          <div style={{ fontFamily:'var(--serif)', fontSize:22, fontWeight:600,
            color:'var(--ink)', letterSpacing:'-0.02em', lineHeight:1.1 }}>
            Postman, but for your traces
          </div>
          <div style={{ fontFamily:'var(--sans)', fontSize:13, color:'var(--ink2)',
            marginTop:6, lineHeight:1.4 }}>
            612ms p95 · 24 spans across 6 services · streamed over OTLP.
          </div>
          <div style={{
            fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
            marginTop:8, background:'var(--surface2)', borderRadius:5,
            padding:'4px 8px', display:'inline-block',
          }}>SELECT price WHERE sku = $1</div>
        </div>
      </div>

      {/* Service chips */}
      <div>
        <Label>service chips</Label>
        <div style={{ display:'flex', flexWrap:'wrap', gap:5 }}>
          {Object.keys(theme.svc).map(s => <SvcChip key={s} svc={s} theme={theme} />)}
        </div>
      </div>

      {/* Badges */}
      <div>
        <Label>span badges</Label>
        <div style={{ display:'flex', gap:6, flexWrap:'wrap' }}>
          <Badge tone="accent">baseline</Badge>
          <Badge tone="ok">ok</Badge>
          <Badge tone="warn">slow</Badge>
          <Badge tone="danger">n+1</Badge>
          <Badge tone="danger">error</Badge>
          <Badge tone="neutral">internal</Badge>
        </div>
      </div>

      {/* Mini span bar preview */}
      <div>
        <Label>span bar</Label>
        <div style={{
          background:'var(--surface)', border:'1px solid var(--line)', borderRadius:10,
          padding:'10px 12px',
        }}>
          {[
            { svc:'api-gateway',     name:'POST /api/checkout',  l:0,  w:100 },
            { svc:'cart-service',    name:'rpc.cart.GetCart',     l:2,  w:20 },
            { svc:'pricing-service', name:'pricing.Quote',         l:5,  w:14, hot:true },
            { svc:'payment-service', name:'rpc.payment.Charge',   l:40, w:52, hot:true },
          ].map((s, i) => {
            const c = theme.svc[s.svc] || {};
            return (
              <div key={i} style={{
                display:'grid', gridTemplateColumns:'minmax(0,1fr) 1.4fr',
                gap:8, padding:'4px 0',
                borderBottom: i < 3 ? '1px solid var(--line2)' : 'none',
              }}>
                <span style={{ fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink)',
                  overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>{s.name}</span>
                <div style={{ position:'relative', height:14 }}>
                  <div style={{
                    position:'absolute', top:3, height:8,
                    left: s.l + '%', width: s.w + '%',
                    background: c.bg, borderRadius:2,
                    boxShadow:`inset 2px 0 0 ${c.fg}`,
                  }} />
                  {s.hot ? <div style={{
                    position:'absolute', top:1, height:12,
                    left: s.l + '%', width: s.w + '%',
                    borderRadius:2, border:'1px solid var(--danger)',
                  }} /> : null}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div style={{ flex:1 }} />
      <div style={{
        fontFamily:'var(--mono)', fontSize:9.5, color:'var(--ink3)',
        letterSpacing:'0.08em',
      }}>
        sans <strong style={{ color:'var(--ink2)' }}>Inter Tight</strong>{' · '}
        serif <strong style={{ color:'var(--ink2)' }}>Fraunces</strong>{' · '}
        mono <strong style={{ color:'var(--ink2)' }}>JetBrains Mono</strong>
      </div>
    </div>
  );
}

function Label({ children }) {
  return <div style={{
    fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
    letterSpacing:'0.14em', color:'var(--ink3)', marginBottom:6,
  }}>{children}</div>;
}

function Swatch({ token }) {
  return (
    <div style={{
      borderRadius:8, overflow:'hidden',
      border:'1px solid var(--line)',
    }}>
      <div style={{ height:38, background:`var(--${token})` }} />
      <div style={{
        padding:'4px 6px', fontFamily:'var(--mono)', fontSize:9, color:'var(--ink3)',
        background:'var(--surface)',
      }}>{token}</div>
    </div>
  );
}

window.SystemCard = SystemCard;
