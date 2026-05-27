import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type SpanRow } from '@/lib/api'
import { svcColor } from '@/lib/span-utils'
import { KIND_LABELS } from '@/lib/span-utils'
import { useWS, type SpanEvent } from '@/lib/ws'

// ── helpers ───────────────────────────────────────────────────────────────────

function fmtNs(ns: number): string {
  if (ns >= 1_000_000_000) return (ns / 1_000_000_000).toFixed(2) + 's'
  if (ns >= 1_000_000) return (ns / 1_000_000).toFixed(1) + 'ms'
  if (ns >= 1_000) return (ns / 1_000).toFixed(0) + 'µs'
  return ns + 'ns'
}

function fmtTime(ns: number): string {
  const d = new Date(ns / 1_000_000)
  const h = d.getHours().toString().padStart(2, '0')
  const m = d.getMinutes().toString().padStart(2, '0')
  const s = d.getSeconds().toString().padStart(2, '0')
  const ms = d.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}

function parseAttrs(raw: string): Record<string, unknown> {
  try { return JSON.parse(raw) } catch { return {} }
}

const SLOW_NS = 250_000_000

// ── SvcChip ───────────────────────────────────────────────────────────────────

function SvcChip({ name }: { name: string }) {
  const { fg, bg } = svcColor(name)
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      padding: '2px 7px', borderRadius: 4,
      fontFamily: 'var(--font-mono)', fontSize: 10.5, fontWeight: 500,
      color: fg, background: bg,
      border: '1px solid ' + fg + '44',
      whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', maxWidth: '100%',
    }}>
      {name}
    </span>
  )
}

// ── TagBadge ──────────────────────────────────────────────────────────────────

function TagBadge({ tag }: { tag?: string }) {
  if (!tag) return null
  const styles: Record<string, { bg: string; color: string }> = {
    'n+1':  { bg: 'var(--destructive, #c0392b)', color: '#fff' },
    error:  { bg: 'var(--destructive, #c0392b)', color: '#fff' },
    slow:   { bg: 'var(--warn, #d97706)', color: '#fff' },
    lint:   { bg: 'var(--warn, #d97706)', color: '#fff' },
  }
  const s = styles[tag] ?? { bg: 'var(--muted)', color: 'var(--muted-foreground)' }
  return (
    <span style={{
      display: 'inline-block', padding: '2px 6px', borderRadius: 4,
      fontFamily: 'var(--font-mono)', fontSize: 10, fontWeight: 600,
      ...s,
    }}>{tag}</span>
  )
}

// ── FilterChip ────────────────────────────────────────────────────────────────

function FilterChip({ label, onClear }: { label: string; onClear: () => void }) {
  return (
    <span style={{
      display: 'inline-flex', alignItems: 'center', gap: 5,
      padding: '4px 8px 4px 10px', borderRadius: 14,
      fontFamily: 'var(--font-mono)', fontSize: 11, fontWeight: 500,
      background: 'color-mix(in oklch, var(--accent) 14%, var(--background))',
      color: 'var(--foreground)', border: '1px solid color-mix(in oklch, var(--accent) 30%, transparent)',
    }}>
      {label}
      <button type="button" onClick={onClear} style={{
        background: 'transparent', border: 'none', cursor: 'pointer',
        color: 'inherit', fontSize: 14, lineHeight: 1, padding: 0, marginLeft: 2,
      }}>×</button>
    </span>
  )
}

// ── SidebarGroup ──────────────────────────────────────────────────────────────

function SbGroup({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 20 }}>
      <div style={{
        fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
        textTransform: 'uppercase', letterSpacing: '0.14em',
        padding: '0 12px', marginBottom: 4,
      }}>{title}</div>
      {children}
    </div>
  )
}

function SbItem({
  active, dot, count, onClick, children,
}: {
  active: boolean; dot?: string; count: number; onClick: () => void; children: React.ReactNode
}) {
  return (
    <button type="button" onClick={onClick} style={{
      display: 'flex', alignItems: 'center', gap: 7,
      width: '100%', textAlign: 'left',
      padding: '5px 12px', border: 'none', cursor: 'pointer',
      background: active ? 'var(--muted)' : 'transparent',
      borderLeft: active ? '2px solid var(--accent)' : '2px solid transparent',
      fontFamily: 'var(--font-mono)', fontSize: 11,
      color: active ? 'var(--foreground)' : 'var(--muted-foreground)',
      transition: 'background 0.1s',
    }}>
      {dot && (
        <span style={{ width: 7, height: 7, borderRadius: 7, background: dot, flexShrink: 0 }} />
      )}
      <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
        {children}
      </span>
      <span style={{ color: 'var(--muted-foreground)', fontSize: 10, flexShrink: 0 }}>{count}</span>
    </button>
  )
}

