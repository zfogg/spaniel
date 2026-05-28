import type { ReactNode } from 'react'
import { ErrorBoundary, type FallbackProps } from 'react-error-boundary'
import { useLocation } from 'react-router-dom'

// Fallback shown when a route view throws during render. The app chrome (nav,
// bottom bar) stays mounted because the boundary only wraps the routed content,
// so the user can still navigate away to recover.
function RouteErrorFallback({ error, resetErrorBoundary }: FallbackProps) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div
      role="alert"
      className="flex h-full flex-col items-center justify-center gap-4 px-6 text-center"
    >
      <svg width="36" height="36" viewBox="0 0 32 32" fill="none" aria-hidden="true">
        <circle cx="16" cy="16" r="12" stroke="var(--danger)" strokeWidth="1.5" opacity="0.7" />
        <path d="M16 9 L16 17" stroke="var(--danger)" strokeWidth="2" strokeLinecap="round" />
        <circle cx="16" cy="21.5" r="1.3" fill="var(--danger)" />
      </svg>

      <div>
        <div className="font-sans text-[15px] font-semibold text-ink">This view hit an error</div>
        <div className="mt-1 font-mono text-[11px] text-ink3">
          The rest of spaniel is fine — try again or switch tabs.
        </div>
      </div>

      {message && (
        <pre className="max-w-[560px] overflow-auto rounded-md border border-line bg-surface2 px-3 py-2 text-left font-mono text-[11px] text-danger-ink">
          {message}
        </pre>
      )}

      <div className="flex gap-2">
        <button
          type="button"
          onClick={resetErrorBoundary}
          className="rounded-md border border-line bg-surface2 px-3 py-1.5 font-mono text-[11px] text-ink transition-colors hover:bg-surface3"
        >
          Try again
        </button>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="rounded-md border border-line bg-transparent px-3 py-1.5 font-mono text-[11px] text-ink2 transition-colors hover:bg-surface2"
        >
          Reload page
        </button>
      </div>
    </div>
  )
}

// Wraps routed content. Keying the reset on the current pathname means
// navigating to another route automatically clears a caught error, so a broken
// page never gets "stuck" once the user moves away.
export default function RouteErrorBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  return (
    <ErrorBoundary FallbackComponent={RouteErrorFallback} resetKeys={[pathname]}>
      {children}
    </ErrorBoundary>
  )
}
