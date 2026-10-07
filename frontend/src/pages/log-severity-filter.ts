import { logSeverityLabel } from '@/lib/log-severity'

export type SevFilter = 'ALL' | 'TRACE' | 'DEBUG' | 'INFO' | 'WARN' | 'ERROR' | 'FATAL'

export function sevLabel(n: number): string {
  return logSeverityLabel(n)
}

export function matchesSevFilter(severity: number, filter: SevFilter): boolean {
  if (filter === 'ALL') return true
  if (filter === 'FATAL') return severity >= 21
  if (filter === 'ERROR') return severity >= 17 && severity < 21
  if (filter === 'WARN') return severity >= 13 && severity < 17
  if (filter === 'INFO') return severity >= 9 && severity < 13
  if (filter === 'DEBUG') return severity >= 5 && severity < 9
  return filter !== 'TRACE' || severity < 5
}
