// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
let SecuritySettings: (typeof import('./SecuritySettings'))['SecuritySettings']

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})
const NativeRequest = globalThis.Request
beforeEach(async () => {
  vi.resetModules()
  vi.stubGlobal(
    'Request',
    class extends NativeRequest {
      constructor(input: RequestInfo | URL, init?: RequestInit) {
        super(typeof input === 'string' ? new URL(input, 'http://spaniel.test') : input, init)
      }
    },
  )
  ;({ SecuritySettings } = await import('./SecuritySettings'))
})
function mount() {
  return render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <SecuritySettings hidden={false} />
    </QueryClientProvider>,
  )
}
const json = (data: unknown) =>
  new Response(JSON.stringify({ data }), { headers: { 'Content-Type': 'application/json' } })
describe('Security settings', () => {
  it('shows loading, creates named tokens once, and hides expiry when no expiry is selected', async () => {
    let created = false
    const fetch = vi.fn(async (request: RequestInfo | URL) => {
      const r = request as Request
      const path = new URL(r.url).pathname
      if (path === '/api/security/tokens' && r.method === 'POST') {
        const body = await r.json()
        expect(body).toEqual({ name: 'backend', no_expiry: true })
        created = true
        return json({ token: { id: 'test-id' }, secret: 'safe-example-secret' })
      }
      if (path === '/api/security/tokens')
        return json(
          created
            ? [
                {
                  id: 'test-id',
                  name: 'backend',
                  prefix: 'spn_example',
                  created_at: '2026-11-02T12:00:00Z',
                  expires_at: null,
                  status: 'active',
                },
              ]
            : [],
        )
      return json({
        enabled: true,
        encryption_password_set: true,
        enabled_source: 'config',
        password_source: 'config',
      })
    })
    vi.stubGlobal('fetch', fetch)
    mount()
    expect(screen.getByRole('status').textContent).toContain('Loading')
    await screen.findByText('No API tokens yet')
    expect(screen.getByLabelText('Token expiry')).toBeTruthy()
    fireEvent.click(screen.getByRole('switch', { name: 'No expiry' }))
    expect(screen.queryByLabelText('Token expiry')).toBeNull()
    fireEvent.change(screen.getByPlaceholderText('e.g. backend, keycloak, temporal'), {
      target: { value: 'backend' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }))
    await screen.findByText('Copy your token now')
    expect((screen.getByLabelText('Created API token') as HTMLInputElement).value).toBe(
      'safe-example-secret',
    )
    fireEvent.click(screen.getByText('I saved it · dismiss'))
    expect(screen.queryByLabelText('Created API token')).toBeNull()
    await screen.findByText('backend')
  })

  it('shows load failures with a working retry', async () => {
    let failed = true
    vi.stubGlobal(
      'fetch',
      vi.fn(async (request: RequestInfo | URL) => {
        if (failed)
          return new Response(JSON.stringify({ error: 'server unavailable' }), { status: 503 })
        return String((request as Request).url).endsWith('/tokens')
          ? json([])
          : json({ enabled: false, enabled_source: 'config', password_source: 'config' })
      }),
    )
    mount()
    await screen.findByText('Unable to load security settings.')
    failed = false
    fireEvent.click(screen.getByText('Try again'))
    await waitFor(() => expect(screen.getByText('No API tokens yet')).toBeTruthy())
  })
})
