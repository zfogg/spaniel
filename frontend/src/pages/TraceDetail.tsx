import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, Span, LintWarning, TraceIssue } from '@/lib/api'
import TraceWaterfall from '@/components/TraceWaterfall'

export default function TraceDetail() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const [spans, setSpans]       = useState<Span[]>([])
  const [warnings, setWarnings] = useState<LintWarning[]>([])
  const [issues, setIssues]     = useState<TraceIssue[]>([])
  const [loading, setLoading]   = useState(true)

  useEffect(() => {
    if (!traceId) return
    api.traces.get(traceId).then(r => {
      setSpans(r.data ?? [])
      setLoading(false)
    })
    api.lint.list().then(r => {
      setWarnings((r.data ?? []).filter(w => w.trace_id === traceId))
    })
    api.issues.get(traceId).then(r => {
      setIssues(r.data ?? [])
    }).catch(() => { /* issues endpoint is optional */ })
  }, [traceId])

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center gap-2.5 px-3.5 py-1.5 border-b border-border shrink-0">
        <button
          onClick={() => navigate(-1)}
          className="bg-none border-none cursor-pointer text-muted-foreground text-base leading-none px-1 py-0.5 rounded"
        >
          ←
        </button>
        <span className="font-mono text-[10px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap">
          {traceId}
        </span>
      </div>

      {loading ? (
        <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">
          Loading…
        </div>
      ) : (
        <div className="flex-1 overflow-hidden">
          <TraceWaterfall
            spans={spans}
            warnings={warnings}
            issues={issues}
            traceId={traceId}
          />
        </div>
      )}
    </div>
  )
}
