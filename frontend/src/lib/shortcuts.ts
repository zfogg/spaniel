import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'

// Custom DOM event pages can listen to for Esc (close panel/modal).
export const ESCAPE_EVENT = 'spaniel:escape'

// Dispatched by ⌘K / Ctrl+K / `/` to open the command palette.
export const SEARCH_PALETTE_EVENT = 'spaniel:open-search'

function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

// Global keyboard shortcuts:
//   ⌘K / Ctrl+K / `/` — open command palette
//   n   — new session
//   b   — toggle baseline on the active session
//   d   — open diff view
//   Esc — dispatch close-panel event (and blur)
export function useGlobalShortcuts() {
  const navigate = useNavigate()

  useEffect(() => {
    async function onKey(e: KeyboardEvent) {
      // Esc always fires, even from inputs (to dismiss them).
      if (e.key === 'Escape') {
        if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
        window.dispatchEvent(new CustomEvent(ESCAPE_EVENT))
        return
      }

      // ⌘K / Ctrl+K opens palette even from inputs.
      if (e.key === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        window.dispatchEvent(new CustomEvent(SEARCH_PALETTE_EVENT))
        return
      }

      // Don't trigger letter shortcuts while typing.
      if (isTypingTarget(e.target)) return
      if (e.metaKey || e.ctrlKey || e.altKey) return

      switch (e.key) {
        case '/': {
          e.preventDefault()
          window.dispatchEvent(new CustomEvent(SEARCH_PALETTE_EVENT))
          return
        }
        case 'n': {
          e.preventDefault()
          await api.sessions.create().catch(() => {})
          navigate('/sessions')
          return
        }
        case 'b': {
          e.preventDefault()
          try {
            const active = await api.sessions.getActive()
            if (active.data?.id) {
              const cur = await api.sessions.get(active.data.id)
              await api.sessions.baseline(active.data.id, !cur.data?.is_baseline)
            }
          } catch {}
          return
        }
        case 'd': {
          e.preventDefault()
          navigate('/diff')
          return
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])
}
