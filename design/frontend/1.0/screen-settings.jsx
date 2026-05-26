// Spaniel — Settings page (Drift direction).  Interactive: live state for
// toggles, ports, paths, sliders.  Sectioned by Network · Storage · Cloud ·
// About, with a left-rail to jump between them.

const { useState: useStateSet, useMemo } = React;
const { Chrome, Sidebar, SbGroup, SbItem, Badge, Frame, fmtMs: _f } = window.UI;

// ── Small atoms reused across the page ──────────────────────────────────
function SettingsRow({ label, hint, children, danger }) {
  return (
    <div style={{
      display:'grid', gridTemplateColumns:'minmax(0, 260px) 1fr',
      gap:18, padding:'14px 0',
      borderBottom:'1px solid var(--line2)',
      alignItems:'flex-start',
    }}>
      <div>
        <div style={{
          fontFamily:'var(--sans)', fontSize:13, fontWeight:600,
          color: danger ? 'var(--dangerInk)' : 'var(--ink)',
        }}>{label}</div>
        {hint ? <div style={{
          fontFamily:'var(--sans)', fontSize:11.5, color:'var(--ink2)',
          marginTop:3, lineHeight:1.45,
        }}>{hint}</div> : null}
      </div>
      <div style={{ display:'flex', alignItems:'center', gap:10, flexWrap:'wrap' }}>
        {children}
      </div>
    </div>
  );
}

function Toggle({ on, onChange }) {
  return (
    <button type="button" onClick={() => onChange(!on)}
      role="switch" aria-checked={on}
      style={{
        width:38, height:22, padding:0, borderRadius:22,
        background: on ? 'var(--accent)' : 'var(--surface3)',
        border:'1px solid ' + (on ? 'var(--accent)' : 'var(--line)'),
        position:'relative', cursor:'pointer', outline:'none',
        transition:'background .15s, border-color .15s',
        flex:'0 0 auto',
      }}>
      <span style={{
        position:'absolute', top:1, left: on ? 17 : 1,
        width:18, height:18, borderRadius:18,
        background:'var(--surface)',
        boxShadow:'0 1px 2px rgba(0,0,0,0.18)',
        transition:'left .18s cubic-bezier(.4,.0,.2,1)',
      }} />
    </button>
  );
}

function TextInput({ value, onChange, placeholder, mono = true, w = 220, suffix }) {
  return (
    <span style={{
      display:'inline-flex', alignItems:'center',
      background:'var(--surface)', border:'1px solid var(--line)',
      borderRadius:6, padding:'0 10px', height:30,
      width: w, maxWidth:'100%',
    }}>
      <input
        type="text" value={value} placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        style={{
          flex:1, minWidth:0, border:'none', outline:'none', background:'transparent',
          fontFamily: mono ? 'var(--mono)' : 'var(--sans)',
          fontSize:12, color:'var(--ink)',
        }} />
      {suffix ? <span style={{
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink3)', marginLeft:6,
      }}>{suffix}</span> : null}
    </span>
  );
}

function NumberInput({ value, onChange, min, max, step = 1, w = 96, suffix, decimals }) {
  // When `decimals` is provided we render a text input that formats integers
  // with the requested trailing zeros (so 1 displays as "1.0") while still
  // letting the user edit freely.  Local string state lets typing escape the
  // formatted display until blur.
  const [text, setText] = React.useState('');
  const [focused, setFocused] = React.useState(false);
  const isText = decimals != null;
  const display = isText
    ? (focused ? text : (Number.isInteger(value) ? value.toFixed(decimals) : String(value)))
    : value;
  return (
    <span style={{
      display:'inline-flex', alignItems:'center',
      background:'var(--surface)', border:'1px solid var(--line)',
      borderRadius:6, padding:'0 8px', height:30,
      width: w,
    }}>
      <input
        type={isText ? 'text' : 'number'}
        inputMode={isText ? 'decimal' : undefined}
        value={display}
        min={min} max={max} step={step}
        onFocus={() => { if (isText) { setText(String(value)); setFocused(true); } }}
        onBlur={() => {
          if (!isText) return;
          setFocused(false);
          const n = parseFloat(text);
          if (Number.isFinite(n)) {
            const clamped = Math.max(min ?? -Infinity, Math.min(max ?? Infinity, n));
            onChange(clamped);
          }
        }}
        onChange={(e) => {
          if (isText) setText(e.target.value);
          else onChange(Number(e.target.value));
        }}
        style={{
          flex:1, minWidth:0, border:'none', outline:'none', background:'transparent',
          fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
          MozAppearance:'textfield',
        }} />
      {suffix ? <span style={{
        fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
        textTransform:'uppercase', letterSpacing:'0.12em',
      }}>{suffix}</span> : null}
    </span>
  );
}

