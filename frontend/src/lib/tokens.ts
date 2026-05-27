// Spaniel design system — Drift direction (sky · sand · slate)
//
// Single source of truth for all design tokens.  CSS vars are the runtime
// layer; these TS constants are for code that needs a literal value
// (canvas drawing, SVG fills, charting palettes).
//
// Light values = Drift; dark values = complementary deep-navy.

export const LIGHT = {
  // page chrome
  bg:       '#e4eaf0',
  surface:  '#f5f9fc',
  surface2: '#eaeff5',
  surface3: '#dfe6ed',

  // text
  ink:  '#1f2937',
  ink2: '#54616e',
  ink3: '#8693a1',

  // borders
  line:  '#cdd6df',
  line2: '#dde4ec',

  // accent — sky blue
  accent:    '#7aa3c4',
  accent2:   '#a8c4dc',
  accentInk: '#284a6a',
  accentBg:  '#c4d8e8',

  // states
  warn:       '#d6b46a',
  warnInk:    '#6a4f1e',
  warnBg:     '#f2e4c4',
  danger:     '#d68a7a',
  dangerInk:  '#7a3a2a',
  dangerBg:   '#f2cfc8',
  ok:         '#88b29a',
  okInk:      '#2a5a3a',
  okBg:       '#c8e0d2',

  // chips
  chipBg:  '#dde6ee',
  chipInk: '#3d4a58',

  // service swatch palette
  svc: {
    'api-gateway':       { fg: '#284a6a', bg: '#c4d8e8' },
    'cart-service':      { fg: '#6a4f1e', bg: '#ecdcb4' },
    'pricing-service':   { fg: '#7a3a2a', bg: '#ecc8bc' },
    'inventory-service': { fg: '#3d5538', bg: '#cfe0c2' },
    'payment-service':   { fg: '#4b3a6e', bg: '#d6c8e6' },
    'redis':             { fg: '#7a3a2a', bg: '#ecc6ba' },
    'postgres':          { fg: '#284a6a', bg: '#c4d8e8' },
    'stripe':            { fg: '#4b3a6e', bg: '#d6c8e6' },
  },

  shadow: '0 1px 0 #d6dee6, 0 12px 24px -16px rgba(30,50,80,.18)',
} as const

export const DARK = {
  // page chrome — deep navy, complements sky accent
  bg:       '#0c1622',
  surface:  '#111f2e',
  surface2: '#172435',
  surface3: '#1c2b3e',

  // text — blue-white
  ink:  '#dceaf6',
  ink2: '#7a93aa',
  ink3: '#445a6e',

  // borders
  line:  '#1e3248',
  line2: '#192840',

  // accent — same sky blue works on dark
  accent:    '#7aa3c4',
  accent2:   '#5588a8',
  accentInk: '#c4dcf0',
  accentBg:  '#1a3048',

  // states — slightly shifted for dark contrast
  warn:       '#c8a050',
  warnInk:    '#f0d898',
  warnBg:     '#2c2010',
  danger:     '#c87060',
  dangerInk:  '#f0b8aa',
  dangerBg:   '#2c1410',
  ok:         '#6a9880',
  okInk:      '#b8e0c8',
  okBg:       '#102418',

  // chips
  chipBg:  '#1a2d3e',
  chipInk: '#90b0c8',

  // service swatch palette — desaturated, dark-bg-appropriate
  svc: {
    'api-gateway':       { fg: '#c4dcf0', bg: '#1a3048' },
    'cart-service':      { fg: '#f0d898', bg: '#2c2010' },
    'pricing-service':   { fg: '#f0b8aa', bg: '#2c1a16' },
    'inventory-service': { fg: '#b8e0c8', bg: '#102818' },
    'payment-service':   { fg: '#d0c4f0', bg: '#20183a' },
    'redis':             { fg: '#f0b8aa', bg: '#2c1a16' },
    'postgres':          { fg: '#c4dcf0', bg: '#1a3048' },
    'stripe':            { fg: '#d0c4f0', bg: '#20183a' },
  },

  shadow: '0 1px 0 #0a1828, 0 12px 24px -16px rgba(0,10,24,.5)',
} as const

// Span timeline palette — 12 perceptually distinct, Drift-aligned.
// Used as the canonical service palette: services are assigned a slot by hash
// of their name, so the same service gets the same color across sessions.
export const SPAN_PALETTE = [
  '#7aa3c4', // sky  (accent)
  '#88b29a', // sage (ok)
  '#d6b46a', // amber
  '#a08cc8', // lavender
  '#6a98b8', // slate-blue
  '#8ab8a0', // mint
  '#c8a870', // sand
  '#7898c0', // periwinkle
  '#d69882', // coral
  '#98a8c0', // steel
  '#b89ad0', // orchid
  '#70a8a8', // teal
] as const

// Pair each palette hex with fg/bg ink+tint pairs for chips.
// Light: tinted bg + darker ink. Dark: deep bg + lifted ink.
const PALETTE_CHIPS_LIGHT: ReadonlyArray<{ fg: string; bg: string }> = [
  { fg: '#284a6a', bg: '#c4d8e8' },
  { fg: '#2a5a3a', bg: '#c8e0d2' },
  { fg: '#6a4f1e', bg: '#ecdcb4' },
  { fg: '#4b3a6e', bg: '#d6c8e6' },
  { fg: '#1f3e5a', bg: '#bccfe0' },
  { fg: '#2f5a48', bg: '#c8e0d2' },
  { fg: '#6a4f1e', bg: '#ecdcb4' },
  { fg: '#2a4a78', bg: '#c8d8ec' },
  { fg: '#7a3a2a', bg: '#ecc8bc' },
  { fg: '#3d4a58', bg: '#dde6ee' },
  { fg: '#4b3a6e', bg: '#e0d4ec' },
  { fg: '#1e4848', bg: '#bcd8d8' },
]
const PALETTE_CHIPS_DARK: ReadonlyArray<{ fg: string; bg: string }> = [
  { fg: '#c4dcf0', bg: '#1a3048' },
  { fg: '#b8e0c8', bg: '#102818' },
  { fg: '#f0d898', bg: '#2c2010' },
  { fg: '#d0c4f0', bg: '#20183a' },
  { fg: '#b8d4ec', bg: '#152a40' },
  { fg: '#b8e0d0', bg: '#102820' },
  { fg: '#f0d898', bg: '#2c2010' },
  { fg: '#c4d8ec', bg: '#15263c' },
  { fg: '#f0b8aa', bg: '#2c1a16' },
  { fg: '#b8c4d4', bg: '#1a2330' },
  { fg: '#d8c4ec', bg: '#241a3a' },
  { fg: '#a8d4d4', bg: '#0e2828' },
]

// Stable string hash → palette index.
function hashStr(s: string): number {
  let h = 2166136261
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 16777619)
  }
  return h >>> 0
}

export function svcPaletteIndex(svcName: string): number {
  return hashStr(svcName) % SPAN_PALETTE.length
}

export function svcHex(svcName: string): string {
  return SPAN_PALETTE[svcPaletteIndex(svcName)]
}

// Helper — get service swatch (light or dark). Falls back to hash-based
// palette slot for unknown services so colors stay stable per name.
export function svcColor(svcName: string, dark: boolean) {
  const map = dark ? DARK.svc : LIGHT.svc
  const hard = (map as Record<string, { fg: string; bg: string }>)[svcName]
  if (hard) return hard
  const chips = dark ? PALETTE_CHIPS_DARK : PALETTE_CHIPS_LIGHT
  return chips[svcPaletteIndex(svcName)]
}
