import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'

// Custom DOM event pages can listen to for Esc (close panel/modal).
export const ESCAPE_EVENT = 'spaniel:escape'

function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

// Global keyboard shortcuts per spec #29:
//   n   — new session
//   b   — toggle baseline on the active session
//   d   — open diff view
//   /   — focus search ([data-shortcut="search"])
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

      // Don't trigger letter shortcuts while typing.
      if (isTypingTarget(e.target)) return
      if (e.metaKey || e.ctrlKey || e.altKey) return

      switch (e.key) {
        case '/': {
          const el = document.querySelector<HTMLElement>('[data-shortcut="search"]')
          if (el) {
            e.preventDefault()
            el.focus()
            if (el instanceof HTMLInputElement) el.select()
          }
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
