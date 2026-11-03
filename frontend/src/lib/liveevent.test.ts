import { describe, expect, it } from 'vitest'
import { changedResources, decodeEvent } from '../generated/liveevent'

describe('shared durable live contract', () => {
  it('validates known payload fields and tolerates compatible extra fields', () => {
    expect(decodeEvent({ type: 'future', timestamp_ns: 1, payload: {} })).toBeNull()
    expect(decodeEvent({ type: 'span', timestamp_ns: 1, payload: { traceId: 4 } })).toBeNull()
    expect(decodeEvent({ type: 'span', timestamp_ns: 1, payload: null })).toBeNull()
    expect(
      decodeEvent({ type: 'span', timestamp_ns: 1, payload: { traceId: 't', future: 4 } })?.type,
    ).toBe('span')
    expect(decodeEvent({ type: 'heartbeat', timestamp_ns: 1 })?.type).toBe('heartbeat')
  })
  it('shares removal, definition, and self telemetry refresh rules', () => {
    const removed = decodeEvent({
      type: 'resources_changed',
      timestamp_ns: 1,
      payload: { resources: ['span', 'dashboard-panel'], reason: 'retention' },
    })!
    expect(changedResources(removed)).toEqual([
      'span',
      'trace-span',
      'dashboard-panel',
      'dashboard-annotations',
      'dashboard-runtime',
    ])
    const definition = decodeEvent({
      type: 'definition_sync',
      timestamp_ns: 1,
      payload: { kind: 'dashboard', status: 'updating' },
    })!
    expect(changedResources(definition)).toEqual([])
    const self = decodeEvent({ type: 'self_span', timestamp_ns: 1, payload: {} })!
    expect(changedResources(self)).toEqual([])
    const metric = decodeEvent({ type: 'metric', timestamp_ns: 1, payload: { catalogOnly: true } })!
    expect(changedResources(metric)).toEqual([])
  })
})
