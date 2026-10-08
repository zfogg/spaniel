import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowDown, ArrowUp } from 'lucide-react'
import { motion } from 'motion/react'

import { type DashboardPanel, api } from '@/lib/api'
import { PanelRenderer } from '@/components/dashboard-panels/PanelRenderer'
import { SqlCode } from '@/components/SqlCode'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

type Props = {
  panel: DashboardPanel
  dashboardId: string
  variables?: Record<string, string>
  onStatus?: (id: string, state: 'loading' | 'ready' | 'error', message?: string) => void
  moveUp?: () => void
  moveDown?: () => void
  moving?: boolean
}

export function DashboardPanel({
  panel,
  dashboardId,
  variables: sharedVariables,
  onStatus,
  moveUp,
  moveDown,
  moving,
}: Props) {
  const [variables, setVariables] = useState<Record<string, string>>({})
  const { data: dashboard } = useQuery({
    queryKey: ['dashboard-runtime', dashboardId],
    queryFn: () => api.dashboards.get(dashboardId).then((response) => response.data),
  })
  useEffect(() => {
    if (dashboard)
      setVariables(
        Object.fromEntries(
          dashboard.variables.map((variable) => [variable.name, variable.default_value]),
        ),
      )
  }, [dashboard])
  let layout: { width?: string; x?: number; y?: number; w?: number; h?: number } = {}
  try {
    layout = JSON.parse(panel.layout_json) as typeof layout
  } catch {
    /* default */
  }
  const width = layout.w ?? (layout.width === 'wide' ? 12 : 6)
  return (
    <motion.article
      layout="position"
      style={{
        gridColumn: `span ${Math.max(1, Math.min(12, width))} / span ${Math.max(1, Math.min(12, width))}`,
        gridColumnStart: layout.x ? Math.max(1, Math.min(12, layout.x)) : undefined,
        gridRow: `span ${Math.max(1, Math.min(6, layout.h ?? 1))} / span ${Math.max(1, Math.min(6, layout.h ?? 1))}`,
        gridRowStart: layout.y ? Math.max(1, layout.y) : undefined,
      }}
      className="min-h-[180px] overflow-hidden rounded-lg border border-border bg-surface"
    >
      <header className="border-b border-border px-4 py-3">
        <div className="flex items-center gap-2">
          <h2 className="min-w-0 flex-1 truncate text-[13px] font-semibold">{panel.title}</h2>
          {moveUp && (
            <button
              type="button"
              aria-label={`Move ${panel.title} up`}
              title="Move panel up"
              disabled={moving}
              onClick={moveUp}
              className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-wait disabled:opacity-50"
            >
              <ArrowUp size={16} />
            </button>
          )}
          {moveDown && (
            <button
              type="button"
              aria-label={`Move ${panel.title} down`}
              title="Move panel down"
              disabled={moving}
              onClick={moveDown}
              className="cursor-pointer rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground disabled:cursor-wait disabled:opacity-50"
            >
              <ArrowDown size={16} />
            </button>
          )}
        </div>
        <Tooltip>
          <TooltipTrigger
            aria-label={`Show SQL for ${panel.title}`}
            className="mt-1 block w-full cursor-help truncate rounded text-left font-mono text-[10px] text-muted-foreground"
          >
            {panel.display_type} · {panel.query_sql}
          </TooltipTrigger>
          <TooltipContent
            side="bottom"
            align="start"
            className="max-w-[min(38rem,calc(100vw-2rem))] items-start border border-border bg-popover p-3 text-popover-foreground shadow-lg"
          >
            <SqlCode value={panel.query_sql} />
          </TooltipContent>
        </Tooltip>
      </header>
      <div className="px-4 py-3">
        {!sharedVariables && dashboard?.variables.length ? (
          <div className="mb-3 flex flex-wrap gap-2">
            {dashboard.variables.map((variable) => (
              <label key={variable.name} className="font-mono text-[10px] text-muted-foreground">
                ${variable.name}
                <input
                  aria-label={`Dashboard variable ${variable.name}`}
                  value={variables[variable.name] ?? ''}
                  onChange={(event) =>
                    setVariables((current) => ({ ...current, [variable.name]: event.target.value }))
                  }
                  className="ml-1 rounded border border-border bg-background px-1 py-0.5 text-foreground"
                />
              </label>
            ))}
          </div>
        ) : null}
        <PanelRenderer
          dashboardId={dashboardId}
          panel={panel}
          variables={sharedVariables ?? variables}
          onStatus={onStatus}
        />
      </div>
    </motion.article>
  )
}
