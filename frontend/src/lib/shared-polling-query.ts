import { useEffect, useRef } from 'react'
import {
  useQuery,
  useQueryClient,
  type QueryKey,
  type UseQueryOptions,
} from '@tanstack/react-query'

type Poller = {
  intervals: Set<number>
  timer: ReturnType<typeof setInterval> | null
}

const pollers = new Map<string, Poller>()

function pollerID(queryKey: QueryKey) {
  return JSON.stringify(queryKey)
}

function updateTimer(poller: Poller, refresh: () => void) {
  if (poller.timer) clearInterval(poller.timer)
  const interval = Math.min(...poller.intervals)
  poller.timer = setInterval(refresh, interval)
}

/**
 * A React Query query with one shared poller per query key. Multiple mounted
 * consumers keep the cached result fresh at the shortest requested interval,
 * rather than creating one timer and one request stream per consumer.
 */
export function useSharedPollingQuery<
  TQueryFnData,
  TError = Error,
  TData = TQueryFnData,
  TQueryKey extends QueryKey = QueryKey,
>(options: UseQueryOptions<TQueryFnData, TError, TData, TQueryKey> & { intervalMs: number }) {
  const { intervalMs, queryKey, ...queryOptions } = options
  const queryClient = useQueryClient()
  const query = useQuery({ ...queryOptions, queryKey, refetchInterval: false })
  const id = pollerID(queryKey)
  const queryKeyRef = useRef(queryKey)
  queryKeyRef.current = queryKey

  useEffect(() => {
    if (intervalMs <= 0) return
    let poller = pollers.get(id)
    if (!poller) {
      poller = { intervals: new Set(), timer: null }
      pollers.set(id, poller)
    }
    poller.intervals.add(intervalMs)
    const refresh = () => {
      void queryClient.refetchQueries(
        { queryKey: queryKeyRef.current, exact: true },
        { cancelRefetch: false },
      )
    }
    updateTimer(poller, refresh)

    return () => {
      const current = pollers.get(id)
      if (!current) return
      current.intervals.delete(intervalMs)
      if (current.intervals.size === 0) {
        if (current.timer) clearInterval(current.timer)
        pollers.delete(id)
        return
      }
      updateTimer(current, refresh)
    }
  }, [id, intervalMs, queryClient])

  return query
}
