import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import { useTheme } from 'next-themes'
import { Moon, Sun } from 'lucide-react'
import { TooltipProvider } from '@/components/ui/tooltip'
import TraceList from './pages/TraceList'
import TraceDetail from './pages/TraceDetail'
import LogViewer from './pages/LogViewer'
import Sessions from './pages/Sessions'
import ServiceMap from './pages/ServiceMap'
import LintPage from './pages/LintPage'

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
        <NavPill to="/logs"           label="Logs"     />
        <NavPill to="/services"       label="Services" />
        <NavPill to="/lint"           label="Lint"     />
        <NavPill to="/sessions"       label="Sessions" />
      </nav>

      <div style={{ flex: 1 }} />

      {/* theme toggle */}
      <ThemeToggle />
    </header>
  )
}

// ── App ───────────────────────────────────────────────────────────────────────

export default function App() {
  return (
    <TooltipProvider delay={300}>
      <BrowserRouter>
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
              <Route path="/traces/:traceId"   element={<TraceDetail />} />
              <Route path="/logs"              element={<LogViewer />}   />
              <Route path="/services"          element={<ServiceMap />}  />
              <Route path="/lint"              element={<LintPage />}    />
              <Route path="/sessions"          element={<Sessions />}    />
            </Routes>
          </main>
        </div>
      </BrowserRouter>
    </TooltipProvider>
  )
}
