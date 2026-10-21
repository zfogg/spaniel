import type { CSSProperties } from 'react'
import { logSeverityLabel } from '@/lib/log-severity'

function labelFor(severity: unknown): string {
  if (
    typeof severity === 'string' &&
    /^(TRACE|DEBUG|INFO|WARN|WARNING|ERROR|FATAL)$/i.test(severity.trim())
  ) {
    return severity.trim().toUpperCase().replace('WARNING', 'WARN')
  }
  return severity == null || severity === '' ? 'UNKNOWN' : logSeverityLabel(severity)
}

const tones: Record<string, CSSProperties> = {
  FATAL: { color: '#e9d5ff', background: '#6b21a8', border: '1px solid #a855f7' },
  ERROR: { color: '#fecaca', background: '#7f1d1d', border: '1px solid #ef4444' },
  WARN: { color: '#fef08a', background: '#713f12', border: '1px solid #eab308' },
  INFO: { color: '#18181b', background: '#fff', border: '1px solid #d4d4d8' },
  DEBUG: { color: '#bfdbfe', background: '#1e3a8a', border: '1px solid #3b82f6' },
}

export default function LogLevelBadge({ severity }: { severity: unknown }) {
  const label = labelFor(severity)
  return (
    <span
      style={{
        borderRadius: 3,
        padding: '1px 5px',
        fontSize: 9,
        fontWeight: 700,
        letterSpacing: '0.06em',
        textTransform: 'uppercase',
        fontFamily: 'var(--font-mono)',
        flexShrink: 0,
        display: 'inline-block',
        ...(tones[label] ?? { color: 'var(--ink3)', background: 'transparent', opacity: 0.7 }),
      }}
    >
      {label}
    </span>
  )
}
