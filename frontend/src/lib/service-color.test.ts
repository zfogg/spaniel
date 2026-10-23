import { expect, it } from 'vitest'
import { serviceColor, serviceHue, serviceSwatch } from './service-color'

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

it('separates Spaniel and all ascii-chat modes, including mirror', () => {
  const names = [
    'spaniel',
    'ascii-chat-client',
    'ascii-chat-server',
    'ascii-chat-mirror',
    'ascii-chat-discovery',
    'ascii-chat-discovery-service',
  ]
  const hues = names.map(serviceHue).sort((a, b) => a - b)
  const gaps = hues.map((hue, index) => (hues[(index + 1) % hues.length] - hue + 360) % 360)
  expect(Math.min(...gaps)).toBeGreaterThanOrEqual(30)
})

it('offers more than 360 hues and distinct swatches for a large service family', () => {
  const names = Array.from({ length: 1000 }, (_, index) => `service-worker-${index}`)
  expect(new Set(names.map(serviceHue)).size).toBeGreaterThan(950)
  expect(new Set(names.map(serviceSwatch)).size).toBe(1000)
  for (const name of ['', ...names]) {
    const color = serviceColor(name)
    expect(color.hue).toBeGreaterThanOrEqual(0)
    expect(color.hue).toBeLessThan(360)
    expect(color.saturation).toBeGreaterThanOrEqual(60)
    expect(color.saturation).toBeLessThanOrEqual(85)
    expect(color.lightness).toBeGreaterThanOrEqual(48)
    expect(color.lightness).toBeLessThanOrEqual(60)
  }
})
