import { useEffect, useRef } from 'react'
import { useTheme } from 'next-themes'

declare global { interface Window { Redoc?: { init: (url: string, options: object, target: HTMLElement) => void } } }

function options(dark: boolean) {
  const c = dark
    ? { bg: '#111f2e', side: '#152536', edge: '#29445b', ink: '#edf5fb', muted: '#a8bdcc', accent: '#75b9e6' }
    : { bg: '#f5f9fc', side: '#edf3f7', edge: '#cbdde8', ink: '#1f2937', muted: '#54616e', accent: '#176d9c' }
  return { theme: { colors: { primary: { main: c.accent }, text: { primary: c.ink, secondary: c.muted }, border: { dark: c.edge, light: c.edge }, responses: { success: { color: c.ink, backgroundColor: c.bg } } }, sidebar: { backgroundColor: c.side, textColor: c.muted, activeTextColor: c.ink }, rightPanel: { backgroundColor: c.bg, textColor: c.ink }, codeBlock: { backgroundColor: c.side }, typography: { fontFamily: 'Inter, sans-serif', headings: { fontFamily: 'Fraunces, serif' }, code: { fontFamily: 'JetBrains Mono, monospace', color: c.ink, backgroundColor: c.side } } }, hideDownloadButton: true, pathInMiddlePanel: true }
}

export default function OpenAPI() {
  const ref = useRef<HTMLDivElement>(null)
  const { resolvedTheme } = useTheme()
  useEffect(() => {
    const target = ref.current
    if (!target) return
    let cancelled = false
    const mount = () => {
      if (cancelled || !window.Redoc) return
      window.Redoc.init('/api/openapi.json', options(resolvedTheme === 'dark'), target)
      const dark = resolvedTheme === 'dark'
      const ink = dark ? '#edf5fb' : '#1f2937'
      const style = document.createElement('style')
      style.textContent = `.redoc-json .property.token.string,.redoc-json .collapser{color:${ink}!important}.redoc-wrap [role="tab"]{color:${ink}!important}`
      target.appendChild(style)
    }
    const id = 'spaniel-redoc-runtime'
    const script = document.getElementById(id) as HTMLScriptElement | null
    if (window.Redoc) mount()
    else if (script) script.addEventListener('load', mount, { once: true })
    else {
      const next = document.createElement('script')
      next.id = id; next.src = 'https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js'; next.async = true
      next.addEventListener('load', mount, { once: true }); document.head.appendChild(next)
    }
    return () => { cancelled = true; target.replaceChildren() }
  }, [resolvedTheme])
  return <main className="flex-1 overflow-y-auto bg-background p-4"><div ref={ref} className="mx-auto max-w-[1280px] overflow-hidden rounded-lg border border-border bg-surface" /></main>
}
