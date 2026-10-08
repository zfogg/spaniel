import { type ReactNode } from 'react'
import { BellRing, CircleHelp } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
export function GroupBadge({ groupKey }: { groupKey: string }) {
  return (
    <span className="inline-flex rounded bg-accent-bg px-1.5 py-0.5 font-mono text-[10px] text-accent-ink">
      {groupKey}
    </span>
  )
}

export function InspectorSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </h3>
      {children}
    </section>
  )
}

export function Field({
  label,
  value,
  mono = false,
}: {
  label: ReactNode
  value: ReactNode
  mono?: boolean
}) {
  return (
    <div>
      <dt className="mb-1 text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'font-mono text-xs' : ''}>{value}</dd>
    </div>
  )
}

export function LabelWithHelp({ children, help }: { children: ReactNode; help: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      {children}
      <Tooltip>
        <TooltipTrigger
          type="button"
          aria-label={`Help: ${typeof children === 'string' ? children : 'field'}`}
          className="inline-flex cursor-help text-muted-foreground hover:text-foreground"
        >
          <CircleHelp className="size-3.5" />
        </TooltipTrigger>
        <TooltipContent className="max-w-xs border border-border bg-popover text-popover-foreground shadow-lg">
          {help}
        </TooltipContent>
      </Tooltip>
    </span>
  )
}

export function Empty({ label }: { label: string }) {
  return (
    <div className="rounded border border-dashed border-border p-10 text-center">
      <BellRing className="mx-auto" />
      <p className="mt-3 text-sm">{label}</p>
    </div>
  )
}
