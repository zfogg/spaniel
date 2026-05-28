// fmtRelMs formats an event timestamp as the offset from the span's start.
// Output matches the mockup: "+0.4ms" for sub-ms, "+6.1ms" for ms, etc.
// Negative deltas (events stamped before the span start — unusual but seen
// in batched exporters) keep their leading minus sign so they look obviously
// wrong rather than getting silently rounded.
//
//   <1 µs        → ns   (e.g. "+500ns")
//   <100 µs      → µs   (e.g. "+2.5µs", "+50.0µs")
//   otherwise    → ms   (e.g. "+0.4ms", "+6.1ms", "+1234.5ms")
export function fmtRelMs(eventNs: number, spanStartNs: number): string {
  const deltaNs = eventNs - spanStartNs
  if (deltaNs === 0) return '+0ms'
  const sign = deltaNs >= 0 ? '+' : ''
  const abs = Math.abs(deltaNs)
  if (abs < 1_000)   return `${sign}${deltaNs}ns`
  if (abs < 100_000) return `${sign}${(deltaNs / 1_000).toFixed(1)}µs`
  return `${sign}${(deltaNs / 1_000_000).toFixed(1)}ms`
}
