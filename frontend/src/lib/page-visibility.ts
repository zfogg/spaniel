import { useSyncExternalStore } from 'react'
import { useQuery, type QueryKey, type UseQueryOptions } from '@tanstack/react-query'

export function isPageVisible() {
  return typeof document === 'undefined' || document.visibilityState !== 'hidden'
}

function subscribe(onChange: () => void) {
  document.addEventListener('visibilitychange', onChange)
  return () => document.removeEventListener('visibilitychange', onChange)
}

export function usePageVisible() {
  return useSyncExternalStore(subscribe, isPageVisible, () => true)
}

// Mounted observers in background browser tabs still count as "active" to
// React Query. Disable expensive view reads explicitly, including their first
// load and timers; the app invalidator marks them stale once on return.
export function useVisibleQuery<
  TQueryFnData,
  TError = Error,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
>(options: UseQueryOptions<TQueryFnData, TError, TData, TQueryKey>) {
  const visible = usePageVisible()
  return useQuery({ ...options, enabled: visible ? options.enabled : false })
}
