// Mix the entire name so shared prefixes do not determine the service color.
// Fractional hues and independent saturation/lightness provide more distinct
// swatches than one of 360 integer hues, without changing colors on arrival.
export function serviceColor(name: string) {
  const custom = customServiceColor(name)
  if (custom) {
    const [r, g, b] = [1, 3, 5].map((i) => parseInt(custom.slice(i, i + 2), 16) / 255)
    const max = Math.max(r, g, b),
      min = Math.min(r, g, b),
      delta = max - min
    const lightness = (max + min) / 2
    const hue = !delta
      ? 0
      : max === r
        ? ((g - b) / delta + 6) % 6
        : max === g
          ? (b - r) / delta + 2
          : (r - g) / delta + 4
    return {
      hue: hue * 60,
      saturation: delta ? (delta / (1 - Math.abs(2 * lightness - 1))) * 100 : 0,
      lightness: lightness * 100,
    }
  }
  let hash = 2166136261 ^ 3365
  for (let i = 0; i < name.length; i++) {
    hash = Math.imul(hash ^ name.charCodeAt(i), 16777619)
  }
  hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b)
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35)
  hash = (hash ^ (hash >>> 16)) >>> 0
  return {
    hue: (hash % 360) + ((hash >>> 16) % 100) / 100,
    saturation: 60 + ((hash >>> 9) % 26),
    lightness: 48 + ((hash >>> 18) % 13),
  }
}

export function serviceHue(name: string): number {
  return serviceColor(name).hue
}

export function serviceSwatch(name: string): string {
  const custom = customServiceColor(name)
  if (custom) return custom
  const { hue, saturation, lightness } = serviceColor(name)
  return `hsl(${hue} ${saturation}% ${lightness}%)`
}

// Hex adapters keep canvas/SVG consumers and callers appending alpha in sync
// with the canonical HSL service color, including browser-local overrides.
function hslHex(hue: number, saturation: number, lightness: number): string {
  const s = saturation / 100
  const l = lightness / 100
  const c = (1 - Math.abs(2 * l - 1)) * s
  const x = c * (1 - Math.abs(((hue / 60) % 2) - 1))
  const m = l - c / 2
  const [r, g, b] =
    hue < 60
      ? [c, x, 0]
      : hue < 120
        ? [x, c, 0]
        : hue < 180
          ? [0, c, x]
          : hue < 240
            ? [0, x, c]
            : hue < 300
              ? [x, 0, c]
              : [c, 0, x]
  return (
    '#' +
    [r, g, b]
      .map((value) =>
        Math.round((value + m) * 255)
          .toString(16)
          .padStart(2, '0'),
      )
      .join('')
  )
}

export function serviceHex(name: string): string {
  const custom = customServiceColor(name)
  if (custom) return custom
  const { hue, saturation, lightness } = serviceColor(name)
  return hslHex(hue, saturation, lightness)
}

export function serviceTextHex(name: string, dark: boolean): string {
  const { hue, saturation } = serviceColor(name)
  return hslHex(hue, saturation, dark ? 78 : 30)
}

const STORAGE_KEY = 'spaniel.service-colors'
export const SERVICE_COLORS_EVENT = 'spaniel:service-colors'

export function normalizeServiceColor(value: string): string | null {
  const hex = value.trim().match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)
  if (hex)
    return (
      '#' + (hex[1].length === 3 ? [...hex[1]].map((c) => c + c).join('') : hex[1]).toLowerCase()
    )
  const rgb = value.trim().match(/^rgb\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)$/i)
  if (!rgb || rgb.slice(1).some((v) => Number(v) > 255)) return null
  return (
    '#' +
    rgb
      .slice(1)
      .map((v) => Number(v).toString(16).padStart(2, '0'))
      .join('')
  )
}

function readColors(): Record<string, string> {
  try {
    const value = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}')
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {}
  } catch {
    return {}
  }
}

export function customServiceColor(name: string): string | null {
  const value = readColors()[name]
  return typeof value === 'string' ? normalizeServiceColor(value) : null
}

export function setServiceColor(name: string, value: string | null) {
  const colors = readColors()
  if (value === null) delete colors[name]
  else {
    const normalized = normalizeServiceColor(value)
    if (!normalized) throw new Error('Enter a hexadecimal color or rgb(0, 0, 0).')
    Object.defineProperty(colors, name, { value: normalized, enumerable: true, configurable: true })
  }
  localStorage.setItem(STORAGE_KEY, JSON.stringify(colors))
  window.dispatchEvent(new Event(SERVICE_COLORS_EVENT))
}
