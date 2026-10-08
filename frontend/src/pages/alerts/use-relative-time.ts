import { useSyncExternalStore } from 'react'

let relativeTimeNow = Date.now()

const relativeTimeListeners = new Set<() => void>()

let relativeTimeInterval: ReturnType<typeof setInterval> | undefined

const subscribeToRelativeTime = (listener: () => void) => {
  relativeTimeListeners.add(listener)
  if (!relativeTimeInterval) {
    relativeTimeInterval = setInterval(() => {
      relativeTimeNow = Date.now()
      for (const notify of relativeTimeListeners) notify()
    }, 1_000)
  }
  return () => {
    relativeTimeListeners.delete(listener)
    if (!relativeTimeListeners.size && relativeTimeInterval) {
      clearInterval(relativeTimeInterval)
      relativeTimeInterval = undefined
    }
  }
}

export const useRelativeTimeNow = () =>
  useSyncExternalStore(
    subscribeToRelativeTime,
    () => relativeTimeNow,
    () => relativeTimeNow,
  )