function Select({ value, onChange, options, w = 120 }) {
  return (
    <span style={{
      display:'inline-flex', alignItems:'center',
      background:'var(--surface)', border:'1px solid var(--line)',
      borderRadius:6, height:30, paddingRight:6,
      width: w,
    }}>
      <select value={value} onChange={(e) => onChange(e.target.value)}
        style={{
          flex:1, minWidth:0, border:'none', outline:'none', background:'transparent',
          fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
          padding:'0 8px', appearance:'none', WebkitAppearance:'none',
        }}>
        {options.map(o => <option key={o.value || o} value={o.value || o}>{o.label || o}</option>)}
      </select>
      <span style={{ fontFamily:'var(--mono)', color:'var(--ink3)', fontSize:9 }}>▾</span>
    </span>
  );
}

function Slider({ value, onChange, min, max, step = 1 }) {
  const pct = ((value - min) / (max - min)) * 100;
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:10,
      width:220, position:'relative', height:30,
    }}>
      <span style={{
        position:'absolute', left:0, right:0, top:'50%', height:4,
        transform:'translateY(-50%)',
        background:'var(--surface3)', borderRadius:4,
      }} />
      <span style={{
        position:'absolute', left:0, top:'50%', height:4,
        transform:'translateY(-50%)',
        width: pct + '%',
        background:'var(--accent)', borderRadius:4,
      }} />
      <input type="range" value={value} min={min} max={max} step={step}
        onChange={(e) => onChange(Number(e.target.value))}
        style={{
          position:'absolute', inset:0, width:'100%', height:'100%',
          margin:0, opacity:0, cursor:'pointer',
        }} />
      <span style={{
        position:'absolute', left: `calc(${pct}% - 8px)`, top:'50%',
        transform:'translateY(-50%)',
        width:16, height:16, borderRadius:16,
        background:'var(--surface)', border:'1.5px solid var(--accent)',
        boxShadow:'0 1px 2px rgba(0,0,0,.15)',
        pointerEvents:'none',
      }} />
    </span>
  );
}

function PathInput({ value, onChange, w = 360 }) {
  return (
    <span style={{ display:'inline-flex', alignItems:'center', gap:6 }}>
      <TextInput value={value} onChange={onChange} w={w} />
      <button type="button" style={{
        padding:'0 12px', height:30, borderRadius:6, cursor:'pointer',
        background:'var(--surface)', border:'1px solid var(--line)',
        fontFamily:'var(--sans)', fontSize:12, fontWeight:500, color:'var(--ink)',
        outline:'none',
      }}>Choose…</button>
      <button type="button" style={{
        padding:'0 10px', height:30, borderRadius:6, cursor:'pointer',
        background:'transparent', border:'1px solid var(--line)',
        fontFamily:'var(--mono)', fontSize:11, color:'var(--ink2)',
        outline:'none',
      }}>↗ reveal</button>
    </span>
  );
}

