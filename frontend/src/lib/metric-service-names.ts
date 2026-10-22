import type { MetricCatalogEntry } from './api'

export function metricServiceNames(catalog: MetricCatalogEntry[], metricName: string): string[] {
  const latest = new Map<string, number>()
  for (const metric of catalog) {
    if (metric.name !== metricName) continue
    latest.set(
      metric.service_name,
      Math.max(latest.get(metric.service_name) ?? -Infinity, metric.last_timestamp_ns),
    )
  }
  return [...latest.keys()].sort((a, b) => latest.get(b)! - latest.get(a)! || a.localeCompare(b))
}