// ── SpanInspector ─────────────────────────────────────────────────────────────

function SpanInspector({ span, onClose }: { span: SpanRow; onClose: () => void }) {
  const navigate = useNavigate()
  const attrs = parseAttrs(span.attributes)
  const entries = Object.entries(attrs)
  const kindLabel = KIND_LABELS[span.kind] ?? 'unknown'
  const isSlow = span.duration_ns > SLOW_NS

  return (
    <aside data-testid="span-inspector" style={{
      width: 380, borderLeft: '1px solid var(--border)',
      background: 'var(--background)',
      display: 'flex', flexDirection: 'column', overflow: 'hidden', flexShrink: 0,
    }}>
      <div style={{ padding: '14px 16px', borderBottom: '1px solid var(--border)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
          <SvcChip name={span.service_name} />
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
            textTransform: 'uppercase', letterSpacing: '0.14em',
          }}>{kindLabel}</span>
          <div style={{ flex: 1 }} />
          <TagBadge tag={span.tag} />
          <button type="button" onClick={onClose} style={{
            background: 'transparent', border: 'none', cursor: 'pointer',
            color: 'var(--muted-foreground)', fontSize: 16, lineHeight: 1, padding: 0,
          }}>×</button>
        </div>
        <div style={{
          fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)',
          lineHeight: 1.4, wordBreak: 'break-word',
        }}>{span.name}</div>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginTop: 12 }}>
          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
              textTransform: 'uppercase', letterSpacing: '0.14em',
            }}>duration</div>
            <div style={{
              fontFamily: 'Georgia, serif', fontSize: 22, fontWeight: 600,
              color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)',
              lineHeight: 1, marginTop: 3,
            }}>{fmtNs(span.duration_ns)}</div>
          </div>
          <div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
              textTransform: 'uppercase', letterSpacing: '0.14em',
            }}>started</div>
            <div style={{
              fontFamily: 'var(--font-mono)', fontSize: 13, color: 'var(--foreground)',
              marginTop: 6,
            }}>{fmtTime(span.start_ns)}</div>
          </div>
        </div>
      </div>

      <div style={{
        padding: '14px 16px', borderBottom: '1px solid var(--border)',
        background: 'var(--muted)',
      }}>
        <div style={{
          fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
          textTransform: 'uppercase', letterSpacing: '0.14em', marginBottom: 5,
        }}>belongs to trace</div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{
            fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
            color: 'var(--foreground)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
          }}>{span.trace_id}</span>
        </div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)', marginTop: 2 }}>
          <button
            type="button"
            onClick={() => navigate(`/traces/${span.trace_id}`)}
            style={{
              background: 'transparent', border: 'none', cursor: 'pointer', padding: 0,
              fontFamily: 'var(--font-mono)', fontSize: 10,
              color: 'var(--accent, #6366f1)', textDecoration: 'underline',
              textDecorationStyle: 'dotted',
            }}
          >open waterfall</button>
        </div>
      </div>

      <div style={{ flex: 1, overflow: 'hidden auto' }}>
        <div style={{
          padding: '12px 16px 4px',
          fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
          textTransform: 'uppercase', letterSpacing: '0.14em',
          display: 'flex', alignItems: 'baseline',
        }}>
          attributes
          <span style={{ flex: 1 }} />
          <span style={{ color: 'var(--muted-foreground)', textTransform: 'none', letterSpacing: 0 }}>
            {entries.length} keys
          </span>
        </div>
        <div style={{ padding: '4px 10px 14px' }}>
          {entries.map(([k, v]) => {
            const isError = k === 'error' && v
            const valueColor =
              typeof v === 'boolean'
                ? (v ? '#3e6a3e' : 'var(--muted-foreground)')
                : typeof v === 'number'
                  ? 'var(--accent, #6366f1)'
                  : 'var(--foreground)'
            return (
              <div key={k} style={{
                display: 'grid', gridTemplateColumns: 'minmax(120px, 0.9fr) minmax(0, 1.1fr)',
                gap: 8, padding: '5px 8px', borderRadius: 5, alignItems: 'baseline',
              }}>
                <div style={{
                  fontFamily: 'var(--font-mono)', fontSize: 10.5, color: 'var(--muted-foreground)',
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                }}>{k}</div>
                <div style={{
                  fontFamily: 'var(--font-mono)', fontSize: 10.5,
                  color: valueColor,
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                  background: isError ? 'color-mix(in oklch, var(--destructive, #c0392b) 14%, transparent)' : 'transparent',
                  padding: '1px 4px', borderRadius: 3, justifySelf: 'start',
                }} title={String(v)}>
                  {typeof v === 'string' ? `"${v}"` : String(v)}
                </div>
              </div>
            )
          })}
          {entries.length === 0 && (
            <div style={{
              padding: '12px 8px', fontFamily: 'var(--font-mono)', fontSize: 10.5,
              color: 'var(--muted-foreground)',
            }}>no attributes</div>
          )}
        </div>
      </div>
    </aside>
  )
}

