import { useMemo } from 'react'
import { formatTimestamp, formatAgo } from './alert-model'
import { useRelativeTimeNow } from './use-relative-time'

export function TimestampWithAgo({ nanoseconds }: { nanoseconds: number }) {
  const now = useRelativeTimeNow()
  const timestamp = useMemo(() => formatTimestamp(nanoseconds), [nanoseconds])
  return (
    <span>
      {timestamp} · {formatAgo(nanoseconds, now)}
    </span>
  )
}
