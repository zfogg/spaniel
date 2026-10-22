// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { telemetryArrivalMotion } from '@/lib/telemetry-arrival-motion'
import { TelemetryArrivalList } from './TelemetryArrival'

describe('telemetryArrivalMotion', () => {
  it('moves a burst farther and at a higher velocity than one arriving row', () => {
    const single = telemetryArrivalMotion(0, 1)
    const burst = telemetryArrivalMotion(3, 4)

    expect(burst.entryDistance).toBeGreaterThan(single.entryDistance)
    expect(burst.duration).toBeGreaterThan(single.duration)
    expect(burst.velocity).toBeGreaterThan(single.velocity)
  })
})

describe('TelemetryArrivalList', () => {
  it('marks only records inserted after the first result as arrivals', () => {
    const { rerender } = render(
      <TelemetryArrivalList items={['older']} itemKey={(id) => id}>
        {(id) => <div>{id}</div>}
      </TelemetryArrivalList>,
    )

    rerender(
      <TelemetryArrivalList items={['newer', 'older']} itemKey={(id) => id}>
        {(id) => <div>{id}</div>}
      </TelemetryArrivalList>,
    )

    expect(screen.getByText('newer').closest('[data-motion-arrival]')).not.toBeNull()
    expect(screen.getByText('older').closest('[data-motion-arrival]')).toBeNull()
  })

  it('does not replay a known record when a local filter makes it visible', () => {
    const { rerender } = render(
      <TelemetryArrivalList
        items={['older']}
        trackedItems={['older', 'hidden']}
        itemKey={(id) => id}
      >
        {(id) => <div>{id}</div>}
      </TelemetryArrivalList>,
    )

    rerender(
      <TelemetryArrivalList
        items={['hidden', 'older']}
        trackedItems={['hidden', 'older']}
        itemKey={(id) => id}
      >
        {(id) => <div>{id}</div>}
      </TelemetryArrivalList>,
    )

    expect(screen.getByText('hidden').closest('[data-motion-arrival]')).toBeNull()
  })
})
