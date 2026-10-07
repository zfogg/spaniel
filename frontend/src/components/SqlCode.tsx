import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { tags, highlightTree, tagHighlighter } from '@lezer/highlight'
import { useMemo, type ReactNode } from 'react'
import { EditorView } from '@codemirror/view'

// CodeMirror's Lezer parser is incremental and error-tolerant: an unfinished
// CTE, string, or parenthesis produces an error node while retaining every
// token it can already classify. That makes it appropriate for live typing,
// unlike a formatter that must successfully parse the whole statement.
const driftTheme = EditorView.theme({
  '&.cm-editor': {
    backgroundColor: 'var(--surface)',
    color: 'var(--foreground)',
    fontFamily: "'JetBrains Mono Variable', 'JetBrains Mono', ui-monospace, monospace",
    fontSize: '12px',
  },
  '.cm-scroller, .cm-content': { backgroundColor: 'var(--surface)', color: 'var(--foreground)' },
  '.cm-content': { caretColor: 'var(--accent-ink)', padding: '10px 12px', minHeight: '8rem' },
  '.cm-line': { color: 'var(--foreground)' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--accent-ink)' },
  '.cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection': {
    backgroundColor: 'var(--accent-bg)',
  },
  '.cm-gutters': {
    backgroundColor: 'var(--surface2)',
    color: 'var(--muted-foreground)',
    borderRight: '1px solid var(--border)',
  },
  '.cm-activeLine, .cm-activeLineGutter': {
    backgroundColor: 'color-mix(in srgb, var(--accent-bg) 55%, transparent)',
  },
  '.cm-scroller': { overflow: 'auto' },
  '&.sql-code .cm-content': { minHeight: 'auto', padding: '8px 10px' },
})

const driftHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: 'var(--sql-keyword)', fontWeight: '700' },
  { tag: [tags.string, tags.special(tags.string)], color: 'var(--sql-string)' },
  { tag: tags.number, color: 'var(--sql-number)' },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    color: 'var(--sql-comment)',
    fontStyle: 'italic',
  },
  { tag: [tags.operatorKeyword, tags.operator], color: 'var(--sql-operator)' },
  { tag: tags.variableName, color: 'var(--foreground)' },
])

// Do not make this a fallback highlighter. `basicSetup` supplies CodeMirror's
// default highlighter too; a fallback loses to it and leaves the light-theme
// purple SQL tokens in place. This is the editor's authoritative palette.
const sqlExtensions = [sql(), driftTheme, syntaxHighlighting(driftHighlight)]

type SqlEditorProps = {
  value: string
  onChange: (value: string) => void
  label?: string
}

export function SqlEditor({ value, onChange, label = 'DuckDB SQL' }: SqlEditorProps) {
  return (
    <CodeMirror
      aria-label={label}
      value={value}
      onChange={onChange}
      extensions={sqlExtensions}
      basicSetup={{
        lineNumbers: true,
        foldGutter: false,
        highlightActiveLineGutter: true,
        bracketMatching: true,
        autocompletion: true,
      }}
      className="overflow-hidden rounded border border-input bg-background text-xs"
    />
  )
}

const staticSQL = sql().language
const staticHighlight = tagHighlighter([
  { tag: tags.keyword, class: 'font-bold text-[var(--sql-keyword)]' },
  { tag: [tags.string, tags.special(tags.string)], class: 'text-[var(--sql-string)]' },
  { tag: tags.number, class: 'text-[var(--sql-number)]' },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    class: 'italic text-[var(--sql-comment)]',
  },
  { tag: [tags.operatorKeyword, tags.operator], class: 'text-[var(--sql-operator)]' },
])

export function SqlCode({ value }: { value: string }) {
  const content = useMemo(() => {
    const nodes: ReactNode[] = []
    let end = 0
    highlightTree(staticSQL.parser.parse(value), staticHighlight, (from, to, className) => {
      if (from > end) nodes.push(value.slice(end, from))
      nodes.push(
        <span key={from} className={className}>
          {value.slice(from, to)}
        </span>,
      )
      end = to
    })
    if (end < value.length) nodes.push(value.slice(end))
    return nodes
  }, [value])
  return (
    <div
      aria-label="SQL query"
      className="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere] font-mono text-[11px] leading-relaxed text-foreground"
    >
      {content}
    </div>
  )
}
