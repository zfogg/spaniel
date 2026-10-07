import { useState } from 'react'
import type { DashboardVariable } from '@/lib/api'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { parameterDescription } from './magic-parameter-descriptions'

export function ReusableParameterList({
  variables,
  insert,
  remove,
}: {
  variables: DashboardVariable[]
  insert: (value: string) => void
  remove: (name: string) => Promise<void>
}) {
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState('')
  const deleteParameter = async (name: string) => {
    if (
      pending ||
      !window.confirm(
        `Delete reusable parameter "$${name}"? Queries using it will need another value or must be updated.`,
      )
    )
      return
    setPending(name)
    setError('')
    try {
      await remove(name)
    } catch (error) {
      setError(error instanceof Error ? error.message : 'Could not delete parameter')
    } finally {
      setPending(null)
    }
  }
  return (
    <div className="mt-3">
      {variables.map((variable) => (
        <div key={variable.name} className="flex items-center gap-1 border-t border-border py-1">
          <Tooltip>
            <TooltipTrigger
              onClick={() => insert('$' + variable.name)}
              className="grid min-w-0 flex-1 cursor-pointer grid-cols-[84px_minmax(0,1fr)_auto] gap-2 px-1 py-2 text-left font-mono text-[10px] hover:bg-muted"
            >
              <span className="truncate text-accent-ink">${variable.name}</span>
              <span className="truncate text-muted-foreground">{variable.source}</span>
              <span>{variable.kind}</span>
            </TooltipTrigger>
            <TooltipContent
              side="left"
              className="flex-col items-start border border-border bg-popover text-popover-foreground"
            >
              <strong className="break-all">{variable.source}</strong>
              <span>{parameterDescription(variable.name)}</span>
              <span>
                Default: {variable.default_value || '(empty)'}. Click to insert ${variable.name}.
              </span>
            </TooltipContent>
          </Tooltip>
          <button
            type="button"
            aria-label={'Delete parameter $' + variable.name}
            disabled={pending !== null}
            onClick={() => void deleteParameter(variable.name)}
            className="cursor-pointer rounded px-1.5 py-1 text-[10px] text-danger hover:bg-muted disabled:cursor-wait disabled:opacity-50"
          >
            {pending === variable.name ? 'Deleting…' : 'Delete'}
          </button>
        </div>
      ))}
      {error && (
        <p role="alert" className="mt-2 text-xs text-danger">
          {error}
        </p>
      )}
    </div>
  )
}
