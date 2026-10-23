// @vitest-environment jsdom
import { afterEach, expect, it } from 'vitest'
import {
  customServiceColor,
  normalizeServiceColor,
  serviceColor,
  serviceSwatch,
  setServiceColor,
} from './service-color'
import { svcHex } from './tokens'

afterEach(() => localStorage.clear())

it('normalizes supported color inputs and rejects invalid colors', () => {
  expect(normalizeServiceColor('#AbC')).toBe('#aabbcc')
  expect(normalizeServiceColor('rgb(12, 34, 56)')).toBe('#0c2238')
  for (const value of ['red', '#abcd', 'rgb(256, 0, 0)', 'rgb(-1, 0, 0)', 'url(example)'])
    expect(normalizeServiceColor(value)).toBeNull()
})

it('shares overrides with badges, swatches, and waterfall colors and restores defaults', () => {
  const original = serviceSwatch('api')
  setServiceColor('api', '#ff0000')
  expect(customServiceColor('api')).toBe('#ff0000')
  expect(serviceSwatch('api')).toBe('#ff0000')
  expect(svcHex('api')).toBe('#ff0000')
  expect(serviceColor('api')).toEqual({ hue: 0, saturation: 100, lightness: 50 })
  setServiceColor('api', null)
  expect(serviceSwatch('api')).toBe(original)
})

it('ignores malformed stored preferences and supports arbitrary service names', () => {
  localStorage.setItem('spaniel.service-colors', 'invalid')
  expect(customServiceColor('api')).toBeNull()
  setServiceColor('__proto__', '#123456')
  expect(customServiceColor('__proto__')).toBe('#123456')
  expect(() => setServiceColor('api', 'rgb(900, 0, 0)')).toThrow()
})
