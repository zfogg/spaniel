import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { qk } from '@/lib/query'
import { api } from '@/lib/api'
import TraceWaterfall from '@/components/TraceWaterfall'
import EmptyState from '@/components/EmptyState'
import ErrorState from '@/components/ErrorState'

function ShareIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <circle cx="13" cy="3"  r="2" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="13" cy="13" r="2" stroke="currentColor" strokeWidth="1.5" />
      <circle cx="3"  cy="8"  r="2" stroke="currentColor" strokeWidth="1.5" />
      <line x1="11.1" y1="4.2"  x2="4.9" y2="6.8"  stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      <line x1="4.9"  y1="9.2"  x2="11.1" y2="11.8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function DownloadIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" aria-hidden="true">
      <path d="M8 2 L8 10 M5 7 L8 10 L11 7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M3 13 L13 13" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  )
}

function HeaderButton({ onClick, title, children, testid }: {
  onClick: () => void
  title: string
  children: React.ReactNode
  testid?: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      data-testid={testid}
      className="inline-flex items-center justify-center w-[26px] h-[26px] rounded-md border border-border bg-transparent text-muted-foreground cursor-pointer outline-none transition-colors hover:bg-muted hover:text-foreground shrink-0"
    >
      {children}
    </button>
  )
}

export default function TraceDetail() {
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()
  const [copied, setCopied]     = useState(false)
  const copiedTimer             = useRef<ReturnType<typeof setTimeout> | null>(null)

  const { data: spans = [], isLoading: loading, isError, error, refetch } = useQuery({
    queryKey: qk.trace(traceId ?? ''),
    queryFn: () => api.traces.get(traceId!).then(r => r.data ?? []),
    enabled: !!traceId,
  })
  const { data: warnings = [] } = useQuery({
    queryKey: qk.lint(),
    queryFn: () => api.lint.list().then(r => r.data ?? []),
    enabled: !!traceId,
    select: rows => rows.filter(w => w.trace_id === traceId),
  })
  const { data: issues = [] } = useQuery({
    queryKey: qk.issues(traceId ?? ''),
    queryFn: () => api.issues.get(traceId!).then(r => r.data ?? []),
    enabled: !!traceId,
  })

  useEffect(() => () => { if (copiedTimer.current) clearTimeout(copiedTimer.current) }, [])

  function handleShare() {
    navigator.clipboard.writeText(window.location.href).then(() => {
      setCopied(true)
      if (copiedTimer.current) clearTimeout(copiedTimer.current)
      copiedTimer.current = setTimeout(() => setCopied(false), 2000)
    })
  }

  function handleDownload() {
    if (!traceId) return
    const a = document.createElement('a')
    a.href = api.traces.exportUrl(traceId)
    a.download = `trace-${traceId.slice(0, 8)}.json`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="flex items-center gap-2 px-3.5 py-1.5 border-b border-border shrink-0">
        <button
          onClick={() => navigate(-1)}
          className="bg-none border-none cursor-pointer text-muted-foreground text-base leading-none px-1 py-0.5 rounded"
        >
          ←
        </button>
        <span className="font-mono text-[10px] text-muted-foreground overflow-hidden text-ellipsis whitespace-nowrap flex-1 min-w-0">
          {traceId}
        </span>

        {/* permalink */}
        <div className="relative shrink-0">
          <HeaderButton onClick={handleShare} title="Copy permalink" testid="btn-share">
            <ShareIcon />
          </HeaderButton>
          {copied && (
            <div
              className="absolute right-0 top-full mt-1.5 px-2 py-1 rounded-md bg-foreground text-background font-mono text-[10px] whitespace-nowrap pointer-events-none z-50"
              data-testid="share-toast"
            >
              link copied
            </div>
          )}
        </div>

        {/* download OTLP JSON */}
        <HeaderButton onClick={handleDownload} title="Download as OTLP JSON" testid="btn-download">
          <DownloadIcon />
        </HeaderButton>
      </div>

      {loading ? (
        <div className="flex-1 flex items-center justify-center text-muted-foreground font-mono text-[13px]">
          Loading…
        </div>
      ) : isError ? (
        <ErrorState what="this trace" error={error} onRetry={() => refetch()} />
      ) : spans.length === 0 ? (
        <EmptyState
          title="Trace not found"
          hint="This trace has no spans — it may have aged out of retention, or the id is wrong."
        />
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