function Pill({ tone = 'neutral', children }) {
  const tones = {
    neutral: { bg:'var(--surface2)', fg:'var(--ink2)', bd:'var(--line)' },
    ok:      { bg:'color-mix(in oklch, var(--ok) 22%, var(--surface))',
               fg:'#3e6a3e', bd:'color-mix(in oklch, var(--ok) 40%, transparent)' },
    warn:    { bg:'color-mix(in oklch, var(--warn) 28%, var(--surface))',
               fg:'var(--warnInk)', bd:'color-mix(in oklch, var(--warn) 50%, transparent)' },
    accent:  { bg:'color-mix(in oklch, var(--accent) 18%, var(--surface))',
               fg:'var(--accentInk)', bd:'color-mix(in oklch, var(--accent) 40%, transparent)' },
  };
  const t = tones[tone];
  return (
    <span style={{
      display:'inline-flex', alignItems:'center', gap:5,
      padding:'3px 8px', borderRadius:14,
      fontFamily:'var(--mono)', fontSize:10, fontWeight:600,
      background:t.bg, color:t.fg, border:`1px solid ${t.bd}`,
      whiteSpace:'nowrap',
    }}>{children}</span>
  );
}

// ── Settings sections ────────────────────────────────────────────────────
function NetworkSection({ s, set, hidden }) {
  return (
    <SettingsCard
      hidden={hidden}
      anchor="network"
      title="Network"
      sub="OTLP receivers, proxy forwarding, and what happens when an app connects"
    >
      <SettingsRow label="OTLP gRPC receiver"
        hint="The :4317 listener that speaks protobuf over gRPC.">
        <Toggle on={s.grpcOn} onChange={v => set('grpcOn', v)} />
        <NumberInput value={s.grpcPort} onChange={v => set('grpcPort', v)}
          min={1024} max={65535} suffix=":port" />
        {s.grpcOn
          ? <Pill tone="ok">● listening</Pill>
          : <Pill>stopped</Pill>}
      </SettingsRow>
      <SettingsRow label="OTLP HTTP receiver"
        hint="The :4318 listener that speaks both protobuf and JSON.">
        <Toggle on={s.httpOn} onChange={v => set('httpOn', v)} />
        <NumberInput value={s.httpPort} onChange={v => set('httpPort', v)}
          min={1024} max={65535} suffix=":port" />
        {s.httpOn
          ? <Pill tone="ok">● listening</Pill>
          : <Pill>stopped</Pill>}
      </SettingsRow>
      <SettingsRow label="OTLP proxy mode"
        hint="When on, Spaniel forwards every span it receives to an upstream backend after recording it locally.">
        <Toggle on={s.proxyOn} onChange={v => set('proxyOn', v)} />
        {s.proxyOn ? <Pill tone="accent">forwarding</Pill> : <Pill>off</Pill>}
      </SettingsRow>
      <SettingsRow label="Upstream OTLP endpoint"
        hint="Where to forward telemetry when proxy mode is on.">
        <Select value={s.proxyProtocol} onChange={v => set('proxyProtocol', v)}
          options={[{value:'grpc',label:'gRPC'},{value:'http',label:'HTTP'}]} w={100} />
        <TextInput value={s.proxyUpstream} onChange={v => set('proxyUpstream', v)}
          placeholder={s.proxyProtocol === 'grpc'
            ? 'otel-collector.example.com:4317'
            : 'https://otel-collector.example.com:4318'}
          w={320} />
      </SettingsRow>
      <SettingsRow label="Forward sampling"
        hint="Fraction of spans to send upstream (0–1).  1.0 forwards everything, 0.1 forwards 10%.">
        <Slider value={s.forwardSample} onChange={v => set('forwardSample', round2(v))}
          min={0} max={1} step={0.01} />
        <NumberInput value={s.forwardSample}
          onChange={v => set('forwardSample', clamp01(v))}
          min={0} max={1} step={0.01} decimals={1} w={88} />
        <Pill>{Math.round(s.forwardSample * 100)}% of spans</Pill>
      </SettingsRow>
      <SettingsRow label="Auto-open browser on connect"
        hint="When a new service first sends spans, automatically open Spaniel in the default browser.">
        <Toggle on={s.autoOpen} onChange={v => set('autoOpen', v)} />
      </SettingsRow>
      <SettingsRow label="Bind address"
        hint="The interface receivers listen on.  Use 0.0.0.0 to accept traffic from docker / LAN.">
        <Select value={s.bind} onChange={v => set('bind', v)}
          options={['127.0.0.1', '0.0.0.0', '[::1]']} w={140} />
      </SettingsRow>
    </SettingsCard>
  );
}

