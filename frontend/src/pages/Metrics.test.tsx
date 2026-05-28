// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Metrics from './Metrics'
import { bucketPoints } from '@/lib/metrics-bucket'

// Metrics uses useNavigate() (in the correlated-traces panel) which needs
// a Router context. Wrap once so the existing test cases stay unchanged.
const renderMetrics = () => render(<MemoryRouter><Metrics /></MemoryRouter>)
import type { MetricCatalogEntry, MetricSeries } from '@/lib/api'

// ── fetch mock ──────────────────────────────────────────────────────────────
// Routes per-path responses; uses the latest setup() values when called.

let routes: Record<string, unknown> = {}

function setup(opts: { catalog?: MetricCatalogEntry[]; series?: Record<string, MetricSeries> }) {
  routes = {
    '/api/metrics': { data: opts.catalog ?? [], meta: { total: 0, page: 1 } },
  }
  for (const [key, series] of Object.entries(opts.series ?? {})) {
    routes['/api/metrics/series?' + key] = { data: series, meta: { total: 0, page: 1 } }
  }
}

beforeEach(() => {
  routes = {}
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    // match by path; ignore query param order and additional params like 'from'
    for (const [routePath, body] of Object.entries(routes)) {
      const routeBase = routePath.split('?')[0]
      const urlBase = url.split('?')[0]
      if (urlBase === routeBase) {
        // check if all required query params are present
        const routeParams = new URLSearchParams(routePath.split('?')[1] || '')
        const urlParams = new URLSearchParams(url.split('?')[1] || '')
        let allMatch = true
        for (const [key, value] of routeParams.entries()) {
          if (urlParams.get(key) !== value) {
            allMatch = false
            break
          }
        }
        if (allMatch) {
          return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
        }
      }
    }
    return new Response('{}', { status: 404 })
  }))
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

// ── tests ───────────────────────────────────────────────────────────────────

