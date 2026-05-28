import { useEffect, useState } from 'react'
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from 'cmdk'
import { useNavigate } from 'react-router-dom'
import { Clock, FileText, GitBranch, Hash, Layers, Server } from 'lucide-react'
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

// ── helpers ───────────────────────────────────────────────────────────────────

const KIND_META: Record<SearchResult['kind'], {
  label: string
  Icon: React.ComponentType<{ className?: string }>
}> = {
  trace:   { label: 'trace',   Icon: Hash },
  span:    { label: 'span',    Icon: GitBranch },
  session: { label: 'session', Icon: Layers },
  service: { label: 'service', Icon: Server },
  log:     { label: 'log',     Icon: FileText },
}

const GROUP_ORDER: SearchResult['kind'][] = ['trace', 'span', 'session', 'service', 'log']

const GROUP_LABELS: Record<SearchResult['kind'], string> = {
  trace:   'Traces',
  span:    'Spans',
  session: 'Sessions',
  service: 'Services',
  log:     'Logs',
}

function resultKey(r: SearchResult, i: number) {
  return `${r.kind}-${r.trace_id}-${r.span_id ?? ''}-${i}`
}

// ── CommandPalette ────────────────────────────────────────────────────────────

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [recent, setRecent] = useState<string[]>([])
  const [loading, setLoading] = useState(false)
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
      setRecent(getRecent())
    }
  }, [open])

  function activate(r: SearchResult) {
    saveRecent(query)
    onClose()
    switch (r.kind) {
      case 'trace':
      case 'span':
        navigate(`/traces/${r.trace_id}`)
        break
      case 'session':
        navigate('/sessions')
        break
      case 'service':
        navigate('/services')
        break
      case 'log':
        navigate('/logs')
        break
    }
  }

  function activateRecent(q: string) {
    setQuery(q)
  }

  const grouped = GROUP_ORDER
    .map(kind => ({ kind, items: results.filter(r => r.kind === kind) }))
    .filter(g => g.items.length > 0)

  return (
    <CommandDialog
      open={open}
      onOpenChange={v => { if (!v) onClose() }}
      label="Global search"
      shouldFilter={false}
      // cmdk's Dialog ships ZERO styling — without these classes the
      // portal renders the overlay + content with no positioning and the
      // palette is invisible. The selectors below match Radix's animation
      // hooks (data-[state=open/closed]) so it fades cleanly.
      overlayClassName="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm"
      contentClassName="fixed left-1/2 top-[20%] z-50 w-[min(640px,90vw)] -translate-x-1/2 overflow-hidden rounded-xl border border-border bg-popover text-popover-foreground shadow-2xl outline-none"
    >
      <div className="flex items-center border-b border-border px-3">
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder="Search traces, spans, sessions, services, logs…"
          className="flex h-11 w-full bg-transparent py-3 font-sans text-sm text-foreground placeholder:text-muted-foreground outline-none"
        />
        {loading && (
          <span className="shrink-0 font-sans text-[10px] text-muted-foreground pr-1">
            searching…
          </span>
        )}
      </div>

      <CommandList className="max-h-[360px] overflow-y-auto py-1">
        {/* Recent searches */}
        {!query.trim() && recent.length > 0 && (
          <CommandGroup heading={
            <span className="px-4 py-1.5 font-sans text-[10px] uppercase tracking-wider text-muted-foreground">
              Recent
            </span>
          }>
            {recent.map(q => (
              <CommandItem
                key={`recent-${q}`}
                value={`recent:${q}`}
                onSelect={() => activateRecent(q)}
                className="flex items-center gap-3 px-4 py-2.5 cursor-pointer aria-selected:bg-muted"
              >
                <Clock className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate font-sans text-sm text-muted-foreground">{q}</span>
                <span className="font-sans text-[10px] text-muted-foreground">recent</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {/* Grouped results */}
        {query.trim() && grouped.map(({ kind, items }) => {
          const { Icon } = KIND_META[kind]
          return (
            <CommandGroup
              key={kind}
              heading={
                <span className="px-4 py-1.5 font-sans text-[10px] uppercase tracking-wider text-muted-foreground">
                  {GROUP_LABELS[kind]}
                </span>
              }
            >
              {items.map((r, i) => (
                <CommandItem
                  key={resultKey(r, i)}
                  value={`${kind}:${r.trace_id}:${r.title}:${i}`}
                  onSelect={() => activate(r)}
                  className="flex items-center gap-3 px-4 py-2.5 cursor-pointer aria-selected:bg-muted"
                >
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
                  <span className="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                    {KIND_META[kind].label}
                  </span>
                </CommandItem>
              ))}
            </CommandGroup>
          )
        })}

        {query.trim() && !loading && results.length === 0 && (
          <CommandEmpty className="px-4 py-8 text-center font-sans text-sm text-muted-foreground">
            No results for{' '}
            <span className="font-medium text-foreground">"{query}"</span>
          </CommandEmpty>
        )}
      </CommandList>

      <div className="flex items-center gap-4 border-t border-border px-4 py-2">
        {([['↑↓', 'navigate'], ['↵', 'open'], ['Esc', 'close']] as const).map(([key, label]) => (
          <span key={label} className="flex items-center gap-1 font-sans text-[10px] text-muted-foreground">
            <kbd className="rounded border border-border px-1 py-px font-mono text-[9px]">{key}</kbd>
            {label}
          </span>
        ))}
      </div>
    </CommandDialog>
  )
}
