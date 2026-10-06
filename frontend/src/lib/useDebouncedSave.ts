import { useCallback, useEffect, useRef, useState } from 'react'

type Options<T> = {
  enabled: boolean
  key: string
  save: (value: T) => Promise<void>
  value: T
  delay?: number
}

// Reusable for editors: delay writes while typing, flush link navigation, and
// let the browser warn before a pending change can be discarded on tab close.
export function useDebouncedSave<T>({ enabled, key, save, value, delay = 300 }: Options<T>) {
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const saveRef = useRef(save)
  const valueRef = useRef(value)
  const initial = useRef<{ key: string; value: T } | null>(null)
  const [pending, setPending] = useState(false)

  saveRef.current = save
  valueRef.current = value

  const flush = useCallback(async () => {
    if (timer.current) clearTimeout(timer.current)
    if (!pending) return
    await saveRef.current(valueRef.current)
    initial.current = { key, value: valueRef.current }
    setPending(false)
  }, [key, pending])

  useEffect(() => {
    if (!enabled) return
    if (!initial.current || initial.current.key !== key) {
      initial.current = { key, value }
      setPending(false)
      return
    }
    if (Object.is(initial.current.value, value)) return
    setPending(true)
    timer.current = setTimeout(() => { void flush() }, delay)
    return () => { if (timer.current) clearTimeout(timer.current) }
  }, [delay, enabled, flush, key, value])

  useEffect(() => () => { if (timer.current) clearTimeout(timer.current) }, [])

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!pending) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', onBeforeUnload)
    return () => window.removeEventListener('beforeunload', onBeforeUnload)
  }, [pending])

  useEffect(() => {
    const onLinkClick = (event: MouseEvent) => {
      if (!pending || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
      const link = (event.target as Element | null)?.closest('a[href]') as HTMLAnchorElement | null
      if (!link || link.target || link.origin !== window.location.origin) return
      event.preventDefault()
      event.stopPropagation()
      void flush().then(() => window.location.assign(link.href)).catch(() => {
        if (window.confirm('Your changes could not be saved. Leave this page anyway?')) window.location.assign(link.href)
      })
    }
    document.addEventListener('click', onLinkClick, true)
    return () => document.removeEventListener('click', onLinkClick, true)
  }, [flush, pending])

  return { flush, pending }
}
