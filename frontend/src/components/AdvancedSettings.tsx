import { useEffect, useState } from 'react'
import type { Settings, SettingsUpdate } from '@/lib/api'

type Props = {
  s: Settings
  mutate: (patch: SettingsUpdate) => void
  tab: 'general' | 'network' | 'ingestion' | 'security' | 'notifications'
  hidden: boolean
}
const inputClass = 'w-full max-w-lg rounded border border-border bg-background px-3 py-2 text-sm'
function TextSetting({
  label,
  value,
  onSave,
  type = 'text',
  hint,
}: {
  label: string
  value: string | number
  onSave: (v: string) => void
  type?: string
  hint?: string
}) {
  const [draft, setDraft] = useState(String(value))
  useEffect(() => setDraft(String(value)), [value])
  return (
    <label className="grid gap-1 text-sm">
      <span>{label}</span>
      <input
        aria-label={label}
        type={type}
        className={inputClass}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => {
          if (draft !== String(value)) onSave(draft)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter') e.currentTarget.blur()
        }}
      />
      {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
    </label>
  )
}
function ToggleSetting({
  label,
  value,
  onSave,
}: {
  label: string
  value: boolean
  onSave: (v: boolean) => void
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <input
        type="checkbox"
        aria-label={label}
        checked={value}
        onChange={(e) => onSave(e.target.checked)}
      />
      {label}
    </label>
  )
}
export function AdvancedSettings({ s, mutate, tab, hidden }: Props) {
  const [secret, setSecret] = useState({
    forward_bearer_token: '',
    forward_tls_ca: '',
    forward_tls_cert: '',
    forward_tls_key: '',
  })
  const [permission, setPermission] = useState(() =>
    typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
  )
  const text = (
    key: keyof SettingsUpdate,
    label: string,
    fallback: string | number = '',
    hint?: string,
  ) => (
    <TextSetting
      key={key}
      label={label}
      value={String(s[key as keyof Settings] ?? fallback)}
      onSave={(v) => mutate({ [key]: v })}
      hint={hint}
    />
  )
  const number = (key: keyof SettingsUpdate, label: string, fallback: number, hint?: string) => (
    <TextSetting
      key={key}
      label={label}
      type="number"
      value={Number(s[key as keyof Settings] ?? fallback)}
      onSave={(v) => mutate({ [key]: Number(v) })}
      hint={hint}
    />
  )
  const toggle = (key: keyof SettingsUpdate, label: string) => (
    <ToggleSetting
      key={key}
      label={label}
      value={Boolean(s[key as keyof Settings])}
      onSave={(v) => mutate({ [key]: v })}
    />
  )
  return (
    <section hidden={hidden} className="mb-4 rounded-lg border border-border bg-card p-5">
      <h2 className="mb-4 text-lg font-semibold">
        {
          {
            general: 'Display defaults',
            network: 'Advertised OTLP endpoints',
            ingestion: 'Ingestion policy',
            security: 'Forwarding security',
            notifications: 'Notifications',
          }[tab]
        }
      </h2>
      <div className="grid gap-5">
        {tab === 'general' && (
          <>
            <label className="grid gap-1 text-sm">
              Default time range
              <select
                aria-label="Default time range"
                className={inputClass}
                value={s.default_time_range ?? '1h'}
                onChange={(e) => mutate({ default_time_range: e.target.value })}
              >
                {['15m', '1h', '24h', 'all'].map((v) => (
                  <option key={v} value={v}>
                    {v}
                  </option>
                ))}
              </select>
            </label>
            {number(
              'refresh_interval_seconds',
              'Refresh interval (seconds)',
              5,
              '0 disables periodic refresh. Live telemetry still arrives over WebSocket.',
            )}
          </>
        )}
        {tab === 'network' && (
          <>
            {text(
              'otlp_http_url',
              'Advertised OTLP HTTP URL',
              '',
              'Optional full exporter URL, including external port. Blank uses public hostname and live HTTP port.',
            )}
            {text(
              'otlp_grpc_url',
              'Advertised OTLP gRPC URL',
              '',
              'Optional full exporter URL. Blank uses public hostname and live gRPC port.',
            )}
          </>
        )}
        {tab === 'ingestion' && (
          <>
            <p className="text-sm text-muted-foreground">
              Filters affect new telemetry before storage and forwarding. Retention expires existing
              telemetry on the next minute pass and preserves baselines.
            </p>
            {(['services', 'routes'] as const).map((kind) => (
              <TextSetting
                key={kind}
                label={`Excluded ${kind}`}
                value={(s[`ingestion_excluded_${kind}`] ?? []).join(', ')}
                onSave={(v) =>
                  mutate({
                    [`ingestion_excluded_${kind}`]: v
                      .split(',')
                      .map((x) => x.trim())
                      .filter(Boolean),
                  })
                }
                hint={
                  kind === 'services'
                    ? 'Comma-separated exact service.name values; applies to traces, logs, and metrics.'
                    : 'Comma-separated exact http.route values; applies to spans.'
                }
              />
            ))}
            {number(
              'source_rps',
              'Per-source span rate (per second)',
              0,
              '0 is unlimited. Excess spans are counted in Sources.',
            )}
            {number(
              'source_burst',
              'Per-source span burst',
              0,
              '0 uses five seconds of the configured rate.',
            )}
            {number(
              'ingestion_max_request_mb',
              'Maximum request size (MB)',
              64,
              'HTTP and decoded exports apply immediately; gRPC wire limit updates after restart.',
            )}
            {number('ingestion_queue_requests', 'Maximum queued requests', 256)}
            {number('ingestion_queue_mb', 'Maximum queue size (MB)', 64)}
            <dl aria-label="Ingestion status" className="grid grid-cols-2 gap-2 text-sm">
              {Object.entries(s.ingestion_status ?? {}).map(([k, v]) => (
                <div key={k}>
                  <dt>{k.replace(/_/g, ' ')}</dt>
                  <dd className="font-mono">{v}</dd>
                </div>
              ))}
            </dl>
            {(['traces', 'logs', 'metrics'] as const).map((signal) => (
              <div key={signal} className="grid gap-2">
                {toggle(`${signal}_retention_enabled`, `${signal} retention enabled`)}
                {number(
                  `${signal}_retention_days`,
                  `${signal} retention (days)`,
                  7,
                  'Disabled uses the existing session retention policy.',
                )}
              </div>
            ))}
          </>
        )}
        {tab === 'security' && (
          <>
            <p className="text-sm text-muted-foreground">
              Shared authentication and TLS for upstream HTTP forwarding. Blank secret inputs keep
              existing values. Saved secrets are never returned by the API. Endpoint-list changes
              require restart.
            </p>
            {(
              [
                'forward_bearer_token',
                'forward_tls_ca',
                'forward_tls_cert',
                'forward_tls_key',
              ] as const
            ).map((k) => (
              <label key={k} className="grid gap-1 text-sm">
                {
                  {
                    forward_bearer_token: 'Upstream bearer token',
                    forward_tls_ca: 'Trusted CA bundle (PEM)',
                    forward_tls_cert: 'Client certificate (PEM)',
                    forward_tls_key: 'Client private key (PEM)',
                  }[k]
                }{' '}
                · {s[`${k}_set`] ? 'configured' : 'not configured'}
                <textarea
                  aria-label={k}
                  autoComplete="off"
                  className={inputClass}
                  value={secret[k]}
                  onChange={(e) => setSecret({ ...secret, [k]: e.target.value })}
                />
              </label>
            ))}
            <div className="flex flex-wrap gap-3">
              <button
                type="button"
                onClick={() => {
                  const patch: SettingsUpdate = {}
                  if (secret.forward_bearer_token)
                    patch.forward_bearer_token = secret.forward_bearer_token
                  if (secret.forward_tls_ca) patch.forward_tls_ca = secret.forward_tls_ca
                  if (secret.forward_tls_cert || secret.forward_tls_key) {
                    patch.forward_tls_cert = secret.forward_tls_cert
                    patch.forward_tls_key = secret.forward_tls_key
                  }
                  mutate(patch)
                  setSecret({
                    forward_bearer_token: '',
                    forward_tls_ca: '',
                    forward_tls_cert: '',
                    forward_tls_key: '',
                  })
                }}
              >
                Save forwarding secrets
              </button>
              <button type="button" onClick={() => mutate({ forward_bearer_token: '' })}>
                Clear forwarding token
              </button>
              <button
                type="button"
                onClick={() =>
                  mutate({ forward_tls_ca: '', forward_tls_cert: '', forward_tls_key: '' })
                }
              >
                Clear forwarding TLS
              </button>
            </div>
          </>
        )}
        {tab === 'notifications' && (
          <>
            {toggle('alerts_browser_enabled', 'Browser notifications for alerts')}
            <button
              type="button"
              disabled={permission === 'unsupported' || permission === 'denied'}
              onClick={async () => {
                setPermission(await Notification.requestPermission())
              }}
            >
              Allow browser notifications
            </button>
            <p className="text-xs text-muted-foreground">
              Browser permission: {permission}. Denied permissions must be changed in browser site
              settings.
            </p>
            {toggle('alerts_pushover_enabled', 'Pushover notifications')}
            {toggle('notification_quiet_enabled', 'Enable quiet hours')}
            {text(
              'notification_quiet_start',
              'Quiet hours start',
              '22:00',
              'HH:MM; equal start and end silences the entire day.',
            )}
            {text('notification_quiet_end', 'Quiet hours end', '08:00')}
            {text(
              'notification_timezone',
              'Notification timezone',
              'UTC',
              'IANA timezone, for example America/New_York.',
            )}
            {number(
              'notification_cooldown_seconds',
              'Minimum notification cooldown (seconds)',
              0,
              'Applies per alert instance and channel. Per-rule cooldowns still apply.',
            )}
          </>
        )}
      </div>
    </section>
  )
}