describe('<Metrics />', () => {
  it('shows the loading state initially', () => {
    setup({ catalog: [] })
    renderMetrics()
    expect(screen.getByText(/Loading metrics/i)).toBeTruthy()
  })

  it('shows an empty-state message when the catalog is empty', async () => {
    setup({ catalog: [] })
    renderMetrics()
    await waitFor(() => {
      expect(screen.getByText(/No metrics yet/i)).toBeTruthy()
    })
    expect(screen.getByText(/localhost:4318\/v1\/metrics/)).toBeTruthy()
  })

  it('groups the catalog by service and shows each metric name + kind tag', async () => {
    setup({
      catalog: [
        { name: 'http.requests',  service_name: 'api',      type: 'counter',   unit: 'req',  description: '', sample_count: 12 },
        { name: 'http.dur',       service_name: 'api',      type: 'histogram', unit: 'ms',   description: '', sample_count: 30 },
        { name: 'pool.in_use',    service_name: 'postgres', type: 'gauge',     unit: 'conn', description: '', sample_count: 8 },
      ],
      series: {
        'name=http.requests&service=api&with_traces=1': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req',
          description: 'request count',
          points: [
            { timestamp_ns: 1, value: 5 },
            { timestamp_ns: 2, value: 8 },
          ],
          traces: [],
        },
      },
    })

    renderMetrics()

    // sidebar entries land
    await waitFor(() => {
      expect(screen.getByText('http.requests')).toBeTruthy()
      expect(screen.getByText('http.dur')).toBeTruthy()
      expect(screen.getByText('pool.in_use')).toBeTruthy()
    })

    // service group headers (may also appear elsewhere; we just want presence).
    expect(screen.getAllByText('api').length).toBeGreaterThan(0)
    expect(screen.getAllByText('postgres').length).toBeGreaterThan(0)

    // kind tags rendered (one per metric, uppercase per the tag component)
    expect(screen.getAllByText(/counter/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/histogram/i).length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText(/gauge/i).length).toBeGreaterThanOrEqual(1)
  })

  it('filters by search query', async () => {
    setup({
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter', unit: 'req',  description: '', sample_count: 1 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',   unit: 'conn', description: '', sample_count: 1 },
      ],
    })
    renderMetrics()
    await waitFor(() => screen.getByText('http.requests'))

    const input = screen.getByPlaceholderText(/search metrics/i)
    fireEvent.change(input, { target: { value: 'pool' } })

    expect(screen.queryByText('http.requests')).toBeNull()
    expect(screen.getByText('pool.in_use')).toBeTruthy()
  })

  it('filters by type chip', async () => {
    setup({
      catalog: [
        { name: 'http.requests', service_name: 'api',      type: 'counter', unit: 'req',  description: '', sample_count: 1 },
        { name: 'pool.in_use',   service_name: 'postgres', type: 'gauge',   unit: 'conn', description: '', sample_count: 1 },
      ],
    })
    renderMetrics()
    await waitFor(() => screen.getByText('http.requests'))

    // Click the "GAUGE" filter chip — must use exact-match since the tag also says GAUGE.
    const chips = screen.getAllByRole('button', { name: /gauge/i })
    // The filter chip is the one inside the type-filter row; both have the same text.
    // Click them all and assert at least one filters the list down.
    fireEvent.click(chips[0])
    await waitFor(() => {
      expect(screen.queryByText('http.requests')).toBeNull()
    })
    expect(screen.getByText('pool.in_use')).toBeTruthy()
  })

  it('renders chart + stats after selecting a metric', async () => {
    setup({
      catalog: [
        { name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req', description: 'request count', sample_count: 2 },
      ],
      series: {
        'name=http.requests&service=api&with_traces=1': {
          name: 'http.requests', service_name: 'api', type: 'counter', unit: 'req',
          description: 'request count',
          points: [
            { timestamp_ns: 1, value: 5 },
            { timestamp_ns: 2, value: 8 },
          ],
          traces: [],
        },
      },
    })

    renderMetrics()
    await waitFor(() => {
      // The page auto-selects the first metric on load, so the chart header
      // (h1 containing the metric name) should appear without a click.
      const headings = screen.getAllByText('http.requests')
      // One in the sidebar list, one in the right-pane h1.
      expect(headings.length).toBeGreaterThanOrEqual(2)
    })

    // Description from the loaded series.
    expect(screen.getByText('request count')).toBeTruthy()
    // Stat strip labels for counter type.
    expect(screen.getByText('rate / min')).toBeTruthy()
    expect(screen.getByText('total')).toBeTruthy()
    expect(screen.getByText('peak')).toBeTruthy()
    // Chart SVG exists.
    expect(document.querySelector('svg')).toBeTruthy()
  })

  it('buckets histogram points by percentile attribute', () => {
    // This test verifies that histograms with percentile attributes are bucketed correctly,
    // catching the bug where histograms without percentile data show as flat y=0.
    const histogramPoints = [
      { timestamp_ns: 1000, value: 10, percentile: 'p50' as const },
      { timestamp_ns: 1000, value: 50, percentile: 'p95' as const },
      { timestamp_ns: 1000, value: 100, percentile: 'p99' as const },
      { timestamp_ns: 2000, value: 20, percentile: 'p50' as const },
      { timestamp_ns: 2000, value: 60, percentile: 'p95' as const },
      { timestamp_ns: 2000, value: 120, percentile: 'p99' as const },
    ]

    const result = bucketPoints(histogramPoints, 'histogram', 60)

    // All three percentiles should be present
    expect(result.p50).toBeDefined()
    expect(result.p95).toBeDefined()
    expect(result.p99).toBeDefined()

    // Data should not be flat zeros (this is the bug being tested)
    const allP50 = result.p50!.every(v => v === 0)
    const allP95 = result.p95!.every(v => v === 0)
    const allP99 = result.p99!.every(v => v === 0)
    expect(allP50 || allP95 || allP99).toBeFalsy()

    // Should have actual values, not just zeros
    expect(result.p50!.some(v => v > 0)).toBeTruthy()
    expect(result.p95!.some(v => v > 0)).toBeTruthy()
    expect(result.p99!.some(v => v > 0)).toBeTruthy()
  })
})
