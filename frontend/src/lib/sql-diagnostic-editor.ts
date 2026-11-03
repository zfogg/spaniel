import { Decoration, EditorView, WidgetType } from '@codemirror/view'
import { displayRange, type SQLDiagnostic } from './sql-analysis'

class DiagnosticAnchor extends WidgetType {
  constructor(readonly message: string) {
    super()
  }
  toDOM() {
    const span = document.createElement('span')
    span.className = 'border-l-2 border-destructive'
    span.textContent = '\u200b'
    span.title = this.message
    span.dataset.testid = 'sql-diagnostic-anchor'
    return span
  }
}

export function diagnosticDecorations(text: string, diagnostics: SQLDiagnostic[]) {
  const ranges = diagnostics.flatMap((d) => {
    const r = displayRange(d, text)
    if (!r) return []
    return [
      r[0] === r[1]
        ? Decoration.widget({ widget: new DiagnosticAnchor(d.message), side: 1 }).range(r[0])
        : Decoration.mark({
            class: 'underline decoration-wavy decoration-destructive underline-offset-4',
            attributes: { 'data-testid': 'sql-diagnostic-range', title: d.message },
          }).range(r[0], r[1]),
    ]
  })
  return EditorView.decorations.of(Decoration.set(ranges, true))
}
