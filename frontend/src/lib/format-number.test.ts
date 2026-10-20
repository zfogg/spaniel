import { describe, expect, it } from 'vitest'
import { formatNumber } from './format-number'

describe('formatNumber', () => {
  it.each([
    [0, '0'],
    [500, '500'],
    [999, '999'],
    [1000, '1k'],
    [65_400, '65.4k'],
    [128_200_000, '128.2m'],
    [1_700_000_000_000, '1.7t'],
    [1_000_000_000, '1b'],
    [999_960, '1m'],
    [-65_400, '-65.4k'],
    [-0, '0'],
    [12.34, '12.3'],
    [NaN, '—'],
    [Infinity, '—'],
  ])('formats %s as %s', (value, expected) => {
    expect(formatNumber(value)).toBe(expected)
  })
})
