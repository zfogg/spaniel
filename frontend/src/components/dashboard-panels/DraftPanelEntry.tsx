import type { DashboardPanel } from '@/lib/api'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

export function DraftPanelEntry({
  panel,
  edit,
}: {
  panel: DashboardPanel
  edit: (panel: DashboardPanel) => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        onClick={() => edit(panel)}
        className="w-full cursor-pointer rounded border border-border p-2 text-left hover:bg-muted"
      >
        <span className="block truncate text-xs font-medium">{panel.title}</span>
        <span className="mt-1 block font-mono text-[10px] text-muted-foreground">
          {panel.display_type.replace(/_/g, ' ')}
        </span>
      </TooltipTrigger>
      <TooltipContent
        side="left"
        className="max-w-sm break-words border border-border bg-popover text-popover-foreground"
      >
        {panel.title}
      </TooltipContent>
    </Tooltip>
  )
}
