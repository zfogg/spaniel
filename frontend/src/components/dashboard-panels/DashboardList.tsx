import { useState } from 'react'
import { Link } from 'react-router-dom'
import { GripVertical, LayoutDashboard } from 'lucide-react'
import type { Dashboard } from '@/lib/api'

export function DashboardList({
  dashboards,
  selectedId,
  href,
  reorder,
  pending,
}: {
  dashboards: Dashboard[]
  selectedId?: string
  href: (id: string, index: number) => string
  reorder: (from: string, to: string) => void
  pending: boolean
}) {
  const [dragged, setDragged] = useState<string | null>(null)
  const [over, setOver] = useState<string | null>(null)
  return (
    <div aria-label="Dashboard list">
      {dashboards.map((dashboard, index) => (
        <div
          key={dashboard.id}
          onDragOver={(event) => {
            if (dragged && !pending) {
              event.preventDefault()
              event.dataTransfer.dropEffect = 'move'
              setOver(dashboard.id)
            }
          }}
          onDrop={(event) => {
            event.preventDefault()
            if (dragged && !pending) reorder(dragged, dashboard.id)
            setDragged(null)
            setOver(null)
          }}
          className={`flex items-center border-l-2 ${over === dashboard.id && dragged !== dashboard.id ? 'outline outline-2 -outline-offset-2 outline-accent' : ''} ${selectedId === dashboard.id ? 'border-accent bg-accent-bg font-semibold text-accent-ink' : 'border-transparent text-muted-foreground hover:bg-muted'}`}
        >
          <button
            type="button"
            draggable={!pending}
            disabled={pending}
            aria-label={`Reorder ${dashboard.name}`}
            title="Drag to reorder · or use Up/Down arrow keys"
            onDragStart={(event) => {
              event.dataTransfer.setData('text/plain', dashboard.id)
              event.dataTransfer.effectAllowed = 'move'
              setDragged(dashboard.id)
            }}
            onDragEnd={() => {
              setDragged(null)
              setOver(null)
            }}
            onKeyDown={(event) => {
              const direction = event.key === 'ArrowUp' ? -1 : event.key === 'ArrowDown' ? 1 : 0
              if (!direction) return
              event.preventDefault()
              const target = dashboards[index + direction]
              if (target && !pending) reorder(dashboard.id, target.id)
            }}
            className="ml-1 cursor-grab rounded px-1 py-2 text-muted-foreground hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent active:cursor-grabbing disabled:cursor-wait disabled:opacity-50"
          >
            <GripVertical size={14} />
          </button>
          <Link
            aria-label={dashboard.name}
            aria-current={selectedId === dashboard.id ? 'page' : undefined}
            to={href(dashboard.id, index)}
            className="flex min-w-0 flex-1 items-center gap-2 py-2 pr-3 text-sm"
          >
            <LayoutDashboard size={14} className="shrink-0" />
            <span className="min-w-0 flex-1 truncate">{dashboard.name}</span>
            <small className="font-mono text-[10px] text-muted-foreground">
              {dashboard.panels.length}
            </small>
          </Link>
        </div>
      ))}
    </div>
  )
}