// ── Spans page ────────────────────────────────────────────────────────────────

export default function Spans() {
  const [spans, setSpans] = useState<SpanRow[]>([])
  const [loading, setLoading] = useState(true)
  const [sortBy, setSortBy] = useState<'time' | 'dur' | 'name'>('time')
  const [query, setQuery] = useState('')
  const [svcSel, setSvcSel] = useState<string | null>(null)
  const [kindSel, setKindSel] = useState<string | null>(null)
  const [tagSel, setTagSel] = useState<string | null>(null)
  const [selectedId, setSelectedId] = useState<string | null>(null)

  async function load(sort: 'time' | 'dur' | 'name') {
    try {
      const { data } = await api.spans.list({ sort })
      setSpans(data)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load(sortBy) }, [sortBy])

  useWS((_ev: SpanEvent) => { load(sortBy) })

  // facet counts computed from the full dataset
  const svcFacets = useMemo(() => {
    const m: Record<string, number> = {}
    for (const s of spans) m[s.service_name] = (m[s.service_name] ?? 0) + 1
    return Object.entries(m).sort((a, b) => b[1] - a[1])
  }, [spans])

  const kindFacets = useMemo(() => {
    const m: Record<string, number> = {}
    for (const s of spans) {
      const k = KIND_LABELS[s.kind] ?? 'unknown'
      m[k] = (m[k] ?? 0) + 1
    }
    return Object.entries(m).sort((a, b) => b[1] - a[1])
  }, [spans])

  const calloutCounts = useMemo(() => {
    const c = { 'n+1': 0, error: 0, slow: 0, lint: 0 }
    for (const s of spans) {
      if (s.tag === 'n+1') c['n+1']++
      else if (s.tag === 'error') c.error++
      else if (s.tag === 'slow') c.slow++
      else if (s.tag === 'lint') c.lint++
    }
    return c
  }, [spans])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return spans.filter(s => {
      if (svcSel && s.service_name !== svcSel) return false
      if (kindSel && KIND_LABELS[s.kind] !== kindSel) return false
      if (tagSel && s.tag !== tagSel) return false
      if (q) {
        const attrs = parseAttrs(s.attributes)
        const hay = [
          s.name, s.service_name, s.trace_id, s.span_id,
          ...Object.keys(attrs), ...Object.values(attrs).map(String),
        ].join(' ').toLowerCase()
        if (!hay.includes(q)) return false
      }
      return true
    })
  }, [spans, svcSel, kindSel, tagSel, query])

  const selected = filtered.find(s => s.span_id === selectedId) ?? null

  const activeFilters: { label: string; clear: () => void }[] = []
  if (svcSel) activeFilters.push({ label: `svc: ${svcSel}`, clear: () => setSvcSel(null) })
  if (kindSel) activeFilters.push({ label: `kind: ${kindSel}`, clear: () => setKindSel(null) })
  if (tagSel) activeFilters.push({ label: `tag: ${tagSel}`, clear: () => setTagSel(null) })

  return (
    <div style={{ display: 'flex', height: '100%', overflow: 'hidden' }}>
      {/* Sidebar */}
      <aside data-testid="spans-sidebar" style={{
        width: 200, borderRight: '1px solid var(--border)',
        background: 'var(--background)',
        display: 'flex', flexDirection: 'column', overflow: 'hidden auto',
        flexShrink: 0, padding: '12px 0',
      }}>
        <SbGroup title="service">
          <SbItem active={!svcSel} onClick={() => setSvcSel(null)} count={spans.length}>
            all services
          </SbItem>
          {svcFacets.map(([svc, n]) => (
            <SbItem
              key={svc}
              active={svcSel === svc}
              dot={svcColor(svc).fg}
              count={n}
              onClick={() => setSvcSel(svcSel === svc ? null : svc)}
            >{svc}</SbItem>
          ))}
        </SbGroup>

        <SbGroup title="kind">
          {kindFacets.map(([k, n]) => (
            <SbItem
              key={k}
              active={kindSel === k}
              count={n}
              onClick={() => setKindSel(kindSel === k ? null : k)}
            >{k}</SbItem>
          ))}
        </SbGroup>

        <SbGroup title="callouts">
          <SbItem
            active={tagSel === 'n+1'}
            dot="var(--destructive, #c0392b)"
            count={calloutCounts['n+1']}
            onClick={() => setTagSel(tagSel === 'n+1' ? null : 'n+1')}
          >n+1</SbItem>
          <SbItem
            active={tagSel === 'slow'}
            dot="var(--warn, #d97706)"
            count={calloutCounts.slow}
            onClick={() => setTagSel(tagSel === 'slow' ? null : 'slow')}
          >slow (&gt;250ms)</SbItem>
          <SbItem
            active={tagSel === 'lint'}
            dot="var(--warn, #d97706)"
            count={calloutCounts.lint}
            onClick={() => setTagSel(tagSel === 'lint' ? null : 'lint')}
          >lint warnings</SbItem>
          <SbItem
            active={tagSel === 'error'}
            dot="var(--destructive, #c0392b)"
            count={calloutCounts.error}
            onClick={() => setTagSel(tagSel === 'error' ? null : 'error')}
          >errored</SbItem>
        </SbGroup>
      </aside>

      {/* Main */}
      <div style={{
        flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden',
        background: 'var(--background)',
      }}>
        {/* Search + filters bar */}
        <div style={{
          padding: '10px 14px', borderBottom: '1px solid var(--border)',
          display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap',
        }}>
          <div style={{
            display: 'inline-flex', alignItems: 'center', gap: 7,
            background: 'var(--muted)', border: '1px solid var(--border)',
            borderRadius: 8, padding: '0 10px', height: 30, flex: 1, minWidth: 240,
          }}>
            <svg width="13" height="13" viewBox="0 0 14 14" fill="none">
              <circle cx="6" cy="6" r="4" stroke="var(--muted-foreground)" strokeWidth="1.4" />
              <line x1="9.2" y1="9.2" x2="12" y2="12" stroke="var(--muted-foreground)"
                strokeWidth="1.4" strokeLinecap="round" />
            </svg>
            <input
              data-testid="spans-search"
              value={query}
              onChange={e => setQuery(e.target.value)}
              placeholder="search by name, attribute key or value, trace id…"
              style={{
                flex: 1, border: 'none', outline: 'none', background: 'transparent',
                fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--foreground)',
              }}
            />
            {query && (
              <button type="button" onClick={() => setQuery('')} style={{
                background: 'transparent', border: 'none', cursor: 'pointer',
                fontFamily: 'var(--font-mono)', color: 'var(--muted-foreground)', fontSize: 11, padding: 0,
              }}>clear</button>
            )}
          </div>

          {activeFilters.map(f => (
            <FilterChip key={f.label} label={f.label} onClear={f.clear} />
          ))}

          <div style={{
            display: 'inline-flex', alignItems: 'center', gap: 6,
            fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)',
            padding: '0 10px', height: 30, borderRadius: 6,
            background: 'var(--muted)', border: '1px solid var(--border)',
          }}>
            <span>sort</span>
            <select
              data-testid="spans-sort"
              value={sortBy}
              onChange={e => setSortBy(e.target.value as typeof sortBy)}
              style={{
                border: 'none', outline: 'none', background: 'transparent',
                fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--foreground)',
                cursor: 'pointer',
              }}
            >
              <option value="time">time ↓</option>
              <option value="dur">duration ↓</option>
              <option value="name">name a→z</option>
            </select>
          </div>
        </div>

        {/* Column headers */}
        <div style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0,1.6fr) 130px 80px 70px 130px 60px',
          gap: 10, padding: '7px 14px', borderBottom: '1px solid var(--border)',
          fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--muted-foreground)',
          textTransform: 'uppercase', letterSpacing: '0.14em',
          background: 'var(--muted)',
        }}>
          <div>name</div>
          <div>service</div>
          <div>kind</div>
          <div style={{ textAlign: 'right' }}>dur</div>
          <div>trace</div>
          <div style={{ textAlign: 'right' }}>tag</div>
        </div>

        {/* Rows */}
        <div style={{ flex: 1, overflow: 'hidden auto' }}>
          {loading ? (
            <div style={{
              padding: '40px 20px', textAlign: 'center',
              fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)',
            }}>loading…</div>
          ) : filtered.length === 0 ? (
            <div style={{
              padding: '40px 20px', textAlign: 'center',
              fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--muted-foreground)',
            }}>{spans.length === 0 ? 'no spans yet' : 'no spans match — clear a filter or try a different query'}</div>
          ) : filtered.map(s => {
            const isSel = s.span_id === selectedId
            const isSlow = s.duration_ns > SLOW_NS
            const kindLabel = KIND_LABELS[s.kind] ?? 'unknown'
            return (
              <button
                key={s.span_id}
                type="button"
                data-testid={`span-row-${s.span_id}`}
                onClick={() => setSelectedId(isSel ? null : s.span_id)}
                style={{
                  display: 'grid', textAlign: 'left', cursor: 'pointer',
                  gridTemplateColumns: 'minmax(0,1.6fr) 130px 80px 70px 130px 60px',
                  gap: 10, padding: '8px 14px', alignItems: 'center',
                  border: 'none', width: '100%',
                  background: isSel
                    ? 'color-mix(in oklch, var(--accent, #6366f1) 14%, var(--background))'
                    : 'transparent',
                  borderLeft: isSel ? '2px solid var(--accent, #6366f1)' : '2px solid transparent',
                  borderBottom: '1px solid var(--border)',
                  outline: 'none',
                }}
              >
                <div style={{
                  fontFamily: 'var(--font-mono)', fontSize: 11.5, color: 'var(--foreground)',
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                }}>{s.name}</div>
                <div><SvcChip name={s.service_name} /></div>
                <div style={{
                  fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)',
                  textTransform: 'uppercase', letterSpacing: '0.08em',
                }}>{kindLabel}</div>
                <div style={{
                  textAlign: 'right', fontFamily: 'var(--font-mono)', fontSize: 11.5,
                  color: isSlow ? 'var(--destructive, #c0392b)' : 'var(--foreground)', fontWeight: 600,
                }}>{fmtNs(s.duration_ns)}</div>
                <div style={{
                  fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--muted-foreground)',
                  overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                }}>{s.trace_id}</div>
                <div style={{ textAlign: 'right' }}>
                  <TagBadge tag={s.tag} />
                </div>
              </button>
            )
          })}
        </div>

        {/* Status footer */}
        <div style={{
          padding: '8px 14px', borderTop: '1px solid var(--border)',
          background: 'var(--muted)',
          fontFamily: 'var(--font-mono)', fontSize: 10.5, color: 'var(--muted-foreground)',
          display: 'flex', alignItems: 'center', gap: 14,
        }}>
          <span>
            <strong style={{ color: 'var(--foreground)' }}>{filtered.length.toLocaleString()}</strong>
            {' '}of {spans.length.toLocaleString()} spans
          </span>
          <span>·</span>
          <span>last 24h</span>
          <span style={{ flex: 1 }} />
          <code style={{
            padding: '3px 8px', borderRadius: 5, border: '1px solid var(--border)',
            background: 'var(--background)', color: 'var(--muted-foreground)',
            fontFamily: 'var(--font-mono)', fontSize: 10,
          }}>spaniel spans --tail</code>
        </div>
      </div>

      {/* Inspector */}
      {selected && (
        <SpanInspector span={selected} onClose={() => setSelectedId(null)} />
      )}
    </div>
  )
}
