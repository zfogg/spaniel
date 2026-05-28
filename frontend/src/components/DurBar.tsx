interface DurBarProps {
  durNs: number
  maxNs: number
  hot?: boolean
}

export function DurBar({ durNs, maxNs, hot = false }: DurBarProps) {
  const pct = maxNs > 0 ? Math.min((durNs / maxNs) * 100, 100) : 0
  return (
    <div className="relative h-2 w-full rounded-full bg-muted">
      <div
        className={`absolute inset-y-0 left-0 rounded-full opacity-80 ${hot ? 'bg-danger' : 'bg-accent'}`}
        style={{ width: `${pct}%` }}
      />
    </div>
  )
}
