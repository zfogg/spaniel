import ServiceNameBadge from './ServiceNameBadge'
export default function MetricServiceBadges({ names }: { names: string[] }) {
  const visible = names.slice(0, 7)
  const remaining = names.slice(7)
  return (
    <ul
      aria-label="Services reporting this metric, newest first"
      className="my-2 flex list-none flex-wrap items-center gap-1.5"
    >
      {visible.map((name) => (
        <li key={name} className="inline-flex min-w-0 max-w-[180px]">
          <ServiceNameBadge name={name} />
        </li>
      ))}
      {remaining.length > 0 && (
        <li title={remaining.join('\n')} className="font-mono text-[11px] text-muted-foreground">
          ... and {remaining.length} more
        </li>
      )}
    </ul>
  )
}
