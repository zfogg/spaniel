const preferredKeys = ['http.route', 'http.request.method', 'http.response.status_code', 'server.address', 'server.port']
const labels: Record<string, string> = {
  'http.route': 'route',
  'http.request.method': 'method',
  'http.response.status_code': 'status',
  'server.address': 'server',
  'server.port': 'port',
}

export function CatalogAttributes({ attributes }: { attributes?: Record<string, unknown> }) {
  if (!attributes) return null
  const entries = Object.entries(attributes).filter(([key]) => key !== 'percentile').sort(([a], [b]) => {
    const rank = (key: string) => preferredKeys.includes(key) ? preferredKeys.indexOf(key) : preferredKeys.length
    return rank(a) - rank(b) || a.localeCompare(b)
  })
  if (!entries.length) return null
  return <dl aria-label="Metric stream attributes" className="my-1.5 flex flex-wrap gap-1">
    {entries.map(([key, value]) => {
      const text = typeof value === 'string' ? value || '""' : JSON.stringify(value) ?? String(value)
      return <div key={key} title={`${key}: ${text}`} className="flex min-w-0 max-w-full items-baseline gap-1 rounded border border-border bg-background/60 px-1.5 py-0.5 font-mono text-[10px] leading-4">
        <dt className="shrink-0 text-muted-foreground">{labels[key] ?? key}</dt>
        <dd className="min-w-0 break-all font-medium text-foreground">{text}</dd>
      </div>
    })}
  </dl>
}
