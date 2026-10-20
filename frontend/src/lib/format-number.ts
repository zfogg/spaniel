/** Compact decimal counts, with one decimal and no redundant trailing zero. */
export function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return '—'
  const units = ['', 'k', 'm', 'b', 't']
  let scaled = Math.abs(value)
  let unit = 0
  while (scaled >= 1000 && unit < units.length - 1) {
    scaled /= 1000
    unit++
  }
  // Rounding can promote a value to the next unit (999.96k → 1m).
  if (Number(scaled.toFixed(1)) >= 1000 && unit < units.length - 1) {
    scaled /= 1000
    unit++
  }
  const rounded = Number(scaled.toFixed(1))
  return `${value < 0 && rounded !== 0 ? '-' : ''}${rounded}${units[unit]}`
}
