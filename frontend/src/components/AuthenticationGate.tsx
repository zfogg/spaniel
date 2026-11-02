import { useState, useSyncExternalStore } from 'react'
import { KeyRound, ShieldCheck } from 'lucide-react'
import { authenticate, getAuthState, subscribeAuth } from '@/lib/auth'

export function AuthenticationGate() {
  const state = useSyncExternalStore(subscribeAuth, getAuthState)
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  if (!state.required) return null
  return (
    <div className="fixed inset-0 z-[100] grid place-items-center bg-background/95 p-6 backdrop-blur-md">
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="auth-title"
        className="w-full max-w-md rounded-2xl border border-border bg-card p-8 shadow-xl"
      >
        <div className="mb-6 flex h-12 w-12 items-center justify-center rounded-xl bg-accent-d/10 text-accent-d">
          <ShieldCheck size={24} />
        </div>
        <h1 id="auth-title" className="text-2xl font-semibold tracking-tight">
          Unlock Spaniel
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          Enter an API token to continue to this page.
        </p>
        {state.code !== 'token_required' && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {state.code === 'token_expired'
              ? 'Your token has expired.'
              : state.code === 'token_revoked'
                ? 'Your token has been revoked.'
                : 'Your token is invalid.'}{' '}
            Enter another token.
          </p>
        )}
        <form
          className="mt-6 grid gap-4"
          onSubmit={async (event) => {
            event.preventDefault()
            setLoading(true)
            setError('')
            try {
              await authenticate(token)
              setToken('')
            } catch (error) {
              setError(error instanceof Error ? error.message : 'Unable to connect to Spaniel.')
            } finally {
              setLoading(false)
            }
          }}
        >
          <label className="grid gap-2 text-sm font-medium">
            API token
            <input
              autoFocus
              type="password"
              autoComplete="off"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              className="h-11 rounded-lg border border-border bg-background px-3 font-mono text-sm"
              placeholder="spn_…"
            />
          </label>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <button
            disabled={loading || !token.trim()}
            className="flex h-11 items-center justify-center gap-2 rounded-lg bg-accent-d text-white disabled:opacity-50"
          >
            <KeyRound size={16} />
            {loading ? 'Checking token…' : 'Unlock'}
          </button>
        </form>
        <details className="mt-6 border-t border-border pt-4 text-xs text-muted-foreground">
          <summary className="cursor-pointer">Locked out? Recover access</summary>
          <p className="mt-3 leading-relaxed">
            On the server, restart Spaniel with <code>--api-token-security=false</code>, or set{' '}
            <code>SPANIEL_API_TOKEN_SECURITY=false</code>. You can also set{' '}
            <code>api_token_security: false</code> in <code>~/.spaniel/config.yaml</code> and
            restart. Flags and environment variables override the file. Create a replacement token
            in Settings → Security, then re-enable protection.
          </p>
        </details>
      </section>
    </div>
  )
}
