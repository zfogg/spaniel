import { useMemo } from 'react'
import { formatTimestamp, formatAgo } from './time-format'
import { useRelativeTimeNow } from './use-relative-time'

export function TimestampWithAgo({
  nanoseconds,
  stacked = false,
}: {
  nanoseconds: number
  stacked?: boolean
}) {
  const now = useRelativeTimeNow()
  const timestamp = useMemo(() => formatTimestamp(nanoseconds), [nanoseconds])
  return (
    <span className={stacked ? 'inline-flex flex-col' : 'whitespace-nowrap'}>
      <span>{timestamp}</span>
      {!stacked && ' · '}
      <span>{formatAgo(nanoseconds, now)}</span>
    </span>
  )
}
