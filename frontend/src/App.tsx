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
import { useGlobalShortcuts } from './lib/shortcuts'
import { api, type ForwarderStatus } from './lib/api'

// ── Spaniel logo SVG ──────────────────────────────────────────────────────────

function SpanielLogo({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 28 28" style={{ display: 'block', flexShrink: 0 }}>
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
        <span style={{
          display: 'inline-block',
          padding: '5px 10px',
          borderRadius: 6,
          fontFamily: 'var(--font-sans)',
          fontSize: 12,
          fontWeight: 500,
          color: isActive ? 'var(--foreground)' : 'var(--muted-foreground)',
          background: isActive ? 'var(--muted)' : 'transparent',
          border: isActive ? '1px solid var(--border)' : '1px solid transparent',
          cursor: 'pointer',
          userSelect: 'none',
          whiteSpace: 'nowrap',
          transition: 'color 0.1s, background 0.1s',
        }}>
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
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: 28,
        height: 28,
        borderRadius: 6,
        background: 'transparent',
        border: '1px solid var(--border)',
        color: 'var(--muted-foreground)',
        cursor: 'pointer',
        outline: 'none',
        transition: 'color 0.1s, background 0.1s',
        flexShrink: 0,
      }}
      onMouseEnter={e => {
        (e.currentTarget as HTMLButtonElement).style.background = 'var(--muted)'
        ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--foreground)'
      }}
      onMouseLeave={e => {
        (e.currentTarget as HTMLButtonElement).style.background = 'transparent'
        ;(e.currentTarget as HTMLButtonElement).style.color = 'var(--muted-foreground)'
      }}
    >
      {isDark ? <Sun size={14} /> : <Moon size={14} />}
    </button>
  )
}

// ── Forwarding status pills ───────────────────────────────────────────────────

function ForwardingPills() {
  const [statuses, setStatuses] = useState<ForwarderStatus[]>([])

  useEffect(() => {
    let alive = true
    const poll = async () => {
      try {
        const { data } = await api.forwarders.list()
        if (alive) setStatuses(data)
      } catch {
        // server may not be up yet; silently retry
      }
    }
    poll()
    const id = setInterval(poll, 5000)
    return () => { alive = false; clearInterval(id) }
  }, [])

  if (statuses.length === 0) return null

  return (
    <>
      <div style={{ width: 1, height: 18, background: 'var(--border)', flexShrink: 0 }} />
      <div style={{ display: 'flex', gap: 4, alignItems: 'center' }}>
        {statuses.map(s => {
          const hasError = s.errors > 0
          const label = new URL(s.url).host
          return (
            <span
              key={s.url}
              title={hasError ? `${s.errors} error(s) — last: ${s.last_error}` : `→ ${s.url}  sent: ${s.sent}`}
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 4,
                padding: '3px 7px',
                borderRadius: 5,
                fontFamily: 'var(--font-mono)',
                fontSize: 10,
                fontWeight: 500,
                color: hasError ? 'var(--destructive-foreground, #fff)' : 'var(--muted-foreground)',
                background: hasError ? 'var(--destructive, #c0392b)' : 'var(--muted)',
                border: '1px solid var(--border)',
                whiteSpace: 'nowrap',
                cursor: 'default',
                userSelect: 'none',
              }}
            >
              <span style={{ opacity: 0.6 }}>→</span>
              {label}
              {hasError
                ? <span style={{ opacity: 0.85 }}>✗</span>
                : <span style={{ opacity: 0.55 }}>✓</span>
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
    <header style={{
      height: 46,
      padding: '0 16px',
      borderBottom: '1px solid var(--border)',
      background: 'var(--background)',
      display: 'flex',
      alignItems: 'center',
      gap: 14,
      flexShrink: 0,
    }}>
      {/* brand */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <SpanielLogo size={22} />
        <span style={{
          fontFamily: 'var(--font-sans)',
          fontSize: 15,
          fontWeight: 600,
          color: 'var(--foreground)',
          letterSpacing: '-0.01em',
          lineHeight: 1,
        }}>
          spaniel
        </span>
        <span style={{
          fontFamily: 'var(--font-mono)',
          fontSize: 9,
          color: 'var(--muted-foreground)',
          letterSpacing: '0.06em',
        }}>
          v0.1
        </span>
      </div>

      {/* divider */}
      <div style={{ width: 1, height: 18, background: 'var(--border)', flexShrink: 0 }} />

      {/* nav */}
      <nav style={{ display: 'flex', gap: 2 }}>
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

      <div style={{ flex: 1 }} />

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
  const [paletteOpen, setPaletteOpen] = useState(false)

  useEffect(() => {
    const open = () => setPaletteOpen(true)
    window.addEventListener(SEARCH_PALETTE_EVENT, open)
    return () => window.removeEventListener(SEARCH_PALETTE_EVENT, open)
  }, [])

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      height: '100vh',
      overflow: 'hidden',
      background: 'var(--background)',
    }}>
      <Chrome />
      <main style={{ flex: 1, overflow: 'hidden' }}>
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
      </main>
      <BottomBar />
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
    </div>
  )
}

export default function App() {
  return (
    <TooltipProvider delay={300}>
      <BrowserRouter>
        <AppShell />
      </BrowserRouter>
    </TooltipProvider>
  )
}
