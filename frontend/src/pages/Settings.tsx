import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FormProvider, useForm, useFormContext, useFormState } from 'react-hook-form'
// zod 4 implements the Standard Schema spec, so we use the standard-schema
// resolver (the default zodResolver entry still targets zod 3 typings).
import { standardSchemaResolver } from '@hookform/resolvers/standard-schema'
import { qk } from '@/lib/query'
import { SettingsFormSchema, pickFormValues, type SettingsFormValues } from '@/lib/settings-form'
import { api, type Settings as SettingsT, type SettingsUpdate, type StorageBreakdown, type UpdateCheckResult } from '@/lib/api'

// Inline validation message for an editable settings field. Reads RHF's
// per-field error (populated by the zod resolver) via context, so the section
// components don't need the form threaded through them. `basis-full` drops it
// onto its own line below the input within the flex-wrap Row.
function FieldError({ name }: { name: keyof SettingsFormValues }) {
  const { control } = useFormContext<SettingsFormValues>()
  const { errors } = useFormState({ control, name })
  const msg = errors[name]?.message
  if (!msg) return null
  return (
    <span data-testid={`err-${name}`} className="basis-full font-mono text-[11px] text-danger">
      {String(msg)}
    </span>
  )
}

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

// Bind address commits on blur / Enter rather than per-keystroke: the value is
// validated server-side, and partial input ("192.168.1") would otherwise be
// rejected and reverted mid-typing.
function BindAddressBox({ value, onCommit, ariaLabel, placeholder }: {
  value: string; onCommit: (v: string) => void; ariaLabel: string; placeholder?: string
}) {
  const [draft, setDraft] = useState(value)
  useEffect(() => { setDraft(value) }, [value])
  const commit = () => { if (draft !== value) onCommit(draft.trim()) }
  return (
    <span
      className="inline-flex items-center bg-white dark:bg-background border border-border rounded-md px-2.5 h-[30px] max-w-full"
      style={{ width: 200 }}
    >
      <input
        type="text"
        aria-label={ariaLabel}
        placeholder={placeholder}
        value={draft}
        onChange={e => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }}
        className="flex-1 min-w-0 border-none outline-hidden bg-transparent text-xs text-foreground font-mono"
      />
    </span>
  )
}

function bindAddressLabel(addr: string | undefined): string {
  switch ((addr ?? '').trim()) {
    case '':
      return 'disabled'
    case '0.0.0.0':
    case '::':
      return 'all interfaces'
    case '127.0.0.1':
    case '::1':
      return 'localhost only'
    default:
      return 'custom'
  }
}

