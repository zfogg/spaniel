import { useState, useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './api'
import { qk } from './query'
export function useDefaultTimeRange<T extends string>() {
  const { data } = useQuery({
    queryKey: qk.settings(),
    queryFn: () => api.settings.get().then((r) => r.data),
  })
  const [selected, setSelected] = useState<T>()
  return [selected ?? ((data?.default_time_range ?? '1h') as T), setSelected] as const
}

export function useRefreshInterval() {
  const { data } = useQuery({
    queryKey: qk.settings(),
    queryFn: () => api.settings.get().then((r) => r.data),
  })
  const seconds = data?.refresh_interval_seconds ?? 5
  return seconds > 0 ? seconds * 1000 : false
}
export function usePeriodicRefresh() {
  const interval = useRefreshInterval()
  const qc = useQueryClient()
  useEffect(() => {
    if (!interval) return
    const timer = setInterval(() => {
      if (document.visibilityState === 'hidden') return
      void qc.refetchQueries(
        {
          type: 'active',
          predicate: (q) =>
            [
              'traces',
              'spans',
              'logs',
              'metrics',
              'metric-series',
              'service-map',
              'coverage',
              'issues',
              'settings',
            ].includes(String(q.queryKey[0])),
        },
        { cancelRefetch: false },
      )
    }, interval)
    return () => clearInterval(timer)
  }, [interval, qc])
}
