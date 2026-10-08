import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const NativeRequest = globalThis.Request
let api: (typeof import('./api'))['api']
const fetchMock = vi.fn<typeof fetch>()

beforeEach(async () => {
  vi.resetModules()
  fetchMock.mockReset()
  // The browser resolves relative API URLs against its origin; Node needs that origin explicitly.
  vi.stubGlobal(
    'Request',
    class extends NativeRequest {
      constructor(input: RequestInfo | URL, init?: RequestInit) {
        super(typeof input === 'string' ? new URL(input, 'http://spaniel.test') : input, init)
      }
    },
  )
  vi.stubGlobal('fetch', fetchMock)
  ;({ api } = await import('./api'))
})

afterEach(() => vi.unstubAllGlobals())

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function lastRequest() {
  return fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0] as Request
}

describe('endpoint request and response compatibility', () => {
  it('preserves alert pagination defaults, query mapping, and timeline silences', async () => {
    fetchMock.mockResolvedValueOnce(json({ data: { items: [], summary: {} }, meta: { total: 0 } }))
    await api.alerts.list({ search: 'slow requests' })
    expect(new URL(lastRequest().url).searchParams.get('page')).toBe('1')
    expect(new URL(lastRequest().url).searchParams.get('limit')).toBe('15')
    expect(new URL(lastRequest().url).searchParams.get('search')).toBe('slow requests')
    const envelope = {
      data: [],
      meta: { total: 0, page: 2, limit: 15 },
      silences: [{ id: 'quiet' }],
    }
    fetchMock.mockResolvedValueOnce(json(envelope))
    expect(await api.alerts.events('rule', { groupKey: 'service=api', page: 2 })).toEqual(envelope)
    expect(new URL(lastRequest().url).pathname).toBe('/api/alerts/rule/events')
    expect(new URL(lastRequest().url).searchParams.get('group_key')).toBe('service=api')
  })

  it('sends dashboard preview bodies and propagates cancellation', async () => {
    const controller = new AbortController()
    const payload = { query_sql: 'SELECT 42 AS value', variables: { service: 'api' } }
    fetchMock.mockResolvedValueOnce(
      json({ data: { rows: [{ value: 42 }], columns: ['value'], warnings: [] } }),
    )
    await api.dashboards.preview('board', payload, controller.signal)
    const request = lastRequest()
    expect(request.method).toBe('POST')
    expect(new URL(request.url).pathname).toBe('/api/dashboards/board/query-preview')
    expect(await request.json()).toEqual(payload)
    controller.abort()
    expect(request.signal.aborted).toBe(true)
  })

  it('maps metric-series query aliases and keeps span/group filtering', async () => {
    fetchMock.mockResolvedValueOnce(json({ data: {} }))
    await api.metrics.series({
      name: 'latency',
      withTraces: true,
      dimensionFilters: { region: 'east' },
    })
    const url = new URL(lastRequest().url)
    expect(url.searchParams.get('with_traces')).toBe('true')
    expect(url.searchParams.get('attributes[region]')).toBe('east')
    const rows = [{ span_id: 'span' }, { count: 3 }]
    fetchMock.mockResolvedValueOnce(json({ data: rows }))
    expect((await api.spans.list()).data).toEqual([rows[0]])
    fetchMock.mockResolvedValueOnce(json({ data: rows }))
    expect((await api.spans.groups()).data).toEqual([rows[1]])
    expect(new URL(lastRequest().url).searchParams.get('view')).toBe('grouped')
  })

  it.each(['alerts', 'dashboards'] as const)(
    'retains raw YAML export/import for %s',
    async (resource) => {
      const yaml = 'name: Example\n'
      fetchMock.mockResolvedValueOnce(new Response(yaml))
      expect(await api[resource].config('example')).toBe(yaml)
      expect(fetchMock).toHaveBeenLastCalledWith(`/api/${resource}/example/config`)
      const envelope = { data: { id: 'example' } }
      fetchMock.mockResolvedValueOnce(json(envelope))
      expect(await api[resource].importConfig(yaml)).toEqual(envelope)
      expect(fetchMock).toHaveBeenLastCalledWith(`/api/${resource}/import`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/yaml' },
        body: yaml,
      })
      fetchMock.mockResolvedValueOnce(new Response('Invalid YAML', { status: 400 }))
      await expect(api[resource].importConfig(yaml)).rejects.toThrow('Invalid YAML')
    },
  )

  it('retains JSON error messages and bearer-cookie seeding', async () => {
    fetchMock.mockResolvedValueOnce(json({ error: 'Query is read-only' }, 400))
    await expect(
      api.dashboards.preview('board', { query_sql: 'DELETE FROM spans' }),
    ).rejects.toThrow('Query is read-only')
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 503, statusText: 'Unavailable' }))
    await expect(api.health.get()).rejects.toThrow('503 Unavailable')
    fetchMock.mockResolvedValueOnce(json({ data: { status: 'ok' } }))
    await api.health.seed('test-token')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/health', {
      headers: { Authorization: 'Bearer test-token' },
    })
  })
})
