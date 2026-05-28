import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type Settings as SettingsT, type SettingsUpdate, type StorageBreakdown } from '@/lib/api'

// ── atoms ────────────────────────────────────────────────────────────────────

function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={`w-[38px] h-[22px] p-0 rounded-[22px] relative cursor-pointer outline-hidden transition-[background,border-color] duration-150 shrink-0 border ${on ? 'bg-accent-d border-accent-d' : 'bg-muted border-border'}`}
    >
      <span
        className="absolute top-px w-[18px] h-[18px] rounded-full bg-background shadow-[0_1px_2px_rgba(0,0,0,0.18)] transition-[left] duration-[180ms] ease-[cubic-bezier(.4,.0,.2,1)]"
        style={{ left: on ? 17 : 1 }}
      />
    </button>
  )
}

function NumberBox({ value, onChange, min, max, step, w = 110, suffix, ariaLabel }: {
  value: number; onChange: (v: number) => void
  min?: number; max?: number; step?: number; w?: number; suffix?: string; ariaLabel: string
}) {
  return (
    <span
      className="inline-flex items-center bg-background border border-border rounded-md px-2 h-[30px]"
      style={{ width: w }}
    >
      <input
        type="number"
        aria-label={ariaLabel}
        value={value}
        min={min} max={max} step={step}
        onChange={e => onChange(Number(e.target.value))}
        className="flex-1 min-w-0 border-none outline-hidden bg-transparent font-mono text-xs text-foreground"
      />
      {suffix && (
        <span className="font-mono text-[10px] text-muted-foreground uppercase tracking-[0.12em] ml-1">{suffix}</span>
      )}
    </span>
  )
}

function TextBox({ value, onChange, w = 320, mono = true, ariaLabel }: {
  value: string; onChange: (v: string) => void
  w?: number; mono?: boolean; ariaLabel: string
}) {
  return (
    <span
      className="inline-flex items-center bg-background border border-border rounded-md px-2.5 h-[30px] max-w-full"
      style={{ width: w }}
    >
      <input
        type="text"
        aria-label={ariaLabel}
        value={value}
        onChange={e => onChange(e.target.value)}
        className={`flex-1 min-w-0 border-none outline-hidden bg-transparent text-xs text-foreground ${mono ? 'font-mono' : 'font-sans'}`}
      />
    </span>
  )
}

function SelectBox<T extends string>({ value, onChange, options, w = 130, ariaLabel }: {
  value: T; onChange: (v: T) => void
  options: readonly T[]; w?: number; ariaLabel: string
}) {
  return (
    <span
      className="inline-flex items-center bg-background border border-border rounded-md h-[30px] pr-1.5"
      style={{ width: w }}
    >
      <select
        aria-label={ariaLabel}
        value={value}
        onChange={e => onChange(e.target.value as T)}
        className="flex-1 min-w-0 border-none outline-hidden bg-transparent font-mono text-xs text-foreground px-2 appearance-none"
      >
        {options.map(o => <option key={o} value={o}>{o}</option>)}
      </select>
      <span className="font-mono text-muted-foreground text-[9px]">▾</span>
    </span>
  )
}

function Pill({ tone = 'neutral', children }: { tone?: 'neutral' | 'ok' | 'warn' | 'accent'; children: React.ReactNode }) {
  const tones = {
    neutral: { bg: 'var(--muted)', fg: 'var(--muted-foreground)', bd: 'var(--border)' },
    ok:      { bg: '#dff0e0', fg: '#3e6a3e', bd: '#9bc4a4' },
    warn:    { bg: '#fcefcf', fg: '#8a6118', bd: '#d9b878' },
    accent:  { bg: 'color-mix(in oklch, var(--accent, #6b7cff) 18%, var(--background))',
               fg: 'var(--accent, #4a5dc7)',
               bd: 'color-mix(in oklch, var(--accent, #6b7cff) 40%, transparent)' },
  } as const
  const t = tones[tone]
  return (
    <span
      className="inline-flex items-center gap-[5px] py-[3px] px-2 rounded-[14px] font-mono text-[10px] font-semibold whitespace-nowrap"
      style={{ background: t.bg, color: t.fg, border: `1px solid ${t.bd}` }}
    >{children}</span>
  )
}

