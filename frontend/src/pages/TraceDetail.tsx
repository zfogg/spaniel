import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, Span, LintWarning } from '@/lib/api'
import TraceWaterfall from '@/components/TraceWaterfall'

export default function TraceDetail() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const [spans, setSpans]     = useState<Span[]>([])
  const [warnings, setWarnings] = useState<LintWarning[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!traceId) return
    api.traces.get(traceId).then(r => {
      setSpans(r.data ?? [])
      setLoading(false)
    })
    api.lint.list().then(r => {
      setWarnings((r.data ?? []).filter(w => w.trace_id === traceId))
    })
  }, [traceId])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div style={{
        display: 'flex', alignItems: 'center', gap: 10,
        padding: '6px 14px',
        borderBottom: '1px solid var(--border)',
        flexShrink: 0,
      }}>
        <button
          onClick={() => navigate(-1)}
          style={{
            background: 'none', border: 'none', cursor: 'pointer',
            color: 'var(--muted-foreground)', fontSize: 16, lineHeight: 1,
            padding: '2px 4px', borderRadius: 4,
          }}
        >
          ←
        </button>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 10,
          color: 'var(--muted-foreground)',
          overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
        }}>
          {traceId}
        </span>
      </div>

      {loading ? (
        <div style={{
          flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center',
          color: 'var(--muted-foreground)', fontFamily: 'var(--font-mono)', fontSize: 13,
        }}>
          Loading…
        </div>
      ) : (
        <div style={{ flex: 1, overflow: 'hidden' }}>
          <TraceWaterfall
            spans={spans}
            warnings={warnings}
            traceId={traceId}
          />
        </div>
      )}
    </div>
  )
}
