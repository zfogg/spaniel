import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import { useTheme } from 'next-themes'
import { Moon, Sun, Search } from 'lucide-react'
import { useEffect, useState } from 'react'
import { TooltipProvider } from '@/components/ui/tooltip'
import { CommandPalette } from '@/components/CommandPalette'
import { SEARCH_PALETTE_EVENT } from '@/lib/shortcuts'
import TraceList from './pages/TraceList'
import Spans from './pages/Spans'
import TraceDetail from './pages/TraceDetail'
import LogViewer from './pages/LogViewer'
import Sessions from './pages/Sessions'
import ServiceMap from './pages/ServiceMap'
import LintPage from './pages/LintPage'
import DiffPage from './pages/DiffPage'
import Metrics from './pages/Metrics'
import Coverage from './pages/Coverage'
import Settings from './pages/Settings'
import BottomBar from './components/BottomBar'
import IssueToast from './components/IssueToast'
import RouteErrorBoundary from './components/RouteErrorBoundary'
import { Toaster } from 'sonner'
import { NuqsAdapter } from 'nuqs/adapters/react-router/v7'
import { useGlobalShortcuts } from './lib/shortcuts'
import { useQuery } from '@tanstack/react-query'
import { qk, useLiveInvalidation } from './lib/query'
import { api } from './lib/api'

// ── Spaniel logo SVG ──────────────────────────────────────────────────────────

function SpanielLogo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 28 28" className="block shrink-0">
      <ellipse cx="8"  cy="13" rx="6" ry="9" fill="var(--accent)" opacity="0.78" transform="rotate(-12 8 13)" />
      <ellipse cx="20" cy="13" rx="6" ry="9" fill="var(--accent)" opacity="0.50" transform="rotate(12 20 13)" />
      <circle cx="14" cy="14" r="5.4" fill="var(--background)" stroke="var(--foreground)" strokeWidth="1.2" />
      <circle cx="12.2" cy="13.5" r="0.9" fill="var(--foreground)" />
      <circle cx="15.8" cy="13.5" r="0.9" fill="var(--foreground)" />
      <path d="M12.6 16.6 Q14 17.6 15.4 16.6" stroke="var(--foreground)" strokeWidth="1.1" fill="none" strokeLinecap="round" />
    </svg>
  )
}

// ── Nav link pill ─────────────────────────────────────────────────────────────

function NavPill({ to, end, label }: { to: string; end?: boolean; label: string }) {
  return (
    <NavLink to={to} end={end}>
      {({ isActive }) => (
        <span
          className={`inline-block px-2.5 py-[5px] rounded-md font-sans text-xs font-medium cursor-pointer select-none whitespace-nowrap transition-colors border ${
            isActive
              ? 'text-foreground bg-muted border-border'
              : 'text-muted-foreground bg-transparent border-transparent'
          }`}
        >
          {label}
        </span>
      )}
    </NavLink>
  )
}

// ── Theme toggle ──────────────────────────────────────────────────────────────

function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme()
  const isDark = resolvedTheme === 'dark'

  return (
    <button
      type="button"
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      title={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
      aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
      className="inline-flex items-center justify-center w-7 h-7 rounded-md bg-transparent border border-border text-muted-foreground cursor-pointer outline-none transition-colors shrink-0 hover:bg-muted hover:text-foreground"
    >
      {isDark ? <Sun size={14} /> : <Moon size={14} />}
    </button>
  )
}

