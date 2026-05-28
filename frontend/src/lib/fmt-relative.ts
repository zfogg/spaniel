export function fmtRelative(ns: number): string {
  const diffMs = Date.now() - ns / 1_000_000
  const diffS = diffMs / 1_000
  if (diffS < 1) return 'just now'
  if (diffS < 60) return `${Math.floor(diffS)}s ago`
  const diffM = diffS / 60
  if (diffM < 60) return `${Math.floor(diffM)}m ago`
  const diffH = diffM / 60
  return `${Math.floor(diffH)}h ago`
}
