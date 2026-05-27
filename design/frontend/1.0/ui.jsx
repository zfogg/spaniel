// Spaniel — shared atoms used by every screen. Every component reads CSS vars
// from the artboard root, so the same component renders correctly under any
// theme.  Inline styles only — no global stylesheets — so themes don't leak
// across artboards.

const { useState } = React;

// ── Logo ─────────────────────────────────────────────────────────────────
// A spaniel ear-drop mark.  Two soft drops in the accent color, a third in
// the warn color for a hint of brand variety.  Drawn from primitives.
function Logo({ size = 22 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 28 28" style={{ display:'block' }}>
      <ellipse cx="8" cy="13" rx="6" ry="9" fill="var(--accent)" opacity="0.78"
        transform="rotate(-12 8 13)" />
      <ellipse cx="20" cy="13" rx="6" ry="9" fill="var(--accent)" opacity="0.55"
        transform="rotate(12 20 13)" />
      <circle cx="14" cy="14" r="5.4" fill="var(--surface)" stroke="var(--ink)" strokeWidth="1.2" />
      <circle cx="12.2" cy="13.5" r="0.9" fill="var(--ink)" />
      <circle cx="15.8" cy="13.5" r="0.9" fill="var(--ink)" />
      <path d="M12.6 16.6 Q14 17.6 15.4 16.6" stroke="var(--ink)" strokeWidth="1.1" fill="none" strokeLinecap="round" />
    </svg>
  );
}

// ── Wordmark + tag ───────────────────────────────────────────────────────
function Brand({ tag }) {
  return (
    <div style={{ display:'flex', alignItems:'center', gap:8, height:22 }}>
      <Logo size={22} />
      <span style={{
        fontFamily:'var(--serif)', fontSize:20, fontWeight:600,
        color:'var(--ink)', letterSpacing:'-0.01em', lineHeight:'22px',
        display:'inline-flex', alignItems:'center', height:22,
      }}>spaniel</span>
      {tag ? <span style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
        textTransform:'lowercase', letterSpacing:'0.06em',
        display:'inline-flex', alignItems:'center', height:22, lineHeight:1,
      }}>{tag}</span> : null}
    </div>
  );
}

// ── Service chip ─────────────────────────────────────────────────────────
function SvcChip({ svc, theme, size = 'sm' }) {
  const c = (theme.svc && theme.svc[svc]) || { fg:'var(--ink2)', bg:'var(--chipBg)' };
  const pad = size === 'sm' ? '2px 7px' : '3px 9px';
  const fs  = size === 'sm' ? 10 : 11;
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:5,
      padding:pad, borderRadius:6, fontFamily:'var(--mono)',
      fontSize:fs, color:c.fg, background:c.bg, whiteSpace:'nowrap',
      letterSpacing:'-0.01em',
    }}>
      <span style={{ width:5, height:5, borderRadius:5, background:c.fg, opacity:0.75 }} />
      {svc}
    </span>
  );
}

// ── Tag badge (n+1, slow, lint…) ─────────────────────────────────────────
function Badge({ tone = 'neutral', children, size = 'sm' }) {
  const tones = {
    neutral: { bg:'var(--chipBg)',                  fg:'var(--chipInk)' },
    danger:  { bg:'color-mix(in oklch, var(--danger) 28%, var(--surface))',
               fg:'var(--dangerInk)' },
    warn:    { bg:'color-mix(in oklch, var(--warn) 35%, var(--surface))',
               fg:'var(--warnInk)' },
    accent:  { bg:'color-mix(in oklch, var(--accent) 30%, var(--surface))',
               fg:'var(--accentInk)' },
    ok:      { bg:'color-mix(in oklch, var(--ok) 30%, var(--surface))',
               fg:'#3e6a3e' },
  };
  const t = tones[tone];
  const fs = size === 'sm' ? 9 : 10;
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:4,
      padding:'2px 6px', borderRadius:4,
      fontFamily:'var(--mono)', fontSize:fs, fontWeight:600,
      color:t.fg, background:t.bg, letterSpacing:'0.04em',
      textTransform:'uppercase',
    }}>{children}</span>
  );
}

