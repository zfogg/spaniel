import type { CSSProperties } from 'react'
import { serviceColor } from '@/lib/service-color'

export default function ServiceNameBadge({
  name,
  additionalCount = 0,
}: {
  name: string
  additionalCount?: number
}) {
  const color = serviceColor(name)
  return (
    <span
      data-service-name={name}
      title={name || '(unnamed service)'}
      style={
        {
          '--service-hue': color.hue,
          '--service-saturation': `${color.saturation}%`,
          '--service-lightness': `${color.lightness}%`,
        } as CSSProperties
      }
      className="inline-flex min-w-0 max-w-full shrink-0 items-center gap-1.5 rounded border border-[hsl(var(--service-hue)_var(--service-saturation)_45%/0.35)] bg-[hsl(var(--service-hue)_var(--service-saturation)_var(--service-lightness)/0.12)] px-1.5 py-0.5 align-middle font-mono text-[10.5px] font-medium leading-tight text-[hsl(var(--service-hue)_var(--service-saturation)_30%)] dark:text-[hsl(var(--service-hue)_var(--service-saturation)_78%)]"
    >
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 shrink-0 rounded-full bg-[hsl(var(--service-hue)_var(--service-saturation)_var(--service-lightness))]"
      />
      <span className="truncate">{name || '(unnamed service)'}</span>
      {additionalCount > 0 && <span className="shrink-0">({additionalCount} more)</span>}
    </span>
  )
}
