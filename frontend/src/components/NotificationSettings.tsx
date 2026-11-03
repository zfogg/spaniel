import { useEffect, useState } from 'react'
import { DateInput, DateSegment, Label, TimeField } from 'react-aria-components'
import { parseTime, type Time } from '@internationalized/date'
import { timeZonesNames } from '@vvo/tzdb'
import { Button } from '@/components/ui/button'
import { Toggle } from '@/components/ui/toggle'
import type { Settings, SettingsUpdate } from '@/lib/api'

const zones = [...new Set(['UTC', ...timeZonesNames])].sort()

function SwitchSetting({
  label,
  value,
  onChange,
}: {
  label: string
  value: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <label className="flex items-center gap-3 text-sm cursor-pointer">
      <Toggle label={label} on={value} onChange={onChange} />
      <span>{label}</span>
    </label>
  )
}

function QuietTime({
  label,
  value,
  onSave,
}: {
  label: string
  value: string
  onSave: (value: string) => void
}) {
  const [draft, setDraft] = useState<Time | null>(() => parseTime(value))
  useEffect(() => setDraft(parseTime(value)), [value])
  return (
    <TimeField
      hourCycle={24}
      granularity="minute"
      shouldForceLeadingZeros
      value={draft}
      onChange={setDraft}
      onBlur={(event) => {
        if (event.currentTarget.contains(event.relatedTarget as Node | null)) return
        if (!draft) {
          setDraft(parseTime(value))
          return
        }
        const next = draft.toString().slice(0, 5)
        if (next !== value) onSave(next)
      }}
      className="grid gap-1.5"
    >
      <Label className="text-sm">{label}</Label>
      <DateInput
        aria-label={label}
        className="flex w-32 items-center rounded-md border border-border bg-background px-3 py-2 font-mono text-sm focus-within:ring-2 focus-within:ring-ring/50"
      >
        {(segment) => (
          <DateSegment
            segment={segment}
            className="rounded px-0.5 outline-none focus:bg-accent focus:text-accent-foreground data-[placeholder]:text-muted-foreground"
          />
        )}
      </DateInput>
    </TimeField>
  )
}

export function NotificationSettings({
  s,
  mutate,
}: {
  s: Settings
  mutate: (patch: SettingsUpdate) => void
}) {
  const [permission, setPermission] = useState(() =>
    typeof Notification === 'undefined' ? 'unsupported' : Notification.permission,
  )
  const [permissionError, setPermissionError] = useState('')
  const [cooldown, setCooldown] = useState(String(s.notification_cooldown_seconds ?? 0))
  useEffect(
    () => setCooldown(String(s.notification_cooldown_seconds ?? 0)),
    [s.notification_cooldown_seconds],
  )
  const timezone = s.notification_timezone || 'Local'
  return (
    <div className="grid gap-6 max-w-2xl">
      <div className="grid gap-4">
        <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
          <SwitchSetting
            label="Browser notifications for alerts"
            value={Boolean(s.alerts_browser_enabled)}
            onChange={(value) => mutate({ alerts_browser_enabled: value })}
          />
          <Button
            variant="outline"
            size="sm"
            disabled={permission !== 'default'}
            onClick={async () => {
              try {
                setPermissionError('')
                setPermission(await Notification.requestPermission())
              } catch (error) {
                setPermissionError(String(error))
              }
            }}
          >
            Allow browser notifications
          </Button>
        </div>
        <p className="-mt-2 text-xs text-muted-foreground">
          Browser permission: {permission}.{' '}
          {permission === 'denied'
            ? 'Allow notifications in your browser’s site settings to enable them.'
            : 'Permission applies to this browser.'}
        </p>
        {permissionError && (
          <p role="alert" className="text-xs text-danger">
            {permissionError}
          </p>
        )}
        <SwitchSetting
          label="Pushover notifications"
          value={Boolean(s.alerts_pushover_enabled)}
          onChange={(value) => mutate({ alerts_pushover_enabled: value })}
        />
      </div>

      <div className="grid gap-4 border-t border-border pt-5">
        <SwitchSetting
          label="Enable quiet hours"
          value={Boolean(s.notification_quiet_enabled)}
          onChange={(value) => mutate({ notification_quiet_enabled: value })}
        />
        <div className="flex flex-wrap gap-5">
          <QuietTime
            label="Quiet hours start"
            value={s.notification_quiet_start || '22:00'}
            onSave={(value) => mutate({ notification_quiet_start: value })}
          />
          <QuietTime
            label="Quiet hours end"
            value={s.notification_quiet_end || '08:00'}
            onSave={(value) => mutate({ notification_quiet_end: value })}
          />
        </div>
        <p className="-mt-2 text-xs text-muted-foreground">
          Quiet hours can cross midnight. Matching start and end times silence the entire day.
        </p>
        <label className="grid gap-1.5 text-sm">
          <span>Notification timezone</span>
          <select
            aria-label="Notification timezone"
            className="w-full max-w-sm rounded-md border border-border bg-background px-3 py-2 text-sm"
            value={timezone}
            onChange={(event) => mutate({ notification_timezone: event.target.value })}
          >
            <option value="Local">
              Server timezone{s.runtime.timezone ? ` — ${s.runtime.timezone}` : ''} (default)
            </option>
            {timezone !== 'Local' && !zones.includes(timezone) && (
              <option value={timezone}>{timezone}</option>
            )}
            {zones.map((zone) => (
              <option key={zone} value={zone}>
                {zone.replace(/_/g, ' ')}
              </option>
            ))}
          </select>
          <span className="text-xs text-muted-foreground">
            Follows the machine running Spaniel until you select a timezone. Your selection is
            saved.
          </span>
        </label>
      </div>

      <label className="grid gap-1.5 border-t border-border pt-5 text-sm">
        <span>Minimum notification cooldown (seconds)</span>
        <input
          aria-label="Minimum notification cooldown (seconds)"
          type="number"
          min={0}
          max={86400}
          step={1}
          className="w-28 rounded-md border border-border bg-background px-3 py-2 font-mono text-sm"
          value={cooldown}
          onChange={(event) => setCooldown(event.target.value)}
          onBlur={() => {
            if (cooldown !== String(s.notification_cooldown_seconds ?? 0))
              mutate({ notification_cooldown_seconds: Number(cooldown) })
          }}
          onKeyDown={(event) => {
            if (event.key === 'Enter') event.currentTarget.blur()
          }}
        />
        <span className="text-xs text-muted-foreground">
          Applies per alert instance and channel. Per-rule cooldowns still apply.
        </span>
      </label>
    </div>
  )
}
