import CodeMirror from '@uiw/react-codemirror'
import { sql } from '@codemirror/lang-sql'
import { yaml } from '@codemirror/lang-yaml'
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
  { tag: [tags.keyword, tags.propertyName], color: 'var(--sql-keyword)', fontWeight: '700' },
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

// The alert importer accepts YAML, so keep it in the same real editor used
// for SQL rather than presenting an unhighlighted textarea beside it.
export function YamlEditor({ value, onChange, label = 'Alert YAML' }: SqlEditorProps) {
  return (
    <CodeMirror
      aria-label={label}
      value={value}
      onChange={onChange}
      extensions={[yaml(), driftTheme, syntaxHighlighting(driftHighlight)]}
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
const staticYAML = yaml().language
const staticYAMLHighlight = tagHighlighter([
  { tag: tags.propertyName, class: 'font-bold text-[var(--sql-keyword)]' },
  { tag: [tags.string, tags.special(tags.string)], class: 'text-[var(--sql-string)]' },
  { tag: tags.number, class: 'text-[var(--sql-number)]' },
  {
    tag: [tags.comment, tags.lineComment, tags.blockComment],
    class: 'italic text-[var(--sql-comment)]',
  },
])

function StaticCode({
  value,
  language,
  highlighter,
  label,
}: {
  value: string
  language: typeof staticSQL
  highlighter: typeof staticHighlight
  label: string
}) {
  const content = useMemo(() => {
    const nodes: ReactNode[] = []
    let end = 0
    highlightTree(language.parser.parse(value), highlighter, (from, to, className) => {
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
  }, [value, language, highlighter])
  return (
    <div
      aria-label={label}
      className="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere] font-mono text-[11px] leading-relaxed text-foreground"
    >
      {content}
    </div>
  )
}

// The OpenAPI document is deliberately served as pretty-printed JSON. Keep
// this renderer read-only and dependency-free: it retains the server's exact
// bytes while adding enough syntax color to make a large contract pleasant to
// scan (without turning the documentation page into an editor).
export function JsonCode({ value }: { value: string }) {
  const content = useMemo(() => {
    const nodes: ReactNode[] = []
    const token =
      /("(?:\\.|[^"\\])*")(?=\s*:)|("(?:\\.|[^"\\])*"|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|\b(?:true|false|null)\b)/g
    let end = 0
    let match: RegExpExecArray | null
    let key = 0
    while ((match = token.exec(value))) {
      if (match.index > end) nodes.push(value.slice(end, match.index))
      const text = match[0]
      const className = match[1]
        ? 'font-semibold text-[var(--sql-keyword)]'
        : text.startsWith('"')
          ? 'text-[var(--sql-string)]'
          : text === 'true' || text === 'false' || text === 'null'
            ? 'font-semibold text-[var(--sql-operator)]'
            : 'text-[var(--sql-number)]'
      nodes.push(
        <span key={`${match.index}-${key++}`} className={className}>
          {text}
        </span>,
      )
      end = token.lastIndex
    }
    if (end < value.length) nodes.push(value.slice(end))
    return nodes
  }, [value])

  return (
    <code className="block min-w-max whitespace-pre font-mono text-[11px] leading-[1.7] text-foreground">
      {content}
    </code>
  )
}

export function SqlCode({ value }: { value: string }) {
  return (
    <StaticCode
      value={value}
      language={staticSQL}
      highlighter={staticHighlight}
      label="SQL query"
    />
  )
}

export function YamlCode({ value }: { value: string }) {
  return (
    <StaticCode
      value={value}
      language={staticYAML}
      highlighter={staticYAMLHighlight}
      label="YAML configuration"
    />
  )
}
