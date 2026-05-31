// Shared time/duration formatters. Page components used to each carry their
// own near-identical copies of these — keep them here so the rounding and
// granularity rules stay consistent everywhere.

const SECOND = 1_000
const MINUTE = 60_000
const HOUR = 3_600_000
const DAY = 86_400_000

/**
 * Relative "Xs/Xm/Xh/Xd ago" from an epoch time in nanoseconds. Floors to
 * whole units (59.9s stays "59s ago", never rounds up to "1m"). Pass `nowMs`
 * to drive live-ticking displays from a single clock source.
 */
export function fmtRelative(ns: number, nowMs: number = Date.now()): string {
  const diffMs = nowMs - ns / 1_000_000
  if (diffMs < SECOND) return 'just now'
  if (diffMs < MINUTE) return `${Math.floor(diffMs / SECOND)}s ago`
  if (diffMs < HOUR) return `${Math.floor(diffMs / MINUTE)}m ago`
  if (diffMs < DAY) return `${Math.floor(diffMs / HOUR)}h ago`
  return `${Math.floor(diffMs / DAY)}d ago`
}

/** Like {@link fmtRelative} but for an epoch time already in milliseconds. */
export function fmtRelativeMs(atMs: number, nowMs: number = Date.now()): string {
  return fmtRelative(atMs * 1_000_000, nowMs)
}

/** Signed millisecond delta from a nanosecond span, e.g. "+12ms" / "-3ms". */
export function fmtDeltaMs(ns: number): string {
  const ms = Math.round(ns / 1_000_000)
  return (ms > 0 ? '+' : '') + ms + 'ms'
}

/** Wall-clock "HH:MM:SS.mmm" from an epoch time in nanoseconds (24-hour). */
export function fmtClock(ns: number): string {
  const d = new Date(ns / 1_000_000)
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  const ss = String(d.getSeconds()).padStart(2, '0')
  const ms = String(d.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}

/** Locale date + time from an epoch time in nanoseconds (24-hour). */
export function fmtDateTime(ns: number): string {
  return new Date(ns / 1_000_000).toLocaleString(undefined, { hour12: false })
}

/** Human duration from a nanosecond count, e.g. "1.23s" / "4.5ms" / "12µs". */
export function fmtDuration(ns: number): string {
  if (ns >= 1_000_000_000) return (ns / 1_000_000_000).toFixed(2) + 's'
  if (ns >= 1_000_000) return (ns / 1_000_000).toFixed(1) + 'ms'
  if (ns >= 1_000) return (ns / 1_000).toFixed(0) + 'µs'
  return ns + 'ns'
}
