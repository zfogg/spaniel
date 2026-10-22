import { useQuery } from '@tanstack/react-query'

import { useSearchParams } from 'react-router-dom'

import { api } from '@/lib/api'

import { qk } from '@/lib/query'

import { serviceSwatch } from '@/lib/service-color'

export type TelemetrySignal = 'traces' | 'spans' | 'logs' | 'metrics'

export default function ServiceFilter({
  signal,
  selected,
  onChange,
  sessionId,
  compact = false,
}: {
  signal: TelemetrySignal

  selected: string[]

  onChange: (services: string[]) => void

  sessionId?: string | null

  compact?: boolean
}) {
  const [params] = useSearchParams()

  const session = sessionId ?? params.get('session') ?? undefined

  const {
    data: services = [],
    isLoading,
    isError,
    refetch,
  } = useQuery({
    queryKey: qk.services(signal, session),

    queryFn: () => api.services.list({ signal, sessionId: session }).then((r) => r.data ?? []),
  })

  const choices = services

  const content = (
    <fieldset aria-label="Services" className="flex min-w-0 flex-col gap-1 text-xs">
      <legend className="mb-2 text-[10px] uppercase tracking-wider text-muted-foreground">
        Services
      </legend>
      <button
        type="button"
        onClick={() => onChange([])}
        aria-pressed={selected.length === 0}

        className="rounded px-2 py-1 text-left text-muted-foreground hover:bg-muted aria-pressed:bg-muted aria-pressed:text-foreground"
      >
        All services
      </button>
      <div className="max-h-64 overflow-y-auto">
        {choices.map((service) => (
          <label
            key={service}
            className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-muted"
          >
            <input
              type="checkbox"
              aria-label={service || '(unnamed service)'}
              checked={selected.includes(service)}

              onChange={() =>
                onChange(
                  selected.includes(service)
                    ? selected.filter((s) => s !== service)
                    : [...selected, service],
                )
              }

              className="shrink-0 accent-[var(--accent)]"
            />
            <span
              className="h-1.5 w-1.5 shrink-0 rounded-full"
              style={{ background: serviceSwatch(service) }}
            />
            <span className="truncate" title={service}>
              {service || '(unnamed service)'}
            </span>
          </label>
        ))}
      </div>
      {isLoading && <span className="px-2 text-muted-foreground">Loading services…</span>}
      {isError && (
        <button type="button" onClick={() => refetch()}>
          Retry loading services
        </button>
      )}
      {!isLoading && !isError && choices.length === 0 && (
        <span className="px-2 text-muted-foreground">No services in this session</span>
      )}
    </fieldset>
  )

  if (!compact) return content

  return (
    <details className="relative text-xs">
      <summary className="cursor-pointer rounded border border-border bg-muted px-2 py-1.5">
        {selected.length ? `${selected.length} services selected` : 'All services'}
      </summary>
      <div className="absolute left-0 top-full z-50 mt-1 w-64 rounded border border-border bg-background p-3 shadow-lg">
        {content}
      </div>
    </details>
  )
}
