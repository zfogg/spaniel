import { describe, expect, it } from 'vitest'
import type { Log } from '@/lib/api'
import { logKey } from '@/lib/log-key'
import { matchesSevFilter, sevLabel } from './log-severity-filter'

const baseLog: Log = {
  attributes: '{"http.route":"/api/logs"}',
  body: 'HTTP request completed',
  received_at: 1_791_548_867_665_099_000,
  service_name: 'spaniel',
  session_id: 'session',
  severity: 9,
  span_id: '',
  timestamp_ns: 1_791_548_867_665_099_000,
  trace_id: '',
}

describe('sevLabel', () => {
  it.each([
    [1, 'TRACE'],
    [4, 'TRACE'],
    [5, 'DEBUG'],
    [8, 'DEBUG'],
    [9, 'INFO'],
    [12, 'INFO'],
    [13, 'WARN'],
    [16, 'WARN'],
    [17, 'ERROR'],
    [20, 'ERROR'],
    [21, 'FATAL'],
    [24, 'FATAL'],
  ])('severity %d → %s', (n, label) => {
    expect(sevLabel(n)).toBe(label)
  })
})

describe('matchesSevFilter', () => {
  it('ALL matches every level', () => {
    for (const n of [1, 5, 9, 13, 17, 21]) expect(matchesSevFilter(n, 'ALL')).toBe(true)
  })

  it('each named filter only matches its band', () => {
    expect(matchesSevFilter(3, 'TRACE')).toBe(true)
    expect(matchesSevFilter(6, 'TRACE')).toBe(false)
    expect(matchesSevFilter(7, 'DEBUG')).toBe(true)
    expect(matchesSevFilter(9, 'DEBUG')).toBe(false)
    expect(matchesSevFilter(11, 'INFO')).toBe(true)
    expect(matchesSevFilter(13, 'INFO')).toBe(false)
    expect(matchesSevFilter(15, 'WARN')).toBe(true)
    expect(matchesSevFilter(17, 'WARN')).toBe(false)
    expect(matchesSevFilter(19, 'ERROR')).toBe(true)
    expect(matchesSevFilter(21, 'ERROR')).toBe(false)
    expect(matchesSevFilter(22, 'FATAL')).toBe(true)
    expect(matchesSevFilter(20, 'FATAL')).toBe(false)
  })
})

describe('logKey', () => {
  it('keeps same-timestamp access logs with different attributes distinct', () => {
    expect(logKey(baseLog)).not.toBe(
      logKey({ ...baseLog, attributes: '{"http.route":"/api/sources"}' }),
    )
  })
})