function fmtBytes(n: number): string {
  if (!n) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0, v = n
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${u[i]}`
}

// ── Forwarding status pills ───────────────────────────────────────────────────

function ForwardingPills() {
  const { data: statuses = [] } = useQuery({
    queryKey: qk.forwarders(),
    queryFn: () => api.forwarders.list().then(r => r.data),
    refetchInterval: 5000,
  })

  if (statuses.length === 0) return null

  return (
    <>
      <div className="w-px h-[18px] bg-border shrink-0" />
      <div className="flex gap-1 items-center">
        {statuses.map(s => {
          const hasError = s.errors > 0
          const hasDropped = (s.dropped_spool ?? 0) > 0
          const pendingBytes = s.pending_bytes ?? 0
          const label = new URL(s.url).host
          const tone = hasDropped ? 'text-white bg-warn' : hasError ? 'text-white bg-destructive' : 'text-muted-foreground bg-muted'
          const titleParts = [`→ ${s.url}`, `sent: ${s.sent}`]
          if (hasError) titleParts.push(`errors: ${s.errors} — last: ${s.last_error}`)
          if (pendingBytes > 0) titleParts.push(`queued: ${fmtBytes(pendingBytes)}`)
          if (hasDropped) titleParts.push(`dropped: ${s.dropped_spool}`)
          return (
            <span
              key={s.url}
              title={titleParts.join('  ')}
              className={`inline-flex items-center gap-1 px-[7px] py-[3px] rounded-[5px] font-mono text-[10px] font-medium border border-border whitespace-nowrap cursor-default select-none ${tone}`}
            >
              <span className="opacity-60">→</span>
              {label}
              {pendingBytes > 0 && (
                <span className="opacity-85">{fmtBytes(pendingBytes)} queued</span>
              )}
              {hasDropped
                ? <span className="opacity-85">⚠</span>
                : hasError
                  ? <span className="opacity-85">✗</span>
                  : <span className="opacity-55">✓</span>
              }
            </span>
          )
        })}
      </div>
    </>
  )
}

// ── Chrome navbar ─────────────────────────────────────────────────────────────

function Chrome() {
  return (
    <header className="h-[46px] px-4 border-b border-border bg-background flex items-center gap-[14px] shrink-0">
      {/* brand */}
      <div className="flex items-center gap-2">
        <SpanielLogo size={22} />
        <span className="font-sans text-[15px] font-semibold text-foreground tracking-[-0.01em] leading-none">
          spaniel
        </span>
        <span className="font-mono text-[9px] text-muted-foreground tracking-[0.06em]">
          v0.1
        </span>
      </div>

      {/* divider */}
      <div className="w-px h-[18px] bg-border shrink-0" />

      {/* nav */}
      <nav className="flex gap-0.5">
        <NavPill to="/"         end   label="Traces"   />
        <NavPill to="/spans"          label="Spans"    />
        <NavPill to="/logs"           label="Logs"     />
        <NavPill to="/metrics"        label="Metrics"  />
        <NavPill to="/services"       label="Services" />
        <NavPill to="/coverage"       label="Coverage" />
        <NavPill to="/lint"           label="Lint"     />
        <NavPill to="/sessions"       label="Sessions" />
        <NavPill to="/settings"       label="Settings" />
      </nav>

      <div className="flex-1" />

      {/* forwarding status */}
      <ForwardingPills />

      {/* search trigger */}
      <button
        type="button"
        onClick={() => window.dispatchEvent(new CustomEvent(SEARCH_PALETTE_EVENT))}
        title="Search (⌘K)"
        className="flex items-center gap-2 rounded-md border border-border bg-transparent px-2.5 py-1 font-sans text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
      >
        <Search size={12} />
        <span className="hidden sm:inline">Search</span>
        <kbd className="hidden rounded border border-border px-1 py-px font-mono text-[9px] sm:inline-block">⌘K</kbd>
      </button>

      {/* theme toggle */}
      <ThemeToggle />
    </header>
  )
}

// ── App ───────────────────────────────────────────────────────────────────────

function AppShell() {
  useGlobalShortcuts()
  useLiveInvalidation()
  const [paletteOpen, setPaletteOpen] = useState(false)

  // Seed the auth cookie when the user first visits with ?token=<value>.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const token = params.get('token')
    if (!token) return
    api.health.seed(token).then(() => {
      params.delete('token')
      const qs = params.toString()
      window.history.replaceState({}, '', window.location.pathname + (qs ? '?' + qs : ''))
    }).catch(() => { /* server will reject API calls with 401 until token is correct */ })
  }, [])

  useEffect(() => {
    const open = () => setPaletteOpen(true)
    window.addEventListener(SEARCH_PALETTE_EVENT, open)
    return () => window.removeEventListener(SEARCH_PALETTE_EVENT, open)
  }, [])

  return (
    <div className="flex flex-col h-screen overflow-hidden bg-background">
      <Chrome />
      <main className="flex-1 overflow-hidden flex flex-col">
        <RouteErrorBoundary>
        <Routes>
          <Route path="/"                  element={<TraceList />}   />
          <Route path="/spans"             element={<Spans />}       />
          <Route path="/traces/:traceId"   element={<TraceDetail />} />
          <Route path="/logs"              element={<LogViewer />}   />
          <Route path="/metrics"           element={<Metrics />}     />
          <Route path="/coverage"          element={<Coverage />}    />
          <Route path="/settings"          element={<Settings />}    />
          <Route path="/services"          element={<ServiceMap />}  />
          <Route path="/lint"              element={<LintPage />}    />
          <Route path="/sessions"          element={<Sessions />}    />
          <Route path="/diff"              element={<DiffPage />}    />
        </Routes>
        </RouteErrorBoundary>
      </main>
      <BottomBar />
      <IssueToast />
      <Toaster position="bottom-right" offset={44} visibleToasts={3} />
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </div>
  )
}

export default function App() {
  return (
    <TooltipProvider delay={300}>
      <BrowserRouter>
        <NuqsAdapter>
          <AppShell />
        </NuqsAdapter>
      </BrowserRouter>
    </TooltipProvider>
  )
}
