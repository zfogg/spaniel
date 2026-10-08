import { useEffect, useRef } from 'react'
import { useTheme } from 'next-themes'
import { Braces } from 'lucide-react'
import { Link } from 'react-router-dom'

declare global {
  interface Window {
    Redoc?: {
      init: (
        document: string | object,
        options: object,
        target: HTMLElement,
        callback?: () => void,
      ) => void
    }
  }
}

type OpenAPISchema = {
  description?: unknown
  properties?: Record<string, OpenAPISchema>
  items?: OpenAPISchema
  allOf?: OpenAPISchema[]
  anyOf?: OpenAPISchema[]
  oneOf?: OpenAPISchema[]
}

type OpenAPIOperation = {
  operationId?: unknown
  parameters?: Array<Record<string, unknown>>
  requestBody?: { content?: Record<string, { schema?: OpenAPISchema }> }
}

type OpenAPIDocument = {
  paths?: Record<string, Record<string, OpenAPIOperation>>
}

function makeParameterDescriptionsVisible(document: OpenAPIDocument) {
  // ReDoc currently renders a parameter description for an object but omits it
  // for scalar query and path parameters. Mirroring the canonical description
  // onto the schema keeps every filter and resource identifier discoverable.
  for (const path of Object.values(document.paths ?? {})) {
    for (const operation of Object.values(path)) {
      for (const parameter of operation?.parameters ?? []) {
        if (
          (parameter.in !== 'query' && parameter.in !== 'path') ||
          typeof parameter.description !== 'string'
        )
          continue
        const schema = parameter.schema
        if (!schema || typeof schema !== 'object' || Array.isArray(schema)) continue
        const schemaObject = schema as Record<string, unknown>
        if (!('description' in schemaObject)) schemaObject.description = parameter.description
      }
    }
  }
  return document
}

function schemaDescriptions(
  schema: OpenAPISchema | undefined,
  descriptions = new Map<string, string>(),
) {
  if (!schema) return descriptions
  for (const [name, property] of Object.entries(schema.properties ?? {})) {
    if (typeof property.description === 'string') descriptions.set(name, property.description)
    schemaDescriptions(property, descriptions)
  }
  schemaDescriptions(schema.items, descriptions)
  for (const branch of [
    ...(schema.allOf ?? []),
    ...(schema.anyOf ?? []),
    ...(schema.oneOf ?? []),
  ]) {
    schemaDescriptions(branch, descriptions)
  }
  return descriptions
}

function appendDescriptions(
  section: HTMLElement,
  heading: string,
  descriptions: Map<string, string>,
) {
  const title = [...section.querySelectorAll('h5')].find(
    (element) => element.textContent?.trim().toLowerCase() === heading,
  )
  const table = title?.nextElementSibling
  if (!table || table.tagName !== 'TABLE') return
  for (const row of table.querySelectorAll('tbody > tr')) {
    const field = row.querySelector<HTMLElement>('td[kind="field"]')
    const description = field?.getAttribute('title')
      ? descriptions.get(field.getAttribute('title')!)
      : undefined
    const details = row.querySelector('td:nth-child(2)')
    if (
      !description ||
      !details ||
      details.textContent?.includes(description) ||
      details.querySelector('.spaniel-openapi-field-description')
    )
      continue
    const copy = document.createElement('p')
    copy.className = 'spaniel-openapi-field-description'
    copy.textContent = description
    details.appendChild(copy)
  }
}

function makeSchemaDescriptionsVisible(target: HTMLElement, document: OpenAPIDocument) {
  const operations = new Map<string, { path: Map<string, string>; body: Map<string, string> }>()
  for (const path of Object.values(document.paths ?? {})) {
    for (const operation of Object.values(path)) {
      if (typeof operation?.operationId !== 'string') continue
      const pathDescriptions = new Map<string, string>()
      for (const parameter of operation.parameters ?? []) {
        if (
          parameter.in === 'path' &&
          typeof parameter.name === 'string' &&
          typeof parameter.description === 'string'
        ) {
          pathDescriptions.set(parameter.name, parameter.description)
        }
      }
      const bodyDescriptions = new Map<string, string>()
      for (const media of Object.values(operation.requestBody?.content ?? {})) {
        schemaDescriptions(media.schema, bodyDescriptions)
      }
      operations.set(operation.operationId, { path: pathDescriptions, body: bodyDescriptions })
    }
  }
  for (const section of target.querySelectorAll<HTMLElement>('[data-section-id^="operation/"]')) {
    if (section.id !== section.dataset.sectionId) continue
    const operation = operations.get(section.dataset.sectionId!.slice('operation/'.length))
    if (!operation) continue
    appendDescriptions(section, 'path parameters', operation.path)
    appendDescriptions(section, 'request body schema: application/json', operation.body)
  }
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
        success: '#14532d',
      }
    : {
        bg: '#f5f9fc',
        side: '#edf3f7',
        edge: '#cbdde8',
        ink: '#1f2937',
        muted: '#54616e',
        accent: '#176d9c',
        success: '#96dfae',
      }
  return {
    theme: {
      colors: {
        primary: { main: c.accent },
        text: { primary: c.ink, secondary: c.muted },
        border: { dark: c.edge, light: c.edge },
        responses: { success: { color: c.ink, backgroundColor: c.success } },
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
  const success = dark ? '#14532d' : '#96dfae'
  const additionalProperty = dark ? '#a8d8f0' : '#0f4c78'
  const verbBadges = dark
    ? ''
    : `
    .redoc-wrap .http-verb.get{background:#96dfae!important;color:#102318!important}
    .redoc-wrap .http-verb.post{background:#bfdbfe!important;color:#172554!important}
    .redoc-wrap .http-verb.put{background:#ddd6fe!important;color:#312e81!important}
    .redoc-wrap .http-verb.patch{background:#fde68a!important;color:#713f12!important}
    .redoc-wrap .http-verb.delete{background:#fecaca!important;color:#7f1d1d!important}
    .redoc-wrap .http-verb.head,.redoc-wrap .http-verb.options{background:#dbe8f1!important;color:#1f2937!important}
  `
  const endpointDropdown = dark
    ? `
    .redoc-wrap button:has(.http-verb)+div[aria-hidden],
    .redoc-wrap button:has(.http-verb)+div[aria-hidden]>div,
    .redoc-wrap button:has(.http-verb)+div[aria-hidden]>div>div{background:#1b2d3d!important;color:#edf5fb!important}
    .redoc-wrap button:has(.http-verb)+div[aria-hidden] :is(p,span,div){color:#edf5fb!important}
    .redoc-wrap button:has(.http-verb)+div[aria-hidden] input{background:#1b2d3d!important;border-color:#36536d!important;color:#edf5fb!important}
    .redoc-wrap button:has(.http-verb)+div[aria-hidden] input::placeholder{color:#a8bdcc!important}
    .redoc-wrap button:has(.http-verb) :is(svg,svg polygon){fill:#edf5fb!important}
  `
    : ''
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
    .redoc-wrap button.sc-kzqdkY:not(.kokIwB){background:${success}!important;color:${dark ? '#effcf3' : '#102318'}!important}
    .redoc-wrap span.sc-Nxspf{color:${additionalProperty}!important}
    .redoc-wrap .spaniel-openapi-field-description{margin:6px 0 0;font-size:12px;line-height:1.45;color:${ink}!important}
    ${verbBadges}
    ${endpointDropdown}
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
      window.Redoc.init(document, options(resolvedTheme === 'dark'), target, () =>
        makeSchemaDescriptionsVisible(target, document),
      )
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
