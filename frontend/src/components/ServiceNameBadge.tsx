import type { CSSProperties } from 'react'
import { serviceHue } from '@/lib/service-color'

export default function ServiceNameBadge({
  name,
  additionalCount = 0,
}: {
  name: string
  additionalCount?: number
}) {
  return (
    <span
      data-service-name={name}
      title={name || '(unnamed service)'}
      style={{ '--service-hue': serviceHue(name) } as CSSProperties}
      className="inline-flex min-w-0 max-w-full shrink-0 items-center gap-1.5 rounded border border-[hsl(var(--service-hue)_70%_45%/0.35)] bg-[hsl(var(--service-hue)_75%_50%/0.12)] px-1.5 py-0.5 align-middle font-mono text-[10.5px] font-medium leading-tight text-[hsl(var(--service-hue)_70%_30%)] dark:text-[hsl(var(--service-hue)_80%_78%)]"
    >
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 shrink-0 rounded-full bg-[hsl(var(--service-hue)_75%_55%)]"
      />
      <span className="truncate">{name || '(unnamed service)'}</span>
      {additionalCount > 0 && <span className="shrink-0">({additionalCount} more)</span>}
    </span>
  )
}
