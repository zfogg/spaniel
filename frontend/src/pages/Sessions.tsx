import { useEffect, useState } from 'react'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { api, Session } from '../lib/api'

function fmtDate(ns: number) {
  return new Date(ns / 1_000_000).toLocaleString()
}

export default function Sessions() {
  const [sessions, setSessions] = useState<Session[]>([])
  const [loading, setLoading] = useState(true)

  function load() {
    api.sessions.list().then(r => {
      setSessions(r.data ?? [])
      setLoading(false)
    })
  }

  useEffect(() => { load() }, [])

  async function createSession() {
    await api.sessions.create()
    load()
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-3 px-6 py-4 border-b border-border">
        <h1 className="text-sm font-medium flex-1">Sessions</h1>
        <Button size="sm" onClick={createSession}>New Session</Button>
      </div>

      {loading ? (
        <div className="p-8 text-muted-foreground text-sm">Loading…</div>
      ) : sessions.length === 0 ? (
        <div className="flex items-center justify-center h-full text-muted-foreground text-sm">No sessions</div>
      ) : (
        <div className="overflow-auto flex-1">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Label</TableHead>
                <TableHead>ID</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-24 text-right">Spans</TableHead>
                <TableHead className="w-24">Flags</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {sessions.map(s => (
                <TableRow key={s.id}>
                  <TableCell className="font-medium text-sm">{s.label}</TableCell>
                  <TableCell className="font-mono text-xs text-muted-foreground">{s.id}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{fmtDate(s.created_at)}</TableCell>
                  <TableCell className="text-right font-mono text-xs">{s.span_count}</TableCell>
                  <TableCell>
                    {s.is_baseline && <Badge variant="secondary" className="text-[10px]">baseline</Badge>}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
