import type { ReactNode } from 'react'

interface ErrorStateProps {
  // What failed to load, e.g. "traces" or "the service map". Used to build the
  // headline ("Couldn't load traces").
  what: string
  // The thrown error from the failed query, if any — its message is surfaced
  // verbatim so a 401/500/network failure is distinguishable at a glance.
  error?: unknown
  // Wire this to react-query's refetch() to give the user a one-click retry.
  onRetry?: () => void
  glyph?: ReactNode
}

function messageOf(error: unknown): string {
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return ''
}

// Shown when a data fetch fails (network error, server down, 4xx/5xx). This is
// deliberately distinct from EmptyState: an empty dataset ("no traces yet") and
// an unreachable backend ("couldn't load traces") are different situations and
// must not look the same, or a stopped spaniel reads as "everything is fine,
// there's just no data."
export default function ErrorState({ what, error, onRetry, glyph }: ErrorStateProps) {
  const detail = messageOf(error)
  return (
    <div
      data-testid="error-state"
      role="alert"
      className="flex h-full flex-col items-center justify-center gap-3 p-10 text-center"
    >
      <div className="mb-1 text-[var(--danger)] opacity-80">
        {glyph ?? (
          <svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0Z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
        )}
      </div>
      <div className="font-mono text-[13px] font-semibold text-foreground">Couldn't load {what}</div>
      <div className="max-w-[380px] font-mono text-[11px] leading-relaxed text-muted-foreground">
        {detail
          ? <>The server returned an error: <span className="text-foreground">{detail}</span>.</>
          : <>Couldn't reach spaniel. Is the server still running?</>}
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-1 rounded-md border border-border bg-transparent px-3 py-1 font-mono text-[11px] text-foreground transition-colors hover:bg-muted"
        >
          Retry
        </button>
      )}
    </div>
  )
}
