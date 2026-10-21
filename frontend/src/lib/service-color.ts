// FNV-1a followed by a 32-bit avalanche avoids clustering names with shared
// prefixes/suffixes. A continuous hue space avoids a tiny repeating palette.
export function serviceHue(name: string): number {
  // Fixed seed qualified against common prefixed service names, so the live
  // api/maker/worker/web names do not bunch into the same hue family.
  let hash = 2166136261 ^ 68
  for (let i = 0; i < name.length; i++) {
    hash = Math.imul(hash ^ name.charCodeAt(i), 16777619)
  }
  hash = Math.imul(hash ^ (hash >>> 16), 0x85ebca6b)
  hash = Math.imul(hash ^ (hash >>> 13), 0xc2b2ae35)
  hash = (hash ^ (hash >>> 16)) >>> 0
  return hash % 360
}

export function serviceSwatch(name: string): string {
  return `hsl(${serviceHue(name)} 75% 55%)`
}
