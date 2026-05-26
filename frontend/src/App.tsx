import { BrowserRouter, Routes, Route, NavLink } from 'react-router-dom'
import TraceList from './pages/TraceList'
import TraceDetail from './pages/TraceDetail'
import LogViewer from './pages/LogViewer'
import Sessions from './pages/Sessions'
import SessionSidebar from './components/SessionSidebar'

export default function App() {
  return (
    <BrowserRouter>
      <div className="flex h-screen overflow-hidden" style={{ background: '#111' }}>
        <aside className="w-56 flex-none border-r border-[#2a2a2a] flex flex-col">
          <div className="px-4 py-4 border-b border-[#2a2a2a]">
            <span className="font-semibold text-white text-base tracking-tight">spaniel 🐕</span>
          </div>
          <nav className="flex flex-col gap-1 px-2 py-3">
            <NavLink
              to="/"
              end
              className={({ isActive }) =>
                `px-3 py-2 rounded text-sm ${isActive ? 'bg-[#2a2a2a] text-white' : 'text-[#888] hover:text-white hover:bg-[#1e1e1e]'}`
              }
            >
              Traces
            </NavLink>
            <NavLink
              to="/logs"
              className={({ isActive }) =>
                `px-3 py-2 rounded text-sm ${isActive ? 'bg-[#2a2a2a] text-white' : 'text-[#888] hover:text-white hover:bg-[#1e1e1e]'}`
              }
            >
              Logs
            </NavLink>
            <NavLink
              to="/sessions"
              className={({ isActive }) =>
                `px-3 py-2 rounded text-sm ${isActive ? 'bg-[#2a2a2a] text-white' : 'text-[#888] hover:text-white hover:bg-[#1e1e1e]'}`
              }
            >
              Sessions
            </NavLink>
          </nav>
          <div className="flex-1 overflow-y-auto">
            <SessionSidebar />
          </div>
        </aside>

        <main className="flex-1 overflow-y-auto">
          <Routes>
            <Route path="/" element={<TraceList />} />
            <Route path="/traces/:traceId" element={<TraceDetail />} />
            <Route path="/logs" element={<LogViewer />} />
            <Route path="/sessions" element={<Sessions />} />
          </Routes>
        </main>
      </div>
    </BrowserRouter>
  )
}
