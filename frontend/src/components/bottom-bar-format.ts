export function fmtRate(n: number): string {
  if (n <= 0) return '0 / s'
  if (n < 1) return '< 1 / s'
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k / s`
  return `${Math.round(n)} / s`
}
