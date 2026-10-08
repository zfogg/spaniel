import { formatDistanceToNow } from 'date-fns'

export const formatTimestamp = (nanoseconds: number) => new Date(nanoseconds / 1e6).toLocaleString()

export const formatAgo = (nanoseconds: number, now = Date.now()) => {
  const milliseconds = nanoseconds / 1e6
  const difference = now - milliseconds
  if (Math.abs(difference) < 60_000) return difference >= 0 ? 'moments ago' : 'in moments'
  return formatDistanceToNow(milliseconds, { addSuffix: true })
}

export const formatTimestampWithAgo = (nanoseconds: number, now = Date.now()) =>
  `${formatTimestamp(nanoseconds)} · ${formatAgo(nanoseconds, now)}`