function round2(n) { return Math.round(n * 100) / 100; }
function clamp01(n) {
  if (Number.isNaN(n)) return 0;
  return Math.max(0, Math.min(1, round2(n)));
}

function StorageSection({ s, set, hidden }) {
  const usedPct = Math.round((s.usedMB / s.maxMB) * 100);
  return (
    <SettingsCard
      hidden={hidden}
      anchor="storage"
      title="Storage"
      sub="Embedded DuckDB · trace store, lint warnings, sessions"
      right={<Pill tone="accent">duckdb {s.dbVersion}</Pill>}
    >
      <SettingsRow label="Database file"
        hint="The single .duckdb file that holds all of Spaniel's local data.">
        <PathInput value={s.dbPath} onChange={v => set('dbPath', v)} w={340} />
      </SettingsRow>
      <SettingsRow label="Max database size"
        hint="Spaniel evicts the oldest spans when the file gets within 10% of this cap.">
        <NumberInput value={s.maxSize} onChange={v => set('maxSize', v)}
          min={64} max={102400} step={64} w={120} />
        <Select value={s.maxUnit} onChange={v => set('maxUnit', v)}
          options={['MB', 'GB']} w={88} />
      </SettingsRow>
      <SettingsRow label="Retention"
        hint="How long to keep spans before they're dropped from the db.  Lower retention = less disk pressure.">
        <NumberInput value={s.retention} onChange={v => set('retention', v)}
          min={1} max={s.retentionUnit === 'seconds' ? 86400 : s.retentionUnit === 'minutes' ? 1440 : s.retentionUnit === 'hours' ? 168 : 90}
          w={120} />
        <Select value={s.retentionUnit} onChange={v => set('retentionUnit', v)}
          options={['seconds', 'minutes', 'hours', 'days']} w={120} />
        <Pill>≈ {humanRetention(s.retention, s.retentionUnit)}</Pill>
      </SettingsRow>

      <SettingsRow label="Storage usage"
        hint={`Currently ${s.usedMB.toLocaleString()} MB across 12,438 spans and 327 traces.`}>
        <div style={{ width:340, display:'flex', flexDirection:'column', gap:6 }}>
          <div style={{
            height:8, borderRadius:8, background:'var(--surface3)',
            border:'1px solid var(--line)', overflow:'hidden', position:'relative',
          }}>
            <div style={{
              position:'absolute', left:0, top:0, bottom:0,
              width: usedPct + '%',
              background: usedPct > 85 ? 'var(--danger)' : usedPct > 60 ? 'var(--warn)' : 'var(--accent)',
            }} />
          </div>
          <div style={{
            display:'flex', justifyContent:'space-between',
            fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
          }}>
            <span>{s.usedMB.toLocaleString()} MB used</span>
            <span style={{ color:'var(--ink3)' }}>of {s.maxSize.toLocaleString()} {s.maxUnit}</span>
          </div>
        </div>
        <button type="button" style={{
          padding:'0 12px', height:30, borderRadius:6, cursor:'pointer',
          background:'var(--surface)', border:'1px solid var(--line)',
          fontFamily:'var(--sans)', fontSize:12, color:'var(--ink)',
          outline:'none',
        }}>spaniel prune</button>
      </SettingsRow>

      <SettingsRow label="Compress on idle"
        hint="Run DuckDB CHECKPOINT + VACUUM when no spans have arrived for 60s.  Reduces file size.">
        <Toggle on={s.compress} onChange={v => set('compress', v)} />
      </SettingsRow>
      <SettingsRow danger label="Drop all data"
        hint="Wipe the database and start over.  Cannot be undone.">
        <button type="button" style={{
          padding:'0 14px', height:30, borderRadius:6, cursor:'pointer',
          background:'transparent',
          border:'1px solid var(--danger)',
          color:'var(--dangerInk)',
          fontFamily:'var(--sans)', fontSize:12, fontWeight:600,
          outline:'none',
        }}>drop &amp; recreate</button>
      </SettingsRow>
    </SettingsCard>
  );
}

