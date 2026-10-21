import { expect, it } from 'vitest'
import { serviceHue } from './service-color'

it('keeps the live Ratio services visually separated around the hue wheel', () => {
  const names = [
    'ratio-api',
    'ratio-maker',
    'ratio-worker',
    'ratio-web',
    'ratio-observer',
    'ratio-qualification',
  ]
  const hues = names.map(serviceHue).sort((a, b) => a - b)
  const gaps = hues.map((hue, index) => (hues[(index + 1) % hues.length] - hue + 360) % 360)
  // A repeat of the original green cluster must fail even if hashes differ.
  expect(Math.min(...gaps)).toBeGreaterThanOrEqual(40)
})

it('preserves a service color across render order and new services arriving', () => {
  const before = serviceHue('ratio-api')
  serviceHue('new-service')
  serviceHue('ratio-worker')
  expect(serviceHue('ratio-api')).toBe(before)
})
