import { afterEach, describe, expect, it, vi } from 'vitest'
import { authenticate, authenticatedFetch, getAuthState } from './auth'

afterEach(() => vi.unstubAllGlobals())
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('token authentication transport', () => {
  it('coordinates concurrent failures, rejects wrong credentials, then retries each request once', async () => {
    let authorized = false
    let sessions = 0
    const requests: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input)
        if (url === '/api/security/session') {
          sessions++
          if (JSON.parse(String(init?.body)).token !== 'correct')
            return json({ code: 'token_invalid', error: 'The API token is invalid.' }, 401)
          authorized = true
          return new Response(null, { status: 204 })
        }
        requests.push(url)
        return authorized ? json({ data: true }) : json({ code: 'token_required' }, 401)
      }),
    )
    const a = authenticatedFetch('/api/settings')
    const b = authenticatedFetch('/api/sql')
    await vi.waitFor(() => expect(getAuthState().required).toBe(true))
    await expect(authenticate('wrong')).rejects.toThrow('invalid')
    expect(getAuthState().required).toBe(true)
    await authenticate('correct')
    expect((await a).status).toBe(200)
    expect((await b).status).toBe(200)
    expect(sessions).toBe(2)
    expect(requests).toEqual(['/api/settings', '/api/sql', '/api/settings', '/api/sql'])
  })

  it('does not retry network or server errors', async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(json({ error: 'unavailable' }, 503))
      .mockRejectedValueOnce(new Error('offline'))
    vi.stubGlobal('fetch', fetch)
    expect((await authenticatedFetch('/api/settings')).status).toBe(503)
    await expect(authenticatedFetch('/api/settings')).rejects.toThrow('offline')
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(getAuthState().required).toBe(false)
  })
})
