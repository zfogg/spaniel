import { useEffect, useRef } from 'react'

/** Identifies records that appeared after the first successful result. */
export function useNewItemIDs<T>(items: readonly T[], key: (item: T) => string) {
  const previous = useRef<Set<string> | null>(null)
  const ids = items.map(key)
  const arriving = previous.current
    ? new Set(ids.filter((id) => !previous.current?.has(id)))
    : new Set<string>()

  useEffect(() => {
    previous.current = new Set(ids)
  }, [ids])

  return arriving
}
