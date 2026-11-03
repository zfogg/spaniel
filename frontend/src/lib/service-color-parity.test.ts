import { expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
const fixture: {
  cases: {
    name: string
    hue: number
    saturation: number
    lightness: number
    swatch: number[]
    lightText: number[]
    darkText: number[]
  }[]
} = JSON.parse(
  readFileSync(new URL('../../../testdata/service-colors.json', import.meta.url), 'utf8'),
)
import { serviceColor, serviceHex, serviceTextHex } from './service-color'
import { svcColor, svcHex } from './tokens'
import { svcColor as spanColor } from './span-utils'

const hex = (rgb: number[]) =>
  '#' + rgb.map((value) => value.toString(16).padStart(2, '0')).join('')

it('matches shared TypeScript/Chromium vectors and every default web adapter', () => {
  for (const v of fixture.cases) {
    expect(serviceColor(v.name)).toEqual({
      hue: v.hue,
      saturation: v.saturation,
      lightness: v.lightness,
    })
    expect(serviceHex(v.name)).toBe(hex(v.swatch))
    expect(svcHex(v.name)).toBe(hex(v.swatch))
    expect(spanColor(v.name)).toEqual({ fg: hex(v.swatch), bg: hex(v.swatch) + '28' })
    expect(serviceTextHex(v.name, false)).toBe(hex(v.lightText))
    expect(serviceTextHex(v.name, true)).toBe(hex(v.darkText))
    expect(svcColor(v.name, false)).toEqual({ fg: hex(v.lightText), bg: hex(v.swatch) + '28' })
    expect(svcColor(v.name, true)).toEqual({ fg: hex(v.darkText), bg: hex(v.swatch) + '28' })
  }
})
