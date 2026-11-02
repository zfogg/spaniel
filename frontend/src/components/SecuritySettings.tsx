import { Toggle } from './ui/toggle'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, Copy, KeyRound, Plus, ShieldCheck, Trash2 } from 'lucide-react'
import { openapiClient } from '@/lib/openapi'
import { unwrap } from '@/lib/api/transport'
import { authenticate } from '@/lib/auth'

const inputClass = 'h-10 rounded-lg border border-border bg-background px-3 text-sm'
function defaultExpiry() {
  const at = new Date()
  at.setFullYear(at.getFullYear() + 1)
  at.setMinutes(at.getMinutes() - at.getTimezoneOffset())
  return at.toISOString().slice(0, 16)
}

export function SecuritySettings({ hidden }: { hidden: boolean }) {
  const status = useQuery({
    queryKey: ['security'],
    queryFn: () => unwrap(openapiClient.GET('/api/security')),
    enabled: !hidden,
  })
  const tokens = useQuery({
    queryKey: ['security-tokens'],
    queryFn: () => unwrap(openapiClient.GET('/api/security/tokens')),
    enabled: !hidden,
  })
  const [name, setName] = useState('')
  const [expires, setExpires] = useState(defaultExpiry)
  const [noExpiry, setNoExpiry] = useState(false)
  const [password, setPassword] = useState('')
  const [secret, setSecret] = useState('')
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError('')
    try {
      await action()
      await Promise.all([status.refetch(), tokens.refetch()])
    } catch (error) {
      setError(error instanceof Error ? error.message : 'Request failed. Try again.')
    } finally {
      setBusy(false)
    }
  }
  if (hidden) return null
  if (status.isPending || tokens.isPending)
    return (
      <p role="status" className="p-6 text-sm text-muted-foreground">
        Loading security settings…
      </p>
    )
  if (status.error || tokens.error)
    return (
      <div role="alert" className="rounded-xl border border-border p-6">
        <p>Unable to load security settings.</p>
        <button
          onClick={() =>
            void run(async () => {
              await Promise.all([
                status.refetch({ throwOnError: true }),
                tokens.refetch({ throwOnError: true }),
              ])
            })
          }
        >
          Try again
        </button>
        {error && <p>{error}</p>}
      </div>
    )
  const settings = status.data!.data
  const rows = tokens.data!.data.filter((token) => token.status !== 'revoked')
  return (
    <section className="grid gap-5" aria-label="API token security">
      <div className="rounded-xl border border-border bg-card p-6">
        <div className="flex items-start justify-between gap-6">
          <div className="flex gap-3">
            <ShieldCheck className="mt-1 text-accent-d" size={22} />
            <div>
              <h2 className="text-lg font-semibold">API token security</h2>
              <p className="mt-1 max-w-lg text-sm text-muted-foreground">
                Protect the API, live connections, MCP, and telemetry ingestion with named tokens.
              </p>
            </div>
          </div>
          <label className="flex shrink-0 items-center gap-3 text-sm">
            <span>{settings.enabled ? 'Enabled' : 'Disabled'}</span>
            <Toggle
              label="Enable API token security"
              on={settings.enabled}
              disabled={busy || settings.enabled_source !== 'config'}
              onChange={(enabled) =>
                void run(() => unwrap(openapiClient.PUT('/api/security', { body: { enabled } })))
              }
            />
          </label>
        </div>
        {settings.enabled_source !== 'config' && (
          <p className="mt-3 text-xs text-muted-foreground">
            Controlled by {settings.enabled_source}. Change the override and restart Spaniel.
          </p>
        )}
        {!settings.enabled && (
          <p className="mt-4 rounded-lg bg-muted px-3 py-2 text-xs text-muted-foreground">
            Create and copy a token before enabling protection. Anyone who can reach this server
            currently has access.
          </p>
        )}
      </div>
      {error && (
        <p
          role="alert"
          className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"
        >
          {error}
        </p>
      )}
      {secret && (
        <div className="rounded-xl border border-accent-d/40 bg-accent-d/5 p-5">
          <h3 className="flex items-center gap-2 font-semibold">
            <KeyRound size={17} />
            Copy your token now
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            This secret is shown once. Store it somewhere safe.
          </p>
          <div className="mt-3 flex gap-2">
            <input
              aria-label="Created API token"
              readOnly
              value={secret}
              className={`${inputClass} min-w-0 flex-1 font-mono`}
            />
            <button
              aria-label="Copy API token"
              className="rounded-lg border border-border px-3"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(secret)
                  setCopied(true)
                } catch {
                  setError('Copy failed. Select and copy the token manually.')
                }
              }}
            >
              {copied ? <Check size={16} /> : <Copy size={16} />}
            </button>
          </div>
          <button className="mt-3 text-sm underline" onClick={() => setSecret('')}>
            I saved it · dismiss
          </button>
        </div>
      )}
      <div className="overflow-hidden rounded-xl border border-border bg-card">
        <div className="border-b border-border px-6 py-4">
          <h2 className="font-semibold">
            API tokens{' '}
            <span className="ml-2 rounded-md bg-muted px-2 py-0.5 text-xs text-muted-foreground">
              {rows.length}
            </span>
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Use a separate token for each service. All tokens grant full access.
          </p>
        </div>
        {rows.length ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-muted/30 text-xs text-muted-foreground">
                <tr>
                  <th className="px-6 py-3">Name</th>
                  <th className="px-4 py-3">Created</th>
                  <th className="px-4 py-3">Expires</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {rows.map((token) => (
                  <tr key={token.id} className="border-t border-border">
                    <td className="px-6 py-4">
                      <div className="font-medium">{token.name}</div>
                      <code className="text-xs text-muted-foreground">{token.prefix}…</code>
                    </td>
                    <td className="px-4 py-4 text-xs text-muted-foreground">
                      {new Date(token.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-4 text-xs text-muted-foreground">
                      {token.expires_at ? new Date(token.expires_at).toLocaleString() : 'Never'}
                    </td>
                    <td className="px-4 py-4">
                      <span
                        className={`rounded-full px-2 py-1 text-xs ${token.status === 'active' ? 'bg-emerald-500/10 text-emerald-600' : 'bg-amber-500/10 text-amber-600'}`}
                      >
                        {token.status}
                      </span>
                    </td>
                    <td className="px-4 py-4">
                      <button
                        aria-label={`Delete ${token.name}`}
                        disabled={busy}
                        title="Revoke token and its sessions"
                        onClick={() => {
                          if (
                            window.confirm(
                              `Delete “${token.name}”? Its clients and browser sessions will lose access immediately.`,
                            )
                          )
                            void run(() =>
                              unwrap(
                                openapiClient.DELETE('/api/security/tokens/{tokenId}', {
                                  params: { path: { tokenId: token.id } },
                                }),
                              ),
                            )
                        }}
                        className="rounded-lg p-2 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                      >
                        <Trash2 size={16} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="grid justify-items-center gap-2 px-6 py-10 text-center">
            <KeyRound className="text-muted-foreground" />
            <h3 className="font-medium">No API tokens yet</h3>
            <p className="text-sm text-muted-foreground">Create your first named token below.</p>
          </div>
        )}
        <form
          className="grid gap-4 border-t border-border p-6"
          onSubmit={(event) => {
            event.preventDefault()
            void run(async () => {
              const result = await unwrap(
                openapiClient.POST('/api/security/tokens', {
                  body: {
                    name,
                    no_expiry: noExpiry,
                    ...(noExpiry ? {} : { expires_at: new Date(expires).toISOString() }),
                  },
                }),
              )
              setSecret(result.data.secret)
              setCopied(false)
              setName('')
              if (!settings.enabled) await authenticate(result.data.secret)
            })
          }}
        >
          <h3 className="text-sm font-semibold">Create a token</h3>
          <div className="flex flex-wrap items-end gap-4">
            <label className="grid flex-1 gap-2 text-xs text-muted-foreground">
              Token name
              <input
                required
                maxLength={128}
                placeholder="e.g. backend, keycloak, temporal"
                value={name}
                onChange={(event) => setName(event.target.value)}
                className={inputClass}
              />
            </label>
            {!noExpiry && (
              <label className="grid gap-2 text-xs text-muted-foreground">
                Expires at · local time
                <input
                  required
                  type="datetime-local"
                  aria-label="Token expiry"
                  value={expires}
                  onChange={(event) => setExpires(event.target.value)}
                  className={inputClass}
                />
              </label>
            )}
            <label className="flex h-10 items-center gap-2 text-sm">
              <Toggle label="No expiry" on={noExpiry} onChange={setNoExpiry} />
              No expiry
            </label>
            <button
              disabled={busy || !name.trim()}
              className="flex h-10 items-center gap-2 rounded-lg bg-accent-d px-4 text-sm text-white disabled:opacity-50"
            >
              <Plus size={16} />
              {busy ? 'Working…' : 'Create token'}
            </button>
          </div>
        </form>
      </div>
      <div className="rounded-xl border border-border bg-card p-6">
        <h2 className="font-semibold">Encryption at rest</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          Tokens live in a private security database, encrypted separately from telemetry. The
          password is stored in the server’s settings file.
        </p>
        <form
          className="mt-4 flex flex-wrap items-end gap-3"
          onSubmit={(event) => {
            event.preventDefault()
            void run(async () => {
              await unwrap(
                openapiClient.PUT('/api/security', { body: { encryption_password: password } }),
              )
              setPassword('')
            })
          }}
        >
          <label className="grid flex-1 gap-2 text-xs text-muted-foreground">
            New encryption password
            <input
              type="password"
              autoComplete="new-password"
              minLength={16}
              maxLength={1024}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={settings.password_source !== 'config'}
              className={inputClass}
            />
          </label>
          <button
            disabled={busy || password.length < 16 || settings.password_source !== 'config'}
            className="h-10 rounded-lg border border-border px-4 text-sm disabled:opacity-50"
          >
            Change password
          </button>
        </form>
        <p className="mt-2 text-xs text-muted-foreground">
          {settings.password_source === 'config'
            ? 'Use at least 16 characters. Changing the password keeps existing tokens valid.'
            : `Controlled by ${settings.password_source}. Change that override and restart Spaniel.`}
        </p>
      </div>
      <details className="rounded-xl border border-border p-5 text-sm text-muted-foreground">
        <summary className="cursor-pointer font-medium">Recovery and client setup</summary>
        <p className="mt-3">
          Send <code>Authorization: Bearer &lt;token&gt;</code> with API, MCP, or OTLP requests. Set
          OTLP exporter headers for each service. If locked out, restart with{' '}
          <code>--api-token-security=false</code>, create a replacement token here, and re-enable
          security. Environment variables and flags override <code>~/.spaniel/config.yaml</code>.
        </p>
      </details>
    </section>
  )
}
