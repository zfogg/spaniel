import { useEffect, useRef, useState } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { useNavigate } from 'react-router-dom'
import { Search, Clock, Hash, FileText, ArrowUpRight } from 'lucide-react'
import { api, type SearchResult } from '@/lib/api'

// ── localStorage recent searches ─────────────────────────────────────────────

const RECENT_KEY = 'spaniel:search-recent'
const MAX_RECENT = 8

function getRecent(): string[] {
  try { return JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]') } catch { return [] }
}

function saveRecent(q: string) {
  if (!q.trim()) return
  const next = [q, ...getRecent().filter(r => r !== q)].slice(0, MAX_RECENT)
  localStorage.setItem(RECENT_KEY, JSON.stringify(next))
}

// ── Item types ────────────────────────────────────────────────────────────────

type Item =
  | { type: 'result'; result: SearchResult }
  | { type: 'recent'; query: string }

// ── Sub-components ────────────────────────────────────────────────────────────

function KindBadge({ kind }: { kind: string }) {
  return (
    <span className="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
      {kind}
    </span>
  )
}

function ResultRow({
  item,
  active,
  onClick,
  onHover,
}: {
  item: Item
  active: boolean
  onClick: () => void
  onHover: () => void
}) {
  const cls = [
    'flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors cursor-pointer',
    active ? 'bg-muted' : 'hover:bg-muted/50',
  ].join(' ')

  if (item.type === 'recent') {
    return (
      <button type="button" className={cls} onClick={onClick} onMouseEnter={onHover}>
        <Clock className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="flex-1 truncate font-sans text-sm text-muted-foreground">
          {item.query}
        </span>
        <span className="font-sans text-[10px] text-muted-foreground">recent</span>
      </button>
    )
  }

  const r = item.result
  const Icon = r.kind === 'log' ? FileText : Hash
  return (
    <button type="button" className={cls} onClick={onClick} onMouseEnter={onHover}>
      <Icon className="size-3.5 shrink-0 text-accent" />
      <div className="min-w-0 flex-1">
        <div className="truncate font-sans text-sm text-foreground">{r.title}</div>
        <div className="flex items-center gap-1.5 truncate font-sans text-[11px] text-muted-foreground">
          <span>{r.subtitle}</span>
          {r.trace_id && (
            <span className="font-mono opacity-50">{r.trace_id.slice(0, 8)}</span>
          )}
        </div>
      </div>
      <KindBadge kind={r.kind} />
      <ArrowUpRight className="size-3 shrink-0 text-muted-foreground" />
    </button>
  )
}

// ── CommandPalette ────────────────────────────────────────────────────────────

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [recent, setRecent] = useState<string[]>([])
  const [selected, setSelected] = useState(0)
  const [loading, setLoading] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  // Debounced search
  useEffect(() => {
    if (!query.trim()) {
      setResults([])
      setLoading(false)
      return
    }
    setLoading(true)
    const t = setTimeout(async () => {
      try {
        const { data } = await api.search.query(query)
        setResults(data ?? [])
      } catch {
        setResults([])
      } finally {
        setLoading(false)
      }
    }, 150)
    return () => clearTimeout(t)
  }, [query])

  // Reset on open
  useEffect(() => {
    if (open) {
      setQuery('')
      setResults([])
      setSelected(0)
      setRecent(getRecent())
      requestAnimationFrame(() => inputRef.current?.focus())
    }
  }, [open])

  const items: Item[] = query.trim()
    ? results.map(r => ({ type: 'result' as const, result: r }))
    : recent.map(q => ({ type: 'recent' as const, query: q }))

  // Clamp selection
  useEffect(() => {
    setSelected(s => Math.min(s, Math.max(0, items.length - 1)))
  }, [items.length])

  // Scroll selected into view
  useEffect(() => {
    const el = listRef.current?.children[selected] as HTMLElement | undefined
    el?.scrollIntoView({ block: 'nearest' })
  }, [selected])

  function activate(item: Item) {
    if (item.type === 'recent') {
      setQuery(item.query)
      setSelected(0)
      return
    }
    saveRecent(query)
    onClose()
    const r = item.result
    if (r.kind === 'log') {
      navigate('/logs')
    } else {
      navigate(`/traces/${r.trace_id}`)
    }
  }

  function onKeyDown(e: React.KeyboardEvent) {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setSelected(s => Math.min(s + 1, items.length - 1))
        break
      case 'ArrowUp':
        e.preventDefault()
        setSelected(s => Math.max(s - 1, 0))
        break
      case 'Enter':
        if (items[selected]) activate(items[selected])
        break
    }
  }

  const hasContent = items.length > 0
  const noResults = query.trim() && !loading && results.length === 0

  return (
    <Dialog.Root open={open} onOpenChange={v => { if (!v) onClose() }}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/40" />
        <Dialog.Content
          aria-label="Global search"
          className="fixed left-1/2 top-[18%] z-50 w-full max-w-[580px] -translate-x-1/2 rounded-xl border border-border bg-background shadow-2xl outline-none"
          onKeyDown={onKeyDown}
        >
          {/* Input row */}
          <div className="flex items-center gap-3 border-b border-border px-4 py-3">
            <Search className="size-4 shrink-0 text-muted-foreground" />
            <input
              ref={inputRef}
              value={query}
              onChange={e => { setQuery(e.target.value); setSelected(0) }}
              placeholder="Search traces, spans, services, logs…"
              className="flex-1 bg-transparent font-sans text-sm text-foreground placeholder:text-muted-foreground outline-none"
            />
            {loading && (
              <span className="shrink-0 font-sans text-[10px] text-muted-foreground">
                searching…
              </span>
            )}
            <kbd className="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
              Esc
            </kbd>
          </div>

          {/* Results list */}
          {hasContent && (
            <div ref={listRef} className="max-h-[360px] overflow-y-auto py-1">
              {items.map((item, i) => (
                <ResultRow
                  key={item.type === 'recent' ? `recent-${item.query}` : `${item.result.kind}-${item.result.trace_id}-${i}`}
                  item={item}
                  active={i === selected}
                  onClick={() => activate(item)}
                  onHover={() => setSelected(i)}
                />
              ))}
            </div>
          )}

          {/* No results */}
          {noResults && (
            <div className="px-4 py-8 text-center font-sans text-sm text-muted-foreground">
              No results for{' '}
              <span className="font-medium text-foreground">"{query}"</span>
            </div>
          )}

          {/* Footer hints */}
          <div className="flex items-center gap-4 border-t border-border px-4 py-2">
            {(['↑↓ navigate', '↵ open', 'Esc close'] as const).map(hint => {
              const [key, label] = hint.split(' ')
              return (
                <span key={hint} className="flex items-center gap-1 font-sans text-[10px] text-muted-foreground">
                  <kbd className="rounded border border-border px-1 py-px font-mono text-[9px]">{key}</kbd>
                  {label}
                </span>
              )
            })}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
