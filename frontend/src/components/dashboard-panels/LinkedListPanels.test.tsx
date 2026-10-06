// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import type { ReactNode } from 'react'
import { LogListPanel } from './LogListPanel'
import { SpanListPanel } from './SpanListPanel'
import { TablePanel } from './TablePanel'
import { TraceListPanel } from './TraceListPanel'

const panel = {} as never

afterEach(cleanup)

function renderPanel(element: ReactNode) {
  return render(<MemoryRouter>{element}</MemoryRouter>)
}

describe('linked dashboard list panels', () => {
  it('opens a trace in its trace detail view', () => {
    renderPanel(<TraceListPanel panel={panel} columns={[]} rows={[{ trace_id: 'trace-1', name: 'checkout', service_name: 'store', duration_ns: 42 }]} />)
    expect(screen.getByRole('link', { name: /checkout/i }).getAttribute('href')).toBe('/traces/trace-1')
  })

  it('filters logs to the selected trace when a trace id is available', () => {
    renderPanel(<LogListPanel panel={panel} columns={[]} rows={[{ trace_id: 'trace-1', body: 'connection reset', service_name: 'store', severity: 17 }]} />)
    expect(screen.getByRole('link', { name: /connection reset/i }).getAttribute('href')).toBe('/logs?traceId=trace-1')
  })

  it('uses OTLP severity labels instead of raw protocol values', () => {
    renderPanel(<LogListPanel panel={panel} columns={[]} rows={[{ body: 'request complete', service_name: 'store', severity: 9 }]} />)
    expect(screen.getByText('INFO')).toBeTruthy()
  })

  it('opens a span through its containing trace', () => {
    renderPanel(<SpanListPanel panel={panel} columns={[]} rows={[{ trace_id: 'trace-1', span_id: 'span-1', name: 'db.query', service_name: 'store', duration_ns: 42 }]} />)
    expect(screen.getByRole('link', { name: /db\.query/i }).getAttribute('href')).toBe('/traces/trace-1')
  })

  it('links trace IDs and gives dashboard-friendly labels to table columns', () => {
    renderPanel(<TablePanel panel={panel} columns={['trace_id', 'duration_sec', 'http_status']} rows={[{ trace_id: 'trace-1', duration_sec: 1.2345, http_status: 200 }]} />)
    expect(screen.getByRole('link', { name: 'trace-1' }).getAttribute('href')).toBe('/traces/trace-1')
    expect(screen.getByText('Duration Sec')).toBeTruthy()
    expect(screen.getByText('HTTP Status')).toBeTruthy()
    expect(screen.getByText('1.235')).toBeTruthy()
  })
})