function Row({ label, hint, danger, children, testid }: {
  label: string; hint?: string; danger?: boolean; testid?: string
  children: React.ReactNode
}) {
  return (
    <div
      data-testid={testid}
      className="grid gap-[18px] py-3.5 border-b border-border items-start"
      style={{ gridTemplateColumns: 'minmax(0, 260px) 1fr' }}
    >
      <div>
        <div className={`font-sans text-[13px] font-semibold ${danger ? 'text-danger' : 'text-foreground'}`}>{label}</div>
        {hint && (
          <div className="font-sans text-[11.5px] text-muted-foreground mt-[3px] leading-[1.45]">{hint}</div>
        )}
      </div>
      <div className="flex items-center gap-2.5 flex-wrap">
        {children}
      </div>
    </div>
  )
}

function Card({ id, title, sub, right, children, hidden }: {
  id: string; title: string; sub?: string; right?: React.ReactNode
  children: React.ReactNode; hidden?: boolean
}) {
  if (hidden) return null
  return (
    <section
      id={id}
      className="bg-background border border-border rounded-xl py-[18px] px-[22px] mb-[18px] scroll-mt-20"
    >
      <div className="flex items-center gap-2.5 mb-2 pb-3 border-b border-border">
        <div>
          <div className="font-serif text-xl font-semibold text-foreground tracking-[-0.01em]">{title}</div>
          {sub && (
            <div className="font-sans text-xs text-muted-foreground mt-0.5">{sub}</div>
          )}
        </div>
        <div className="flex-1" />
        {right}
      </div>
      {children}
    </section>
  )
}

// ── helpers ──────────────────────────────────────────────────────────────────

function fmtUptime(ns: number): string {
  const s = Math.floor(ns / 1_000_000_000)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
}

function fmtMB(bytes: number): string {
  const mb = bytes / (1024 * 1024)
  if (mb < 1) return `${Math.round(bytes / 1024)} KB`
  if (mb < 1024) return `${mb.toFixed(1)} MB`
  return `${(mb / 1024).toFixed(2)} GB`
}

type RetentionUnit = 'seconds' | 'minutes' | 'hours' | 'days'

