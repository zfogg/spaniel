import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'

const parameters = [
  { name: 'session_id', source: 'Current session', kind: 'string', detail: 'Built-in: the active ingestion session ID, resolved by Spaniel each time the query runs.' },
  { name: 'service', source: 'spans.service_name', kind: 'string', detail: 'Service name to compare with spans.service_name (also available on logs and metrics). Define its value in Reusable parameters; it is not filled automatically.' },
  { name: 'operation', source: 'spans.name', kind: 'string', detail: 'Span operation name to compare with spans.name. Define its value in Reusable parameters; it is not filled automatically.' },
  { name: 'status_code', source: 'spans.status_code', kind: 'number', detail: 'OpenTelemetry span status: 0 = unset, 1 = OK, 2 = error. Not an HTTP response status. Define its value in Reusable parameters.' },
  { name: 'severity', source: 'logs.severity', kind: 'number', detail: 'OpenTelemetry severity number: 1–4 TRACE, 5–8 DEBUG, 9–12 INFO, 13–16 WARN, 17–20 ERROR, 21–24 FATAL; 0 is unspecified. Define its value in Reusable parameters.' },
] as const

export function parameterDescription(name: string) {
  return parameters.find(parameter => parameter.name === name)?.detail
}

export function MagicParameters({ insert }: { insert: (value: string) => void }) {
  return <section className="rounded-lg border border-border bg-background p-3">
    <h2 className="text-sm font-semibold">Magic parameters</h2>
    <p className="mt-1 text-xs leading-4 text-muted-foreground">Values resolved automatically when a query runs.</p>
    <ParameterShortcuts insert={insert} magic/>
  </section>
}

function ParameterShortcuts({ insert, magic }: { insert: (value: string) => void; magic: boolean }) {
  return <div className="mt-3 space-y-1">{parameters.filter(parameter => (parameter.name === 'session_id') === magic).map(parameter => <Tooltip key={parameter.name}>
      <TooltipTrigger aria-label={'$' + parameter.name + ' ' + parameter.source + ' ' + parameter.kind} onClick={() => insert('$' + parameter.name)} className="grid w-full cursor-pointer grid-cols-[96px_minmax(0,1fr)_auto] items-center gap-2 rounded px-2 py-2 text-left hover:bg-muted">
        <span className="font-mono text-[10px] text-accent-ink">${parameter.name}</span>
        <span className="truncate font-mono text-[9px] text-muted-foreground">{parameter.source}</span>
        <span className="rounded bg-accent-bg px-1.5 py-0.5 font-mono text-[9px] text-accent-ink">{parameter.kind}</span>
      </TooltipTrigger>
      <TooltipContent side="left" className="max-w-xs flex-col items-start whitespace-normal border border-border bg-popover text-popover-foreground shadow-lg">
        <strong className="break-all font-mono">{parameter.source}</strong>
        <span>{parameter.detail} Use the parameter without quotes.</span>
      </TooltipContent>
    </Tooltip>)}</div>
}
