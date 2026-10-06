/** Human-readable OTLP severity bands. The protocol's numeric values are not useful in the UI. */
export function logSeverityLabel(value: unknown): string {
  const severity = Number(value)
  if (!Number.isFinite(severity)) return 'UNKNOWN'
  if (severity >= 21) return 'FATAL'
  if (severity >= 17) return 'ERROR'
  if (severity >= 13) return 'WARN'
  if (severity >= 9) return 'INFO'
  if (severity >= 5) return 'DEBUG'
  return 'TRACE'
}
