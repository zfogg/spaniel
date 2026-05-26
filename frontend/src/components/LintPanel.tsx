import { Badge } from '@/components/ui/badge'
import { LintWarning } from '../lib/api'

interface Props {
  warnings: LintWarning[]
  onSelectSpan?: (spanId: string) => void
}

function severityVariant(s: string): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (s === 'error') return 'destructive'
  if (s === 'warning') return 'outline'
  return 'secondary'
}

export default function LintPanel({ warnings, onSelectSpan }: Props) {
  if (warnings.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
        No lint warnings
      </div>
    )
  }

  // Group by rule_id
  const grouped = warnings.reduce<Record<string, LintWarning[]>>((acc, w) => {
    if (!acc[w.rule_id]) acc[w.rule_id] = []
    acc[w.rule_id].push(w)
    return acc
  }, {})

  return (
    <div className="overflow-auto h-full p-4 flex flex-col gap-4">
      {Object.entries(grouped).map(([ruleId, items]) => (
        <div key={ruleId}>
          <div className="flex items-center gap-2 mb-2">
            <Badge variant={severityVariant(items[0].severity)} className="font-mono text-[10px]">
              {items[0].severity.toUpperCase()}
            </Badge>
            <span className="font-mono text-xs text-foreground">{ruleId}</span>
            <span className="text-xs text-muted-foreground ml-auto">{items.length}×</span>
          </div>
          <div className="flex flex-col gap-1">
            {items.map((w, i) => (
              <button
                key={i}
                onClick={() => onSelectSpan?.(w.span_id)}
                className="text-left text-xs text-muted-foreground hover:text-foreground bg-muted/20 hover:bg-muted/40 rounded px-3 py-2 font-mono transition-colors"
              >
                <span className="text-foreground/60">{w.span_id.slice(0, 8)}…</span>
                {'  '}{w.message}
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
