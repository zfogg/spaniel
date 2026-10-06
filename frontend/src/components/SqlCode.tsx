import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags } from '@lezer/highlight'
import { EditorView } from '@codemirror/view'

// CodeMirror's Lezer parser is incremental and error-tolerant: an unfinished
// CTE, string, or parenthesis produces an error node while retaining every
// token it can already classify. That makes it appropriate for live typing,
// unlike a formatter that must successfully parse the whole statement.
const driftTheme = EditorView.theme({
  '&': {
    backgroundColor: 'var(--background)',
    color: 'var(--foreground)',
    fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
    fontSize: '12px',
  },
  '.cm-content': { caretColor: 'var(--accent-ink)', padding: '10px 12px', minHeight: '8rem' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent-ink)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': { backgroundColor: 'var(--accent-bg)' },
  '.cm-gutters': { backgroundColor: 'var(--surface2)', color: 'var(--muted-foreground)', borderRight: '1px solid var(--border)' },
  '.cm-activeLine, .cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--accent-bg) 55%, transparent)' },
  '.cm-scroller': { overflow: 'auto' },
  '&.sql-code .cm-content': { minHeight: 'auto', padding: '8px 10px' },
})

const driftHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--accent-ink)', fontWeight: '700' },
  { tag: [tags.string, tags.special(tags.string)], color: 'var(--ok-ink)' },
  { tag: tags.number, color: 'var(--warn-ink)' },
  { tag: [tags.comment, tags.lineComment, tags.blockComment], color: 'var(--muted-foreground)', fontStyle: 'italic' },
  { tag: [tags.operatorKeyword, tags.operator], color: 'var(--danger-ink)' },
  { tag: tags.variableName, color: 'var(--foreground)' },
])

const sqlExtensions = [sql(), driftTheme, syntaxHighlighting(driftHighlight)]

type SqlEditorProps = {
  value: string
  onChange: (value: string) => void
  label?: string
}

export function SqlEditor({ value, onChange, label = 'DuckDB SQL' }: SqlEditorProps) {
  return <CodeMirror
    aria-label={label}
    value={value}
    onChange={onChange}
    extensions={sqlExtensions}
    basicSetup={{ lineNumbers: true, foldGutter: false, highlightActiveLineGutter: true, bracketMatching: true, autocompletion: true }}
    className="overflow-hidden rounded border border-input bg-background text-xs"
  />
}

export function SqlCode({ value }: { value: string }) {
  return <CodeMirror
    aria-label="SQL query"
    value={value}
    editable={false}
    basicSetup={{ lineNumbers: false, foldGutter: false, highlightActiveLine: false, highlightActiveLineGutter: false }}
    extensions={sqlExtensions}
    className="sql-code max-h-56 overflow-auto rounded border border-border bg-background text-[11px]"
  />
}
