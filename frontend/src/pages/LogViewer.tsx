import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, Log } from '../lib/api'

const SEVERITY_LABELS: Record<number, string> = {
  1: 'TRACE', 2: 'TRACE2', 3: 'TRACE3', 4: 'TRACE4',
  5: 'DEBUG', 6: 'DEBUG2', 7: 'DEBUG3', 8: 'DEBUG4',
  9: 'INFO', 10: 'INFO2', 11: 'INFO3', 12: 'INFO4',
  13: 'WARN', 14: 'WARN2', 15: 'WARN3', 16: 'WARN4',
  17: 'ERROR', 18: 'ERROR2', 19: 'ERROR3', 20: 'ERROR4',
  21: 'FATAL', 22: 'FATAL2', 23: 'FATAL3', 24: 'FATAL4',
}

function severityVariant(n: number): 'default' | 'secondary' | 'destructive' | 'outline' {
  if (n >= 17) return 'destructive'
  if (n >= 13) return 'outline'
  if (n >= 9) return 'secondary'
  return 'secondary'
}

function fmtTs(ns: number) {
  return new Date(ns / 1_000_000).toLocaleTimeString(undefined, { hour12: false })
}

export default function LogViewer() {
  const [logs, setLogs] = useState<Log[]>([])
  const [services, setServices] = useState<string[]>([])
  const [filterService, setFilterService] = useState('all')
  const [filterSeverity, setFilterSeverity] = useState('all')
  const [loading, setLoading] = useState(true)
  const navigate = useNavigate()

  useEffect(() => {
    api.logs.list({}).then(r => {
      setLogs(r.data ?? [])
      setLoading(false)
    }).catch(() => setLoading(false))
    api.services.list().then(r => setServices(r.data ?? []))
  }, [])

  const filtered = logs.filter(l => {
    if (filterService !== 'all' && l.service_name !== filterService) return false
    if (filterSeverity === 'error' && l.severity < 17) return false
    if (filterSeverity === 'warn' && l.severity < 13) return false
    if (filterSeverity === 'info' && l.severity < 9) return false
    return true
  })

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-3 px-6 py-4 border-b border-border">
        <h1 className="text-sm font-medium flex-1">Logs</h1>
        <Select value={filterService} onValueChange={v => setFilterService(v ?? 'all')}>
          <SelectTrigger className="w-40 h-8 text-xs">
            <SelectValue placeholder="All services" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All services</SelectItem>
            {services.map(s => <SelectItem key={s} value={s}>{s}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={filterSeverity} onValueChange={v => setFilterSeverity(v ?? 'all')}>
          <SelectTrigger className="w-32 h-8 text-xs">
            <SelectValue placeholder="All levels" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All levels</SelectItem>
            <SelectItem value="info">INFO+</SelectItem>
            <SelectItem value="warn">WARN+</SelectItem>
            <SelectItem value="error">ERROR+</SelectItem>
          </SelectContent>
        </Select>
      </div>

      {loading ? (
        <div className="p-8 text-muted-foreground text-sm">Loading…</div>
      ) : filtered.length === 0 ? (
        <div className="flex items-center justify-center h-full text-muted-foreground text-sm">No logs yet</div>
      ) : (
        <div className="overflow-auto flex-1">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-24">Time</TableHead>
                <TableHead className="w-24">Level</TableHead>
                <TableHead className="w-32">Service</TableHead>
                <TableHead>Body</TableHead>
                <TableHead className="w-28">Trace</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((l, i) => (
                <TableRow key={i}>
                  <TableCell className="font-mono text-xs text-muted-foreground">{fmtTs(l.timestamp_ns)}</TableCell>
                  <TableCell>
                    <Badge variant={severityVariant(l.severity)} className="font-mono text-[10px]">
                      {SEVERITY_LABELS[l.severity] ?? String(l.severity)}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">{l.service_name}</TableCell>
                  <TableCell className="font-mono text-xs max-w-md truncate">{l.body}</TableCell>
                  <TableCell>
                    {l.trace_id && l.trace_id !== '00000000000000000000000000000000' && (
                      <button
                        onClick={() => navigate(`/traces/${l.trace_id}`)}
                        className="font-mono text-[10px] text-primary hover:underline truncate w-24 text-left block"
                      >
                        {l.trace_id.slice(0, 8)}…
                      </button>
                    )}
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
