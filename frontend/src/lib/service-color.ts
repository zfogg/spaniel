// Mix the entire name so shared prefixes do not determine the service color.
// Fractional hues and independent saturation/lightness provide more distinct
// swatches than one of 360 integer hues, without changing colors on arrival.
export function serviceColor(name: string) {
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
  const { hue, saturation, lightness } = serviceColor(name)
  return `hsl(${hue} ${saturation}% ${lightness}%)`
}
