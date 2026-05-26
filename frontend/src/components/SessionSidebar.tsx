import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { api, Session } from '../lib/api'

export default function SessionSidebar() {
  const [sessions, setSessions] = useState<Session[]>([])

  function load() {
    api.sessions.list().then(r => setSessions(r.data ?? []))
  }

  useEffect(() => { load() }, [])

  async function newSession() {
    await api.sessions.create()
    load()
  }

  if (sessions.length === 0) return null

  return (
    <div className="px-2 py-3">
      <div className="flex items-center justify-between px-2 mb-2">
        <span className="text-[10px] uppercase tracking-wider text-muted-foreground">Sessions</span>
        <Button variant="ghost" size="sm" className="h-5 text-[10px] px-1" onClick={newSession}>
          + New
        </Button>
      </div>
      <Separator className="mb-2" />
      <div className="flex flex-col gap-0.5">
        {sessions.slice(0, 8).map((s, i) => (
          <div
            key={s.id}
            className={`px-2 py-1.5 rounded text-xs flex items-center gap-1.5 ${i === 0 ? 'bg-muted/50 text-foreground' : 'text-muted-foreground hover:text-foreground hover:bg-muted/20'}`}
          >
            {i === 0 && <span className="w-1.5 h-1.5 rounded-full bg-success flex-none" />}
            <span className="truncate">{s.label}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
