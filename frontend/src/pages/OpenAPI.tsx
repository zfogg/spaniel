import { useEffect, useRef } from 'react'
import { useTheme } from 'next-themes'
import { Braces } from 'lucide-react'
import { Link } from 'react-router-dom'

declare global {
  interface Window {
    Redoc?: { init: (document: string | object, options: object, target: HTMLElement) => void }
  }
}

type OpenAPIDocument = {
  paths?: Record<string, Record<string, { parameters?: Array<Record<string, unknown>> }>>
}

function makeParameterDescriptionsVisible(document: OpenAPIDocument) {
  // ReDoc currently renders a parameter description for an object but omits it
  // for scalar query parameters. Mirroring the canonical parameter description
  // onto its schema keeps every query filter equally discoverable in the UI.
  for (const path of Object.values(document.paths ?? {})) {
    for (const operation of Object.values(path)) {
      for (const parameter of operation?.parameters ?? []) {
        if (parameter.in !== 'query' || typeof parameter.description !== 'string') continue
        const schema = parameter.schema
        if (!schema || typeof schema !== 'object' || Array.isArray(schema)) continue
        if (!('description' in schema)) schema.description = parameter.description
      }
    }
  }
  return document
}

function options(dark: boolean) {
  const c = dark
    ? {
        bg: '#111f2e',
        side: '#152536',
        edge: '#29445b',
        ink: '#edf5fb',
        muted: '#a8bdcc',
        accent: '#75b9e6',
      }
    : {
        bg: '#f5f9fc',
        side: '#edf3f7',
        edge: '#cbdde8',
        ink: '#1f2937',
        muted: '#54616e',
        accent: '#176d9c',
      }
  return {
    theme: {
      colors: {
        primary: { main: c.accent },
        text: { primary: c.ink, secondary: c.muted },
        border: { dark: c.edge, light: c.edge },
        responses: { success: { color: c.ink, backgroundColor: c.bg } },
      },
      sidebar: { backgroundColor: c.side, textColor: c.muted, activeTextColor: c.ink },
      rightPanel: { backgroundColor: c.bg, textColor: c.ink },
      codeBlock: { backgroundColor: c.side },
      schema: { nestedBackground: c.side, linesColor: c.edge },
      typography: {
        fontFamily: 'Inter, sans-serif',
        headings: { fontFamily: 'Fraunces, serif' },
        code: { fontFamily: 'JetBrains Mono, monospace', color: c.ink, backgroundColor: c.side },
      },
    },
    hideDownloadButton: true,
    pathInMiddlePanel: true,
  }
}

function applyThemeOverrides(dark: boolean) {
  const bg = dark ? '#111f2e' : '#f5f9fc'
  const side = dark ? '#152536' : '#edf3f7'
  const ink = dark ? '#edf5fb' : '#1f2937'
  const selected = dark ? '#29445b' : '#dbe8f1'
  const styleID = 'spaniel-redoc-contrast'
  const style = document.getElementById(styleID) ?? document.createElement('style')
  style.id = styleID
  // ReDoc captures its styled-components palette when it first mounts. The
  // application theme can change later, so keep the three ReDoc columns and
  // their text tied to the current application palette independently.
  style.textContent = `
    .redoc-wrap,.redoc-wrap .api-content{background:${bg}!important;color:${ink}!important}
    .redoc-wrap>.menu-content{background:${side}!important;color:${ink}!important}
    .redoc-wrap>.menu-content *{background-color:${side}!important;color:${ink}!important}
    .redoc-wrap>:nth-child(2){background:${bg}!important;color:${ink}!important}
    .redoc-wrap>:nth-child(4),.redoc-wrap pre{background:${side}!important;color:${ink}!important}
    .redoc-wrap :is(.react-tabs__tab-panel,[class*="sc-gsFSXq"]){background:${side}!important;color:${ink}!important}
    .redoc-wrap td[colspan="2"]>div{background:${side}!important;color:${ink}!important}
    .redoc-wrap .api-content :is(h1,h2,h3,h4,h5,h6,p,td,th,label,span){color:${ink}!important}
    .redoc-json .property.token.string,.redoc-json .collapser{color:${ink}!important}
    .redoc-wrap [role="tab"]{background:${side}!important;color:${ink}!important}
    .redoc-wrap [role="tab"][aria-selected="true"]{background:${selected}!important;color:${ink}!important}
  `
  if (!style.parentNode) document.head.appendChild(style)
}

export default function OpenAPI() {
  const ref = useRef<HTMLDivElement>(null)
  const initialized = useRef(false)
  const { resolvedTheme } = useTheme()
  useEffect(() => {
    if (resolvedTheme) applyThemeOverrides(resolvedTheme === 'dark')
  }, [resolvedTheme])
  useEffect(() => {
    const target = ref.current
    // ReDoc owns a React tree below this node. Clearing it during React's
    // development remount cycle races ReDoc's own cleanup and throws
    // "removeChild ... not a child". Mount once and leave lifecycle ownership
    // to the host element.
    if (!target || !resolvedTheme || initialized.current) return
    initialized.current = true
    const mount = async () => {
      if (!window.Redoc || target.dataset.redocMounted === 'true') return
      target.dataset.redocMounted = 'true'
      const response = await fetch('/api/openapi.json')
      if (!response.ok) throw new Error(`The OpenAPI document returned ${response.status}.`)
      const document = makeParameterDescriptionsVisible((await response.json()) as OpenAPIDocument)
      window.Redoc.init(document, options(resolvedTheme === 'dark'), target)
    }
    const id = 'spaniel-redoc-runtime'
    const script = document.getElementById(id) as HTMLScriptElement | null
    if (window.Redoc) mount()
    else if (script) script.addEventListener('load', mount, { once: true })
    else {
      const next = document.createElement('script')
      next.id = id
      next.src = 'https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js'
      next.async = true
      next.addEventListener('load', mount, { once: true })
      document.head.appendChild(next)
    }
  }, [resolvedTheme])
  return (
    <main className="openapi-docs-scroll min-h-0 flex-1 overflow-y-scroll overscroll-contain bg-background p-4">
      <header className="mx-auto mb-4 flex max-w-[1280px] items-center justify-between rounded-lg border border-border bg-surface px-4 py-3 shadow-sm">
        <div>
          <p className="font-mono text-[10px] tracking-[0.14em] text-muted-foreground">
            OPENAPI REFERENCE
          </p>
          <h1 className="mt-0.5 font-serif text-lg font-semibold">Spaniel API</h1>
        </div>
        <Link
          to="/docs/openapi-spec"
          className="inline-flex items-center gap-1.5 rounded-md border border-border bg-background px-3 py-2 text-xs font-medium text-foreground transition-colors hover:bg-muted"
        >
          <Braces size={14} /> View OpenAPI spec file
        </Link>
      </header>
      <div
        ref={ref}
        className="mx-auto max-w-[1280px] overflow-hidden rounded-lg border border-border bg-surface"
      />
    </main>
  )
}
