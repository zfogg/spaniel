import { useEffect, useRef } from 'react'

export default function OpenAPI() {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const id = 'spaniel-redoc-runtime'
    if (!document.getElementById(id)) {
      const script = document.createElement('script')
      script.id = id
      script.src = 'https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js'
      script.async = true
      document.head.appendChild(script)
    }

    const container = containerRef.current
    if (!container) return
    const redoc = document.createElement('redoc')
    redoc.setAttribute('spec-url', '/api/openapi.json')
    container.replaceChildren(redoc)

    return () => container.replaceChildren()
  }, [])
  return (
    <main className="flex-1 overflow-y-auto bg-[#f1f6f9] p-4 dark:bg-background">
      <div
        ref={containerRef}
        className="mx-auto max-w-[1280px] overflow-hidden rounded-lg border border-[#cbdde8] bg-white dark:border-border dark:bg-surface"
      />
    </main>
  )
}
