import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const parameters = [
  {
    name: 'session_id',
    source: 'Current session',
    kind: 'string',
    detail:
      'Built-in: the active ingestion session ID, resolved by Spaniel each time the query runs.',
  },
] as const

export function parameterDescription(name: string) {
  return parameters.find((parameter) => parameter.name === name)?.detail
}

export function MagicParameters({ insert }: { insert: (value: string) => void }) {
  return (
    <section className="rounded-lg border border-border bg-background p-3">
      <h2 className="text-sm font-semibold">Magic parameters</h2>
      <p className="mt-1 text-xs leading-4 text-muted-foreground">
        Only reserved values resolved automatically. Define every dashboard filter explicitly below.
      </p>
      <ParameterShortcuts insert={insert} magic />
    </section>
  )
}

function ParameterShortcuts({
  insert,
  magic,
}: {
  insert: (value: string) => void
  magic: boolean
}) {
  return (
    <div className="mt-3 space-y-1">
      {parameters
        .filter((parameter) => magic)
        .map((parameter) => (
          <Tooltip key={parameter.name}>
            <TooltipTrigger
              aria-label={'$' + parameter.name + ' ' + parameter.source + ' ' + parameter.kind}
              onClick={() => insert('$' + parameter.name)}
              className="grid w-full cursor-pointer grid-cols-[96px_minmax(0,1fr)_auto] items-center gap-2 rounded px-2 py-2 text-left hover:bg-muted"
            >
              <span className="font-mono text-[10px] text-accent-ink">${parameter.name}</span>
              <span className="truncate font-mono text-[9px] text-muted-foreground">
                {parameter.source}
              </span>
              <span className="rounded bg-accent-bg px-1.5 py-0.5 font-mono text-[9px] text-accent-ink">
                {parameter.kind}
              </span>
            </TooltipTrigger>
            <TooltipContent
              side="left"
              className="max-w-xs flex-col items-start whitespace-normal border border-border bg-popover text-popover-foreground shadow-lg"
            >
              <strong className="break-all font-mono">{parameter.source}</strong>
              <span>{parameter.detail} Use the parameter without quotes.</span>
            </TooltipContent>
          </Tooltip>
        ))}
    </div>
  )
}
