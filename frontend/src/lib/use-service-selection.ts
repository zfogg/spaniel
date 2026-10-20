import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'

// Repeated URL parameters preserve arbitrary service names and browser history.
export function useServiceSelection(
  legacyService = false,
): [string[], (services: string[]) => void] {
  const [params, setParams] = useSearchParams()
  const key = JSON.stringify(params.getAll('services'))
  const legacy = legacyService ? params.get('service') : null
  const selected = useMemo(() => {
    const services: string[] = JSON.parse(key)
    return services.length ? services : legacy && legacy !== 'all' ? [legacy] : []
  }, [key, legacy])
  const setSelected = (services: string[]) => {
    const next = new URLSearchParams(params)
    next.delete('services')
    if (legacyService) next.delete('service')
    for (const service of services) next.append('services', service)
    setParams(next, { replace: true })
  }
  return [selected, setSelected]
}