// ── Top chrome — logo, nav, session, status ─────────────────────────────
function Chrome({ active = 'traces', theme }) {
  const navs = ['traces','spans','metrics','services','coverage','lints','sessions','settings'];
  return (
    <div style={{
      height:46, padding:'0 16px',
      borderBottom:'1px solid var(--line)',
      background:'var(--surface)',
      display:'flex', alignItems:'center', gap:18,
    }}>
      <Brand tag="v0.4" />
      <div style={{ width:1, height:18, background:'var(--line)' }} />
      <nav style={{ display:'flex', gap:2 }}>
        {navs.map((n) => (
          <span key={n} style={{
            padding:'5px 10px', borderRadius:6,
            fontFamily:'var(--sans)', fontSize:12, fontWeight:500,
            color: n === active ? 'var(--ink)' : 'var(--ink2)',
            background: n === active ? 'var(--surface2)' : 'transparent',
            border: n === active ? '1px solid var(--line)' : '1px solid transparent',
            textTransform:'capitalize', cursor:'default',
          }}>{n}</span>
        ))}
      </nav>
      <div style={{ flex:1 }} />
      <button type="button" style={{
        display:'inline-flex', alignItems:'center', gap:7,
        padding:'4px 8px 4px 9px', borderRadius:6, whiteSpace:'nowrap',
        background:'var(--surface2)', border:'1px solid var(--line)',
        color:'var(--ink2)', fontFamily:'var(--sans)', fontSize:11, fontWeight:500,
        cursor:'pointer', outline:'none', height:26,
      }}>
        <svg width="12" height="12" viewBox="0 0 14 14" fill="none"
          style={{ display:'block', flex:'0 0 auto' }}>
          <circle cx="6" cy="6" r="4" stroke="currentColor" strokeWidth="1.4" />
          <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="currentColor"
            strokeWidth="1.4" strokeLinecap="round" />
        </svg>
        <span>Search</span>
        <span style={{
          fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
          background:'var(--surface)', border:'1px solid var(--line)',
          padding:'1px 5px', borderRadius:4, marginLeft:3,
        }}>{(typeof navigator !== 'undefined' && /Mac|iPhone|iPad/i.test(navigator.platform || navigator.userAgent || '')) ? '⌘K' : 'Ctrl+K'}</span>
      </button>
      <span style={{
        display:'inline-flex', alignItems:'center', gap:7,
        padding:'4px 9px', borderRadius:14, whiteSpace:'nowrap',
        background:'color-mix(in oklch, var(--ok) 25%, var(--surface))',
        color:'#3e6a3e', fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
      }}>
        <span style={{ width:6, height:6, borderRadius:6, background:'var(--ok)', flex:'0 0 auto',
          boxShadow:'0 0 0 3px color-mix(in oklch, var(--ok) 30%, transparent)' }} />
        otlp 4317/4318
      </span>
      <span style={{
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
        padding:'4px 8px', border:'1px solid var(--line)', borderRadius:6,
      }}>session: <strong style={{ color:'var(--ink)' }}>feat/checkout</strong></span>
    </div>
  );
}

// ── Sidebar (filter rail) — used by trace list + lint screens ───────────
function Sidebar({ children }) {
  return (
    <aside style={{
      width:200, padding:'18px 14px',
      borderRight:'1px solid var(--line)',
      background:'var(--surface2)',
      display:'flex', flexDirection:'column', gap:18,
      fontFamily:'var(--sans)',
    }}>{children}</aside>
  );
}
function SbGroup({ title, children }) {
  return (
    <div>
      <div style={{
        fontFamily:'var(--mono)', fontSize:9, textTransform:'uppercase',
        letterSpacing:'0.14em', color:'var(--ink3)', marginBottom:8,
      }}>{title}</div>
      <div style={{ display:'flex', flexDirection:'column', gap:2 }}>{children}</div>
    </div>
  );
}
function SbItem({ children, active, count, dot, onClick }) {
  return (
    <div onClick={onClick} role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
      style={{
      display:'flex', alignItems:'center', gap:8,
      padding:'5px 8px', borderRadius:6,
      fontSize:12, color: active ? 'var(--ink)' : 'var(--ink2)',
      background: active ? 'var(--surface)' : 'transparent',
      boxShadow: active ? 'inset 0 0 0 1px var(--line)' : 'none',
      fontWeight: active ? 600 : 500,
      cursor: onClick ? 'pointer' : 'default',
      userSelect: 'none',
    }}>
      {dot ? <span style={{ width:7, height:7, borderRadius:7, background:dot }} /> : null}
      <span style={{ flex:1, overflow:'hidden', textOverflow:'ellipsis', whiteSpace:'nowrap' }}>
        {children}
      </span>
      {count != null ? <span style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
      }}>{count}</span> : null}
    </div>
  );
}

// ── Section header above a panel ────────────────────────────────────────
function PanelHead({ title, sub, right }) {
  return (
    <div style={{
      padding:'12px 16px', borderBottom:'1px solid var(--line)',
      display:'flex', alignItems:'center', gap:12,
      background:'var(--surface)',
    }}>
      <div style={{ flex:1 }}>
        <div style={{
          fontFamily:'var(--sans)', fontSize:13, fontWeight:600,
          color:'var(--ink)', letterSpacing:'-0.01em',
        }}>{title}</div>
        {sub ? <div style={{
          fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
          marginTop:2, letterSpacing:'0.02em',
        }}>{sub}</div> : null}
      </div>
      {right}
    </div>
  );
}

// ── Card frame ──────────────────────────────────────────────────────────
function Frame({ children, style }) {
  return (
    <div style={{
      background:'var(--bg)',
      width:'100%', height:'100%',
      overflow:'hidden',
      display:'flex', flexDirection:'column',
      fontFamily:'var(--sans)', color:'var(--ink)',
      ...style,
    }}>{children}</div>
  );
}

// ── Duration formatter ──────────────────────────────────────────────────
function fmtMs(ms) {
  if (ms < 1) return ms.toFixed(1) + 'ms';
  if (ms >= 1000) return (ms/1000).toFixed(2) + 's';
  return Math.round(ms) + 'ms';
}

window.UI = { Logo, Brand, SvcChip, Badge, Chrome, Sidebar, SbGroup, SbItem,
  PanelHead, Frame, fmtMs };