function humanRetention(n: number, unit: RetentionUnit): string {
  const sec = unit === 'seconds' ? n
    : unit === 'minutes' ? n * 60
    : unit === 'hours' ? n * 3600
    : n * 86400
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.round(sec / 60)}min`
  if (sec < 86400) return `${Math.round(sec / 3600)}h`
  return `${Math.round(sec / 86400)} days`
}

// ── sections ─────────────────────────────────────────────────────────────────

function NetworkSection({ s, mutate, hidden }: {
  s: SettingsT; mutate: (patch: SettingsUpdate) => void; hidden: boolean
}) {
  return (
    <Card
      id="network"
      title="Network"
      sub="OTLP receivers and proxy forwarding"
      hidden={hidden}
    >
      <Row label="OTLP gRPC receiver"
        hint="The :4317 listener that speaks protobuf over gRPC. Set port = 0 to disable."
        testid="row-grpc">
        <Toggle on={s.otlp_grpc_port > 0} onChange={v => mutate({ otlp_grpc_port: v ? 4317 : 0 })} label="enable grpc receiver" />
        <NumberBox value={s.otlp_grpc_port} onChange={v => mutate({ otlp_grpc_port: v })}
          min={0} max={65535} suffix=":port" ariaLabel="grpc port" />
        {s.otlp_grpc_port > 0
          ? <Pill tone="ok">● listening :{s.runtime.otlp_grpc_port}</Pill>
          : <Pill>stopped</Pill>}
      </Row>
      <Row label="OTLP HTTP receiver"
        hint="The :4318 listener that speaks both protobuf and JSON. Set port = 0 to disable."
        testid="row-http">
        <Toggle on={s.otlp_http_port > 0} onChange={v => mutate({ otlp_http_port: v ? 4318 : 0 })} label="enable http receiver" />
        <NumberBox value={s.otlp_http_port} onChange={v => mutate({ otlp_http_port: v })}
          min={0} max={65535} suffix=":port" ariaLabel="http port" />
        {s.otlp_http_port > 0
          ? <Pill tone="ok">● listening :{s.runtime.otlp_http_port}</Pill>
          : <Pill>stopped</Pill>}
      </Row>
      <Row label="Bind address"
        hint="Network interface the UI and OTLP receivers listen on. Use 0.0.0.0 to accept traffic from docker / LAN."
        testid="row-bind">
        <SelectBox<'127.0.0.1' | '0.0.0.0' | '::1'>
          value={(s.bind_address as '127.0.0.1' | '0.0.0.0' | '::1') ?? '127.0.0.1'}
          onChange={v => mutate({ bind_address: v })}
          options={['127.0.0.1', '0.0.0.0', '::1'] as const}
          w={140} ariaLabel="bind address"
        />
        <Pill>{s.bind_address === '0.0.0.0' ? 'all interfaces' : 'localhost only'}</Pill>
      </Row>
      <Row label="Forward sampling"
        hint="Fraction of received payloads to forward upstream (1.0 = everything, 0.1 = 10%)."
        testid="row-forward-sample">
        <input
          type="range"
          min={0} max={1} step={0.01}
          value={s.forward_sample}
          onChange={e => mutate({ forward_sample: parseFloat(e.target.value) })}
          aria-label="forward sample"
          className="w-[180px]"
        />
        <NumberBox value={s.forward_sample} onChange={v => mutate({ forward_sample: v })}
          min={0} max={1} step={0.01} w={90} ariaLabel="forward sample number" />
        <Pill tone="accent">
          {s.forward_sample >= 1 ? 'everything' : `${Math.round(s.forward_sample * 100)}% of spans`}
        </Pill>
      </Row>
      <Row label="UI / API port"
        hint="HTTP port the spaniel UI and JSON API are served on. Restart required."
        testid="row-port">
        <NumberBox value={s.port} onChange={v => mutate({ port: v })}
          min={1} max={65535} ariaLabel="ui port" suffix=":port" />
      </Row>
      <Row label="Upstream OTLP endpoint(s)"
        hint="If set, every received OTLP payload is forwarded to these URLs after being stored locally."
        testid="row-forward">
        <TextBox
          value={(s.forward ?? []).join(', ')}
          onChange={v => mutate({ forward: v.split(',').map(x => x.trim()).filter(Boolean) })}
          w={420} ariaLabel="forward urls" mono
        />
        {(s.forward ?? []).length > 0
          ? <Pill tone="accent">forwarding to {s.forward.length} upstream{s.forward.length === 1 ? '' : 's'}</Pill>
          : <Pill>off</Pill>}
      </Row>
      <Row label="Auto-open browser on startup"
        hint="When the daemon starts, open the spaniel UI in the default browser."
        testid="row-autoopen">
        <Toggle on={!s.no_browser} onChange={v => mutate({ no_browser: !v })} label="auto open browser" />
      </Row>
    </Card>
  )
}

const TABLE_SEGMENTS = [
  { key: 'spans',         color: 'var(--accent, #6b7cff)',    label: 'Spans' },
  { key: 'logs',          color: '#3e9c8a',                   label: 'Logs' },
  { key: 'metrics',       color: '#b58400',                   label: 'Metrics' },
  { key: 'span_events',   color: '#7c5cbf',                   label: 'Events' },
  { key: 'trace_issues',  color: 'var(--danger, #b04040)',    label: 'Issues' },
] as const

function StorageSection({ s, mutate, hidden, onPrune, onDrop, breakdown, onCompact }: {
  s: SettingsT; mutate: (patch: SettingsUpdate) => void; hidden: boolean
  onPrune: () => void; onDrop: () => void
  breakdown: StorageBreakdown | null
  onCompact: () => Promise<void>
}) {
  const [retUnit, setRetUnit] = useState<RetentionUnit>('days')
  const [compacting, setCompacting] = useState(false)
  const [compactMsg, setCompactMsg] = useState<string | null>(null)

  const usedPct = s.max_db_size_mb > 0
    ? Math.min(100, Math.round((s.runtime.db_size_bytes / (s.max_db_size_mb * 1024 * 1024)) * 100))
    : 0
  const barColor = usedPct > 85 ? 'var(--danger, #b04040)' : usedPct > 60 ? '#b58400' : 'var(--accent, #6b7cff)'

  // Stacked bar: total bytes across the tracked tables.
  const totalBytes = breakdown
    ? breakdown.tables.reduce((sum, t) => sum + t.approx_bytes, 0)
    : 0

  const handleCompact = useCallback(async () => {
    setCompacting(true)
    setCompactMsg(null)
    try {
      await onCompact()
      setCompactMsg('Done')
    } catch (e) {
      setCompactMsg(String(e))
    } finally {
      setCompacting(false)
    }
  }, [onCompact])

  return (
    <Card id="storage" title="Storage" sub="Embedded DuckDB · spans, logs, metrics, sessions"
      right={<Pill tone="accent">duckdb · {fmtMB(s.runtime.db_size_bytes)}</Pill>}
      hidden={hidden}>
      <Row label="Database file" hint="The single .duckdb file that holds all of spaniel's local data."
        testid="row-dbpath">
        <TextBox value={s.db_path} onChange={v => mutate({ db_path: v })} w={420} ariaLabel="db path" />
      </Row>
      <Row label="Max database size"
        hint="Retention drops the oldest sessions when the file grows past this cap. 0 = unlimited."
        testid="row-maxsize">
        <NumberBox value={s.max_db_size_mb} onChange={v => mutate({ max_db_size_mb: v })}
          min={0} max={102400} suffix="MB" w={130} ariaLabel="max db size mb" />
      </Row>
      <Row label="Retention"
        hint="How long to keep spans before they're dropped. Lower = less disk pressure."
        testid="row-retention">
        <NumberBox value={s.retention_days} onChange={v => mutate({ retention_days: v })}
          min={0} max={3650} ariaLabel="retention days" w={120} suffix="days" />
        <SelectBox<RetentionUnit>
          value={retUnit}
          onChange={setRetUnit}
          options={['seconds', 'minutes', 'hours', 'days'] as const}
          ariaLabel="retention unit"
        />
        <span data-testid="retention-pill">
          <Pill>≈ {humanRetention(s.retention_days, retUnit)}</Pill>
        </span>
      </Row>
      <Row label="Max sessions"
        hint="Cap the number of sessions kept on disk. Oldest are dropped first. 0 = unlimited."
        testid="row-maxsessions">
        <NumberBox value={s.max_sessions} onChange={v => mutate({ max_sessions: v })}
          min={0} max={10000} ariaLabel="max sessions" w={120} />
      </Row>
      <Row label="Storage usage"
        hint={`Currently ${fmtMB(s.runtime.db_size_bytes)} on disk.`}
        testid="row-usage">
        <div className="w-[340px] flex flex-col gap-1.5">
          <div
            data-testid="usage-bar"
            className="h-2 rounded-lg bg-muted border border-border overflow-hidden relative"
          >
            <div
              data-testid="usage-bar-fill"
              className="absolute left-0 top-0 bottom-0"
              style={{ width: `${usedPct}%`, background: barColor }}
            />
          </div>
          <div className="flex justify-between font-mono text-[10.5px] text-muted-foreground">
            <span>{fmtMB(s.runtime.db_size_bytes)} used</span>
            <span className="opacity-70">
              {s.max_db_size_mb > 0 ? `of ${s.max_db_size_mb.toLocaleString()} MB` : 'no cap'}
            </span>
          </div>
        </div>
        <button
          type="button"
          onClick={onPrune}
          className="px-3 h-[30px] rounded-md cursor-pointer bg-background border border-border font-sans text-xs text-foreground outline-hidden"
        >spaniel prune</button>
      </Row>

      {/* Per-table stacked bar breakdown */}
      <Row label="Breakdown" hint="Estimated bytes per table from DuckDB's internal catalogue." testid="row-breakdown">
        <div className="w-[340px] flex flex-col gap-2">
          <div
            data-testid="breakdown-bar"
            className="h-3 rounded-lg bg-muted border border-border overflow-hidden flex"
          >
            {totalBytes > 0 && TABLE_SEGMENTS.map(seg => {
              const tbl = breakdown?.tables.find(t => t.name === seg.key)
              const pct = tbl ? (tbl.approx_bytes / totalBytes) * 100 : 0
              if (pct < 0.5) return null
              return (
                <div
                  key={seg.key}
                  data-testid={`breakdown-seg-${seg.key}`}
                  title={`${seg.label}: ${fmtMB(tbl?.approx_bytes ?? 0)}`}
                  style={{ width: `${pct}%`, background: seg.color }}
                />
              )
            })}
          </div>
          <div className="flex gap-2.5 flex-wrap font-mono text-[10px] text-muted-foreground">
            {TABLE_SEGMENTS.map(seg => {
              const tbl = breakdown?.tables.find(t => t.name === seg.key)
              return (
                <span key={seg.key} className="flex items-center gap-1">
                  <span className="w-2 h-2 rounded-sm inline-block" style={{ background: seg.color }} />
                  {seg.label}
                  {tbl ? ` · ${fmtMB(tbl.approx_bytes)}` : ''}
                </span>
              )
            })}
          </div>

          {/* Top sessions by size */}
          {breakdown && breakdown.sessions.length > 0 && (
            <div
              data-testid="sessions-breakdown"
              className="mt-1 flex flex-col gap-[3px] font-mono text-[10.5px] text-muted-foreground"
            >
              <div className="text-[10px] uppercase tracking-[0.05em] opacity-70 mb-0.5">
                Top sessions by size
              </div>
              {breakdown.sessions.slice(0, 5).map(ss => (
                <div key={ss.id} className="flex justify-between">
                  <span className="max-w-[220px] overflow-hidden text-ellipsis whitespace-nowrap">
                    {ss.label || ss.id.slice(0, 8)}
                  </span>
                  <span className="opacity-70">{fmtMB(ss.approx_bytes)} · {ss.span_count} spans</span>
                </div>
              ))}
            </div>
          )}
        </div>
      </Row>

      {/* Compact button */}
      <Row label="Compact" hint="CHECKPOINT + VACUUM — returns free pages to the OS." testid="row-compact">
        <button
          type="button"
          data-testid="compact-btn"
          disabled={compacting}
          onClick={handleCompact}
          className={`px-3.5 h-[30px] rounded-md bg-background border border-border font-sans text-xs text-foreground outline-hidden ${compacting ? 'cursor-default opacity-60' : 'cursor-pointer opacity-100'}`}
        >
          {compacting ? 'compacting…' : 'compact now'}
        </button>
        {compactMsg && (
          <span
            data-testid="compact-msg"
            className={`font-mono text-[11px] ${compactMsg === 'Done' ? 'text-[#3e6a3e]' : 'text-danger'}`}
          >
            {compactMsg}
          </span>
        )}
      </Row>

      <Row danger label="Drop all data"
        hint="Wipe the database and start over. Cannot be undone."
        testid="row-drop">
        <button
          type="button"
          onClick={onDrop}
          className="px-3.5 h-[30px] rounded-md cursor-pointer bg-transparent border border-danger text-danger font-sans text-xs font-semibold outline-hidden"
        >drop &amp; recreate</button>
      </Row>
    </Card>
  )
}

function AboutSection({ s, hidden }: { s: SettingsT; hidden: boolean }) {
  return (
    <Card id="about" title="About" sub="Build, license, source" hidden={hidden}>
      <Row label="Version" hint="Current binary build." testid="row-version">
        <code className="font-mono text-xs text-foreground">
          spaniel {s.runtime.version}
        </code>
        <Pill tone="accent">channel: stable</Pill>
      </Row>
      <Row label="License" hint="Spaniel is MIT-licensed." testid="row-license">
        <Pill>MIT</Pill>
        <a
          href="https://github.com/zfogg/spaniel"
          target="_blank"
          rel="noreferrer"
          className="font-sans text-xs text-accent-d underline decoration-dotted"
        >view source</a>
      </Row>
      <Row label="Config file" hint="All settings on this page are persisted to this YAML file."
        testid="row-config">
        <code className="font-mono text-xs text-foreground">
          {s.runtime.config_path}
        </code>
      </Row>
    </Card>
  )
}

// ── page ─────────────────────────────────────────────────────────────────────

type Section = 'network' | 'storage' | 'about'

export default function Settings() {
  const [data, setData] = useState<SettingsT | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [section, setSection] = useState<Section>('network')
  const [saving, setSaving] = useState(false)
  const [savedAt, setSavedAt] = useState<number | null>(null)
  const [breakdown, setBreakdown] = useState<StorageBreakdown | null>(null)

  useEffect(() => {
    let cancel = false
    api.settings.get()
      .then(r => { if (!cancel) setData(r.data) })
      .catch(e => { if (!cancel) setError(String(e)) })
    return () => { cancel = true }
  }, [])

  useEffect(() => {
    let cancel = false
    api.storage.get()
      .then(r => { if (!cancel) setBreakdown(r.data) })
      .catch(() => {})
    return () => { cancel = true }
  }, [])

  const mutate = useCallback(async (patch: SettingsUpdate) => {
    if (!data) return
    // Optimistic update so toggles & inputs feel snappy.
    setData(prev => prev ? { ...prev, ...patch } : prev)
    setSaving(true)
    setError(null)
    try {
      const r = await api.settings.update(patch)
      setData(r.data)
      setSavedAt(Date.now())
    } catch (e) {
      setError(String(e))
      // Refetch to reconcile.
      api.settings.get().then(r => setData(r.data)).catch(() => {})
    } finally {
      setSaving(false)
    }
  }, [data])

  const onPrune = useCallback(async () => {
    if (!confirm('Apply retention now? Drops old sessions according to your retention settings.')) return
    // We don't have a generic /prune endpoint exposed; just trigger a settings round-trip so the user gets feedback.
    // (Server runs retention on a timer and on startup; the CLI `spaniel prune` is the one-shot entrypoint.)
    alert('Retention runs automatically every hour and at startup. Run `spaniel prune` from the CLI for an immediate pass.')
  }, [])

  const onDrop = useCallback(async () => {
    if (!confirm('Drop ALL spans, logs, metrics, sessions, and lint warnings? This cannot be undone.')) return
    try {
      await api.settings.dropAllData()
      setSavedAt(Date.now())
    } catch (e) {
      setError(String(e))
    }
  }, [])

  const onCompact = useCallback(async () => {
    await api.settings.compact()
    // Refresh breakdown after compaction so sizes update.
    api.storage.get().then(r => setBreakdown(r.data)).catch(() => {})
  }, [])

  const sections = useMemo(() => ([
    { id: 'network' as const, label: 'Network' },
    { id: 'storage' as const, label: 'Storage · DuckDB' },
    { id: 'about'   as const, label: 'About' },
  ]), [])

  if (error && !data) {
    return (
      <div className="flex-1 flex items-center justify-center p-10">
        <div className="text-center max-w-[460px]">
          <div className="font-mono text-[13px] text-danger mb-2">
            Couldn't load settings
          </div>
          <div className="font-mono text-[11px] text-muted-foreground">{error}</div>
        </div>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">
        Loading settings…
      </div>
    )
  }

  return (
    <div className="flex-1 flex min-h-0 overflow-hidden">
      {/* sidebar */}
      <div className="w-[220px] border-r border-border bg-muted py-3.5 px-3 flex flex-col gap-4 shrink-0">
        <div>
          <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-2">settings</div>
          {sections.map(sec => {
            const on = section === sec.id
            return (
              <button
                key={sec.id}
                type="button"
                onClick={() => setSection(sec.id)}
                className={`w-full flex items-center gap-1.5 py-1.5 px-2 rounded-md cursor-pointer text-foreground font-mono text-[11px] outline-hidden text-left mb-0.5 ${on ? 'bg-background border border-border' : 'bg-transparent border border-transparent'}`}
              >
                {sec.label}
              </button>
            )
          })}
        </div>

        <div>
          <div className="font-mono text-[9px] text-muted-foreground uppercase tracking-[0.14em] mb-2">config file</div>
          <div className="font-mono text-[10.5px] text-foreground py-[5px] px-2 bg-background border border-border rounded-md leading-[1.4] break-all">{data.runtime.config_path}</div>
        </div>

        <div className="flex-1" />

        <div
          data-testid="daemon-pill"
          className="py-1.5 px-2 rounded-md bg-[#dff0e0] border border-[#9bc4a4] font-mono text-[10.5px] text-[#3e6a3e] flex flex-col gap-0.5"
        >
          <span>
            <span className="w-1.5 h-1.5 rounded-full bg-[#3e6a3e] inline-block mr-1.5" />
            daemon running
          </span>
          <span className="opacity-70">pid {data.runtime.pid} · {fmtUptime(data.runtime.uptime_ns)}</span>
        </div>
      </div>

      {/* main */}
      <div className="flex-1 overflow-x-hidden overflow-y-auto pt-5 px-6 pb-10">
        <div className="mb-[18px]">
          <div className="font-mono text-[10px] text-muted-foreground uppercase tracking-[0.18em] mb-1.5">preferences</div>
          <h1 className="m-0 font-serif text-[28px] font-semibold tracking-[-0.02em] text-foreground">Settings</h1>
          <div className="font-sans text-[13px] text-muted-foreground mt-1 max-w-[640px]">
            Saved to <code className="font-mono text-xs text-foreground bg-muted py-px px-1.5 rounded">{data.runtime.config_path}</code>. Port changes take effect after restart.
          </div>
        </div>

        {/* save state strip */}
        <div data-testid="save-state" className="mb-3 min-h-[18px] font-mono text-[11px]">
          {error && <span className="text-danger">✗ {error}</span>}
          {!error && saving && <span className="text-muted-foreground">saving…</span>}
          {!error && !saving && savedAt && <span className="text-[#3e6a3e]">✓ saved</span>}
        </div>

        <NetworkSection s={data} mutate={mutate} hidden={section !== 'network'} />
        <StorageSection s={data} mutate={mutate} hidden={section !== 'storage'}
          onPrune={onPrune} onDrop={onDrop} breakdown={breakdown} onCompact={onCompact} />
        <AboutSection s={data} hidden={section !== 'about'} />
      </div>
    </div>
  )
}
