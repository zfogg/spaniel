import { useEffect, type DetailedHTMLProps, type HTMLAttributes } from 'react'

declare global {
  namespace JSX { interface IntrinsicElements { 'redoc': DetailedHTMLProps<HTMLAttributes<HTMLElement>, HTMLElement> & { specUrl?: string } } }
}

export default function OpenAPI() {
  useEffect(() => {
    const id = 'spaniel-redoc-runtime'
    if (document.getElementById(id)) return
    const script = document.createElement('script')
    script.id = id
    script.src = 'https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js'
    script.async = true
    document.head.appendChild(script)
  }, [])
  return <main className="flex-1 overflow-y-auto bg-[#f1f6f9] p-4 dark:bg-background"><div className="mx-auto max-w-[1280px] overflow-hidden rounded-lg border border-[#cbdde8] bg-white dark:border-border dark:bg-surface"><redoc specUrl="/api/openapi.json" /></div></main>
}
