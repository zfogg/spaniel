import { useEffect, useState } from 'react'
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from 'cmdk'
import { useNavigate } from 'react-router-dom'
import { Clock, FileText, Hash } from 'lucide-react'
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

  function activate(kind: 'trace' | 'log' | 'recent', traceId?: string, searchQuery?: string) {
    if (kind === 'recent' && searchQuery) {
      setQuery(searchQuery)
      return
    }
    saveRecent(query)
    onClose()
    if (kind === 'log') {
      navigate('/logs')
    } else if (traceId) {
      navigate(`/traces/${traceId}`)
    }
  }

  const showRecent = !query.trim() && recent.length > 0
  const showResults = !!query.trim()

  return (
    <CommandDialog
      open={open}
      onOpenChange={v => { if (!v) onClose() }}
      label="Global search"
      shouldFilter={false}
    >
      <div className="flex items-center border-b border-border px-3">
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder="Search traces, spans, services, logs…"
          className="flex h-11 w-full bg-transparent py-3 font-sans text-sm text-foreground placeholder:text-muted-foreground outline-none"
        />
        {loading && (
          <span className="shrink-0 font-sans text-[10px] text-muted-foreground pr-1">
            searching…
          </span>
        )}
      </div>

      <CommandList className="max-h-[360px] overflow-y-auto py-1">
        {showRecent && (
          <CommandGroup heading={
            <span className="px-4 py-1.5 font-sans text-[10px] uppercase tracking-wider text-muted-foreground">
              Recent
            </span>
          }>
            {recent.map(q => (
              <CommandItem
                key={`recent-${q}`}
                value={`recent:${q}`}
                onSelect={() => activate('recent', undefined, q)}
                className="flex items-center gap-3 px-4 py-2.5 cursor-pointer aria-selected:bg-muted"
              >
                <Clock className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="flex-1 truncate font-sans text-sm text-muted-foreground">{q}</span>
                <span className="font-sans text-[10px] text-muted-foreground">recent</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}

        {showResults && (
          <>
            {results.filter(r => r.kind === 'trace').length > 0 && (
              <CommandGroup heading={
                <span className="px-4 py-1.5 font-sans text-[10px] uppercase tracking-wider text-muted-foreground">
                  Traces
                </span>
              }>
                {results.filter(r => r.kind === 'trace').map((r, i) => (
                  <CommandItem
                    key={`trace-${r.trace_id}-${i}`}
                    value={`trace:${r.trace_id}:${r.title}`}
                    onSelect={() => activate('trace', r.trace_id)}
                    className="flex items-center gap-3 px-4 py-2.5 cursor-pointer aria-selected:bg-muted"
                  >
                    <Hash className="size-3.5 shrink-0 text-accent" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-sans text-sm text-foreground">{r.title}</div>
                      <div className="flex items-center gap-1.5 truncate font-sans text-[11px] text-muted-foreground">
                        <span>{r.subtitle}</span>
                        <span className="font-mono opacity-50">{r.trace_id.slice(0, 8)}</span>
                      </div>
                    </div>
                    <span className="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                      trace
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {results.filter(r => r.kind === 'log').length > 0 && (
              <CommandGroup heading={
                <span className="px-4 py-1.5 font-sans text-[10px] uppercase tracking-wider text-muted-foreground">
                  Logs
                </span>
              }>
                {results.filter(r => r.kind === 'log').map((r, i) => (
                  <CommandItem
                    key={`log-${r.trace_id}-${i}`}
                    value={`log:${r.trace_id}:${r.title}`}
                    onSelect={() => activate('log', r.trace_id)}
                    className="flex items-center gap-3 px-4 py-2.5 cursor-pointer aria-selected:bg-muted"
                  >
                    <FileText className="size-3.5 shrink-0 text-accent" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-sans text-sm text-foreground">{r.title}</div>
                      <div className="truncate font-sans text-[11px] text-muted-foreground">{r.subtitle}</div>
                    </div>
                    <span className="shrink-0 rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                      log
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            <CommandEmpty className="px-4 py-8 text-center font-sans text-sm text-muted-foreground">
              No results for{' '}
              <span className="font-medium text-foreground">"{query}"</span>
            </CommandEmpty>
          </>
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