function NumberBox({ value, onChange, min, max, step, w = 110, suffix, ariaLabel }: {
  value: number; onChange: (v: number) => void
  min?: number; max?: number; step?: number; w?: number; suffix?: string; ariaLabel: string
}) {
  return (
    <span
      className="inline-flex items-center bg-white dark:bg-background border border-border rounded-md px-2 h-[30px]"
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
      className="inline-flex items-center bg-white dark:bg-background border border-border rounded-md px-2.5 h-[30px] max-w-full"
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
      className="inline-flex items-center bg-white dark:bg-background border border-border rounded-md h-[30px] pr-1.5"
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

function Pill({ tone = 'neutral', children }: { tone?: 'neutral' | 'ok' | 'warn' | 'danger' | 'accent'; children: React.ReactNode }) {
  const tones = {
    neutral: { bg: 'var(--muted)', fg: 'var(--muted-foreground)', bd: 'var(--border)' },
    ok:      { bg: '#dff0e0', fg: '#3e6a3e', bd: '#9bc4a4' },
    warn:    { bg: '#fcefcf', fg: '#8a6118', bd: '#d9b878' },
    danger:  { bg: '#fde8e8', fg: '#922020', bd: '#e0a0a0' },
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

function Row({ label, hint, danger, children, testid, align = 'start' }: {
  label: string; hint?: string; danger?: boolean; testid?: string
  align?: 'start' | 'center'
  children: React.ReactNode
}) {
  return (
    <div
      data-testid={testid}
      className={`grid gap-[18px] py-3.5 border-b border-border last:border-b-0 ${align === 'center' ? 'items-center' : 'items-start'}`}
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

function ForwarderStatusRows() {
  const { data: statuses = [] } = useQuery({
    queryKey: qk.forwarders(),
    queryFn: () => api.forwarders.list().then(r => r.data),
    refetchInterval: 5000,
  })

  const active = statuses.filter(s => (s.pending_bytes ?? 0) > 0 || (s.dropped_spool ?? 0) > 0)
  if (active.length === 0) return null

  return (
    <>
      {active.map(s => {
        const host = (() => { try { return new URL(s.url).host } catch { return s.url } })()
        const pending = s.pending_bytes ?? 0
        const dropped = s.dropped_spool ?? 0
        return (
          <Row
            key={s.url}
            label={`→ ${host} spool`}
            hint={`Disk-backed retry buffer for upstream ${s.url}.`}
          >
            {pending > 0 && <Pill tone="warn">{fmtSettingsBytes(pending)} queued</Pill>}
            {dropped > 0 && <Pill tone="danger">{dropped} dropped</Pill>}
            {pending === 0 && dropped === 0 && <Pill tone="ok">empty</Pill>}
          </Row>
        )
      })}
    </>
  )
}

function fmtSettingsBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

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
        <FieldError name="otlp_grpc_port" />
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
        <FieldError name="otlp_http_port" />
      </Row>
      <Row label="IPv4 bind address"
        hint="IPv4 address the UI and OTLP receivers listen on. 127.0.0.1 = localhost; 0.0.0.0 = all interfaces (LAN / docker). Leave blank to disable IPv4."
        testid="row-bind-v4">
        <BindAddressBox
          value={s.bind_address_v4 ?? ''}
          onCommit={v => mutate({ bind_address_v4: v })}
          ariaLabel="ipv4 bind address" placeholder="127.0.0.1"
        />
        <Pill>{bindAddressLabel(s.bind_address_v4)}</Pill>
        <FieldError name="bind_address_v4" />
      </Row>
      <Row label="IPv6 bind address"
        hint="IPv6 address the UI and OTLP receivers listen on. ::1 = localhost; :: = all interfaces. Served alongside IPv4 (dual-stack). Leave blank to disable IPv6."
        testid="row-bind-v6">
        <BindAddressBox
          value={s.bind_address_v6 ?? ''}
          onCommit={v => mutate({ bind_address_v6: v })}
          ariaLabel="ipv6 bind address" placeholder="::1"
        />
        <Pill>{bindAddressLabel(s.bind_address_v6)}</Pill>
        <FieldError name="bind_address_v6" />
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
        <FieldError name="forward_sample" />
      </Row>
      <Row label="UI / API port"
        hint="HTTP port the spaniel UI and JSON API are served on. Restart required."
        testid="row-port">
        <NumberBox value={s.port} onChange={v => mutate({ port: v })}
          min={1} max={65535} ariaLabel="ui port" suffix=":port" />
        <FieldError name="port" />
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
        <FieldError name="forward" />
      </Row>
      <ForwarderStatusRows />
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
        <FieldError name="db_path" />
      </Row>
      <Row label="Max database size"
        hint="Retention drops the oldest sessions when the file grows past this cap. 0 = unlimited."
        testid="row-maxsize">
        <NumberBox value={s.max_db_size_mb} onChange={v => mutate({ max_db_size_mb: v })}
          min={0} max={102400} suffix="MB" w={130} ariaLabel="max db size mb" />
        <FieldError name="max_db_size_mb" />
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
        <FieldError name="retention_days" />
      </Row>
      <Row label="Max sessions"
        hint="Cap the number of sessions kept on disk. Oldest are dropped first. 0 = unlimited."
        testid="row-maxsessions">
        <NumberBox value={s.max_sessions} onChange={v => mutate({ max_sessions: v })}
          min={0} max={10000} ariaLabel="max sessions" w={120} />
        <FieldError name="max_sessions" />
      </Row>
      <Row label="Per-source rate limit"
        hint="Max spans/sec accepted per service.name. Excess spans are dropped and counted in the Sources panel. 0 = unlimited."
        testid="row-source-rps">
        <NumberBox value={s.source_rps} onChange={v => mutate({ source_rps: v })}
          min={0} max={1000000} step={0.1} ariaLabel="source rps" w={120} />
        <span className="font-mono text-[11px] text-muted-foreground">spans / s&nbsp;&nbsp;(0 = unlimited)</span>
        <FieldError name="source_rps" />
      </Row>
      <Row label="Per-source burst"
        hint="Token bucket capacity per service. Allows short spikes above the rate limit. 0 = rate limit × 5."
        testid="row-source-burst">
        <NumberBox value={s.source_burst} onChange={v => mutate({ source_burst: v })}
          min={0} max={1000000} ariaLabel="source burst" w={120} />
        <span className="font-mono text-[11px] text-muted-foreground">spans&nbsp;&nbsp;(0 = rps × 5)</span>
        <FieldError name="source_burst" />
      </Row>
      <Row label="Self-monitor"
        hint="Send Spaniel's own traces and metrics to itself via its OTLP gRPC port. Enables the dogfood loop: see Spaniel's request latency, DB query times, and ingest spans in the Traces and Metrics tabs. Takes effect immediately."
        testid="row-self-monitor">
        <Toggle on={s.self_monitor} onChange={v => mutate({ self_monitor: v })} label="self monitor" />
        {s.self_monitor && (
          <span className="font-mono text-[11px] text-muted-foreground">
            → localhost:{s.runtime.otlp_grpc_port}
          </span>
        )}
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
          className="px-3 h-[30px] rounded-md cursor-pointer bg-white dark:bg-background border border-border font-sans text-xs text-foreground outline-hidden"
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
          className={`px-3.5 h-[30px] rounded-md bg-white dark:bg-background border border-border font-sans text-xs text-foreground outline-hidden ${compacting ? 'cursor-default opacity-60' : 'cursor-pointer opacity-100'}`}
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
  const [updateResult, setUpdateResult] = useState<UpdateCheckResult | null>(null)
  const [checkingUpdate, setCheckingUpdate] = useState(false)

  const handleCheckUpdates = useCallback(async () => {
    setCheckingUpdate(true)
    try {
      const { data } = await api.settings.checkUpdates()
      setUpdateResult(data)
    } catch {
      setUpdateResult({ current: s.runtime.version, latest: '', channel: s.runtime.channel, is_outdated: false, release_notes_url: '', checked_at_ns: 0, error: "couldn't reach github" })
    } finally {
      setCheckingUpdate(false)
    }
  }, [s.runtime.version, s.runtime.channel])

  return (
    <Card id="about" title="About" sub="Build, license, source" hidden={hidden}>
      <Row label="Version" hint="Current binary build." testid="row-version">
        <code className="font-mono text-xs text-foreground">
          spaniel {s.runtime.version}
        </code>
        <Pill tone="accent">channel: {s.runtime.channel}</Pill>
        <button
          type="button"
          data-testid="check-updates-btn"
          disabled={checkingUpdate}
          onClick={handleCheckUpdates}
          className={`px-3 h-[28px] rounded-md bg-white dark:bg-background border border-border font-sans text-xs text-foreground outline-hidden ${checkingUpdate ? 'cursor-default opacity-60' : 'cursor-pointer'}`}
        >
          {checkingUpdate ? 'checking…' : 'check for updates'}
        </button>
        {updateResult && (
          <div data-testid="update-result" className="font-mono text-[11px]">
            {updateResult.error
              ? <span className="text-muted-foreground">couldn't reach github</span>
              : updateResult.is_outdated
              ? <a
                  href={updateResult.release_notes_url}
                  target="_blank"
                  rel="noreferrer"
                  className="text-accent-d underline decoration-dotted"
                >↑ {updateResult.latest} available</a>
              : <span className="text-[#3e6a3e]">✓ on latest ({updateResult.latest})</span>
            }
          </div>
        )}
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
      <Row label="Creator" hint="Who thought of spaniel?" testid="row-creator" align="center">
        <span className="font-sans text-xs text-foreground">Zachary Fogg</span>
        <a
          href="mailto:me@zfo.gg"
          className="font-sans text-xs text-accent-d underline decoration-dotted"
        >me@zfo.gg</a>
        <a
          href="https://zfo.gg"
          target="_blank"
          rel="noreferrer"
          className="font-sans text-xs text-accent-d underline decoration-dotted"
        >zfo.gg</a>
        <span className="font-sans text-xs text-muted-foreground">·</span>
        <span className="font-sans text-xs text-foreground">Claude</span>
        <a
          href="https://claude.ai"
          target="_blank"
          rel="noreferrer"
          className="font-sans text-xs text-accent-d underline decoration-dotted"
        >claude.ai</a>
      </Row>
    </Card>
  )
}

// ── page ─────────────────────────────────────────────────────────────────────

type Section = 'network' | 'storage' | 'about'

export default function Settings() {
  const queryClient = useQueryClient()
  const [localError, setLocalError] = useState<string | null>(null)
  const [section, setSection] = useState<Section>('network')
  const [saving, setSaving] = useState(false)
  const [savedAt, setSavedAt] = useState<number | null>(null)

  const settingsQuery = useQuery({
    queryKey: qk.settings(),
    queryFn: () => api.settings.get().then(r => r.data),
  })
  const data = settingsQuery.data ?? null
  const error = localError ?? (settingsQuery.isError ? String(settingsQuery.error) : null)

  const { data: breakdown = null } = useQuery({
    queryKey: qk.storage(),
    queryFn: () => api.storage.get().then(r => r.data),
  })

  // Advisory validation: the form mirrors the (optimistically-updated) server
  // data, so editing a field reflects into RHF and surfaces inline zod errors.
  // Saving still goes through `mutate` unconditionally — the server remains the
  // source of truth for rejections.
  const formValues = useMemo(() => (data ? pickFormValues(data) : undefined), [data])
  const form = useForm<SettingsFormValues>({
    resolver: standardSchemaResolver(SettingsFormSchema),
    values: formValues,
    mode: 'onChange',
  })
  // Re-validate whenever the synced values change so errors track the inputs.
  useEffect(() => { if (formValues) form.trigger() }, [formValues, form])

  const mutate = useCallback(async (patch: SettingsUpdate) => {
    if (!data) return
    // Optimistic cache update so toggles & inputs feel snappy.
    queryClient.setQueryData<SettingsT>(qk.settings(), prev => prev ? { ...prev, ...patch } : prev)
    setSaving(true)
    setLocalError(null)
    try {
      const r = await api.settings.update(patch)
      queryClient.setQueryData(qk.settings(), r.data)
      setSavedAt(Date.now())
    } catch (e) {
      setLocalError(String(e))
      // Refetch to reconcile.
      queryClient.invalidateQueries({ queryKey: qk.settings() })
    } finally {
      setSaving(false)
    }
  }, [data, queryClient])

  const onPrune = useCallback(async () => {
    if (!confirm('Apply retention now? Drops old sessions according to your retention settings.')) return
    try {
      const { data: res } = await api.settings.prune()
      const deleted = res.deleted_by_age + res.deleted_by_count + res.deleted_by_size
      // Refresh the settings card + storage breakdown so db size / counts
      // reflect the prune we just ran.
      queryClient.invalidateQueries({ queryKey: qk.settings() })
      queryClient.invalidateQueries({ queryKey: qk.storage() })
      queryClient.invalidateQueries({ queryKey: qk.sessions() })
      alert(deleted > 0
        ? `Pruned ${deleted} session${deleted === 1 ? '' : 's'} `
          + `(age ${res.deleted_by_age}, count ${res.deleted_by_count}, size ${res.deleted_by_size}). `
          + `${res.final_sessions} remain, ${(res.final_db_size_bytes / 1_048_576).toFixed(1)} MB on disk.`
        : `Nothing to prune — ${res.final_sessions} session${res.final_sessions === 1 ? '' : 's'} within policy.`)
    } catch (e) {
      alert(`Prune failed: ${e instanceof Error ? e.message : String(e)}`)
    }
  }, [queryClient])

  const onDrop = useCallback(async () => {
    if (!confirm('Drop ALL spans, logs, metrics, sessions, and lint warnings? This cannot be undone.')) return
    try {
      await api.settings.dropAllData()
      setSavedAt(Date.now())
      // Everything was wiped — refresh all cached views.
      queryClient.invalidateQueries()
    } catch (e) {
      setLocalError(String(e))
    }
  }, [queryClient])

  const onCompact = useCallback(async () => {
    await api.settings.compact()
    // Refresh breakdown after compaction so sizes update.
    queryClient.invalidateQueries({ queryKey: qk.storage() })
  }, [queryClient])

  const sections = useMemo(() => ([
    { id: 'network' as const, label: 'Network' },
    { id: 'storage' as const, label: 'Storage' },
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
    <FormProvider {...form}>
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
    </FormProvider>
  )
}