function humanRetention(n, unit) {
  const sec = unit === 'seconds' ? n
    : unit === 'minutes' ? n * 60
    : unit === 'hours' ? n * 3600
    : n * 86400;
  if (sec < 60) return sec + 's';
  if (sec < 3600) return Math.round(sec/60) + 'min';
  if (sec < 86400) return Math.round(sec/3600) + 'h';
  return Math.round(sec/86400) + ' days';
}

function AboutSection({ s, set, hidden }) {
  return (
    <SettingsCard hidden={hidden} anchor="about" title="About" sub="Build, license, telemetry">
      <SettingsRow label="Version" hint="Current binary build · pinned in ~/.spaniel/version">
        <code style={{ fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)' }}>spaniel 0.4.2</code>
        <Pill tone="accent">channel: stable</Pill>
        <button type="button" style={{
          padding:'0 12px', height:30, borderRadius:6, cursor:'pointer',
          background:'var(--surface)', border:'1px solid var(--line)',
          fontFamily:'var(--sans)', fontSize:12, color:'var(--ink)', outline:'none',
        }}>check for updates</button>
      </SettingsRow>
      <SettingsRow label="License" hint="Spaniel is MIT-licensed.">
        <Pill>MIT</Pill>
        <a style={{
          fontFamily:'var(--sans)', fontSize:12, color:'var(--accentInk)',
          textDecoration:'underline', textDecorationStyle:'dotted',
        }} href="#">view source</a>
      </SettingsRow>
      <SettingsRow label="Anonymous usage stats"
        hint="Help shape Spaniel by sending crash + feature counts.  No span data, ever.">
        <Toggle on={s.telemetry} onChange={v => set('telemetry', v)} />
      </SettingsRow>
    </SettingsCard>
  );
}

// ── Card wrapper for each section ────────────────────────────────────────
function SettingsCard({ anchor, title, sub, right, children, hidden }) {
  if (hidden) return null;
  return (
    <section id={anchor} style={{
      background:'var(--surface)',
      border:'1px solid var(--line)',
      borderRadius:12, padding:'18px 22px', marginBottom:18,
      scrollMarginTop:80,
    }}>
      <div style={{
        display:'flex', alignItems:'center', gap:10, marginBottom:8,
        paddingBottom:12, borderBottom:'1px solid var(--line)',
      }}>
        <div>
          <div style={{
            fontFamily:'var(--serif)', fontSize:20, fontWeight:600,
            color:'var(--ink)', letterSpacing:'-0.01em',
          }}>{title}</div>
          {sub ? <div style={{
            fontFamily:'var(--sans)', fontSize:12, color:'var(--ink2)', marginTop:2,
          }}>{sub}</div> : null}
        </div>
        <div style={{ flex:1 }} />
        {right}
      </div>
      {children}
    </section>
  );
}

// ── The full Settings screen ─────────────────────────────────────────────
function Settings({ theme }) {
  const [s, setS] = useStateSet({
    grpcOn: true,
    grpcPort: 4317,
    httpOn: true,
    httpPort: 4318,
    proxyOn: false,
    proxyProtocol: 'http',
    proxyUpstream: 'https://otel-collector.acme.com:4318',
    forwardSample: 1,
    autoOpen: true,
    bind: '127.0.0.1',

    dbVersion: '0.10.2',
    dbPath: '~/.spaniel/spaniel.duckdb',
    maxSize: 4,
    maxUnit: 'GB',
    usedMB: 612,
    retention: 24,
    retentionUnit: 'hours',
    compress: true,

    telemetry: false,
  });
  const set = (k, v) => setS(prev => ({ ...prev, [k]: v }));
  const [section, setSection] = useStateSet('network');

  const sections = [
    { id:'network', label:'Network',           dot:'var(--accent)' },
    { id:'storage', label:'Storage · DuckDB', dot:'var(--warn)' },
    { id:'about',   label:'About',              dot:'var(--ink3)' },
  ];

  return (
    <Frame>
      <Chrome active="settings" theme={theme} />
      <div style={{ flex:1, display:'flex', minHeight:0 }}>
        <Sidebar>
          <SbGroup title="settings">
            {sections.map(sec => (
              <SbItem key={sec.id} active={section === sec.id}
                dot={section === sec.id ? sec.dot : 'var(--ink3)'}
                onClick={() => setSection(sec.id)}>{sec.label}</SbItem>
            ))}
          </SbGroup>
          <SbGroup title="config file">
            <div style={{
              fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
              padding:'5px 8px', background:'var(--surface)',
              border:'1px solid var(--line)', borderRadius:6,
              lineHeight:1.4, wordBreak:'break-all',
            }}>
              ~/.spaniel/<br/>config.toml
            </div>
            <button type="button" style={{
              marginTop:6, padding:'5px 8px', borderRadius:6,
              background:'transparent', border:'1px solid var(--line)',
              fontFamily:'var(--mono)', fontSize:10.5, color:'var(--ink2)',
              cursor:'pointer', textAlign:'left', outline:'none',
            }}>open in $EDITOR</button>
          </SbGroup>
          <div style={{ flex:1 }} />
          <SbGroup title="status">
            <div style={{
              padding:'6px 8px', borderRadius:6,
              background:'color-mix(in oklch, var(--ok) 18%, var(--surface))',
              fontFamily:'var(--mono)', fontSize:10.5, color:'#3e6a3e',
              display:'flex', flexDirection:'column', gap:2,
            }}>
              <span><span style={{ width:6, height:6, borderRadius:6, background:'var(--ok)',
                display:'inline-block', marginRight:6 }} />daemon running</span>
              <span style={{ color:'var(--ink3)' }}>pid 32118 · 4h 12m</span>
            </div>
          </SbGroup>
        </Sidebar>

        <div style={{
          flex:1, overflow:'hidden auto', padding:'20px 24px 40px',
          display:'flex', flexDirection:'column',
        }}>
          <div style={{ marginBottom:18 }}>
            <div style={{
              fontFamily:'var(--mono)', fontSize:10, color:'var(--ink3)',
              textTransform:'uppercase', letterSpacing:'0.18em', marginBottom:6,
            }}>preferences</div>
            <h1 style={{
              margin:0, fontFamily:'var(--serif)', fontSize:28, fontWeight:600,
              letterSpacing:'-0.02em', color:'var(--ink)',
            }}>Settings</h1>
            <div style={{
              fontFamily:'var(--sans)', fontSize:13, color:'var(--ink2)',
              marginTop:4, maxWidth:640,
            }}>
              Saved to <code style={{
                fontFamily:'var(--mono)', fontSize:12, color:'var(--ink)',
                background:'var(--surface2)', padding:'1px 6px', borderRadius:4,
              }}>~/.spaniel/config.toml</code>. Changes take effect immediately — no restart needed.
            </div>
          </div>

          <NetworkSection s={s} set={set} hidden={section !== 'network'} />
          <StorageSection s={s} set={set} hidden={section !== 'storage'} />
          <AboutSection s={s} set={set} hidden={section !== 'about'} />
        </div>
      </div>
    </Frame>
  );
}

window.Settings = Settings;
