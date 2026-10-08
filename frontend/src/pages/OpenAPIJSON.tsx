import { useQuery } from '@tanstack/react-query'
import { Braces, Copy, ExternalLink, FileCode2, LoaderCircle } from 'lucide-react'
import { Link } from 'react-router-dom'
import { useState } from 'react'
import { JsonCode, YamlCode } from '@/components/ui/HighlightedCode'

type SpecFormat = 'yaml' | 'json'

async function loadOpenAPI(format: SpecFormat) {
  const response = await fetch(`/api/openapi.${format}`)
  if (!response.ok) throw new Error(`The OpenAPI document returned ${response.status}.`)
  return response.text()
}

export default function OpenAPIJSON() {
  const [format, setFormat] = useState<SpecFormat>('yaml')
  const [copied, setCopied] = useState(false)
  const document = useQuery({
    queryKey: ['openapi-document', format],
    queryFn: () => loadOpenAPI(format),
    staleTime: Infinity,
  })
  const copy = async () => {
    if (!document.data) return
    await navigator.clipboard.writeText(document.data)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 2_000)
  }

  return (
    <main className="flex-1 overflow-y-auto bg-[#f1f6f9] text-[#263b4c] dark:bg-background dark:text-foreground">
      <header className="border-b border-[#cbdde8] bg-[#edf5fa] px-5 py-5 sm:px-7 dark:border-border dark:bg-surface">
        <div className="mx-auto flex max-w-[1224px] flex-col justify-between gap-4 sm:flex-row sm:items-end">
          <div>
            <div className="flex items-center gap-2 text-[#426b8c] dark:text-muted-foreground">
              <FileCode2 size={17} />
              <span className="font-mono text-[10px] tracking-[0.14em]">OPENAPI 3.1 CONTRACT</span>
            </div>
            <h1 className="mt-2 text-[22px] font-semibold tracking-[-.035em]">
              OpenAPI spec files
            </h1>
            <p className="mt-1 max-w-2xl text-[12px] leading-relaxed text-[#627789] dark:text-muted-foreground">
              The live, versioned API contract that powers Spaniel’s generated clients and reference
              docs.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Link
              to="/docs/openapi"
              className="inline-flex items-center gap-1.5 rounded-md border border-[#bcd1df] bg-white px-3 py-2 text-xs font-medium text-[#315d7e] shadow-sm transition-colors hover:bg-[#f5faff] dark:border-border dark:bg-background dark:text-foreground dark:hover:bg-muted"
            >
              Redoc reference <ExternalLink size={13} />
            </Link>
            <a
              href={`/api/openapi.${format}`}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1.5 rounded-md border border-[#bcd1df] bg-white px-3 py-2 text-xs font-medium text-[#315d7e] shadow-sm transition-colors hover:bg-[#f5faff] dark:border-border dark:bg-background dark:text-foreground dark:hover:bg-muted"
            >
              Raw {format.toUpperCase()} <ExternalLink size={13} />
            </a>
            <button
              type="button"
              onClick={copy}
              disabled={!document.data}
              className="inline-flex items-center gap-1.5 rounded-md bg-[#315d7e] px-3 py-2 text-xs font-medium text-white shadow-sm transition-colors hover:bg-[#244b69] disabled:cursor-not-allowed disabled:opacity-50"
            >
              <Copy size={13} /> {copied ? 'Copied' : `Copy ${format.toUpperCase()}`}
            </button>
          </div>
        </div>
      </header>
      <section className="mx-auto max-w-[1224px] p-4 sm:p-7">
        <div className="overflow-hidden rounded-lg border border-[#cbdde8] bg-white shadow-[0_12px_32px_-24px_rgba(30,70,100,.45)] dark:border-border dark:bg-surface">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[#dce8ef] bg-[#f8fbfd] px-4 py-3 dark:border-border dark:bg-muted">
            <div className="flex items-center gap-2">
              <span className="grid h-7 w-7 place-items-center rounded bg-[#e5f1f8] text-[#315d7e] dark:bg-background dark:text-foreground">
                <Braces size={15} />
              </span>
              <div>
                <p className="font-mono text-[11px] font-semibold">/api/openapi.{format}</p>
                <p className="text-[10px] text-[#7890a1] dark:text-muted-foreground">
                  Live response · syntax highlighted
                </p>
              </div>
            </div>
            <div className="flex items-center gap-3">
              <div
                role="tablist"
                aria-label="OpenAPI specification format"
                className="flex rounded-md bg-[#e7f0f5] p-0.5 dark:bg-background"
              >
                {(['yaml', 'json'] as const).map((candidate) => (
                  <button
                    key={candidate}
                    type="button"
                    role="tab"
                    aria-selected={format === candidate}
                    onClick={() => setFormat(candidate)}
                    className={`rounded px-2.5 py-1 font-mono text-[10px] font-semibold uppercase transition-colors ${format === candidate ? 'bg-white text-[#315d7e] shadow-sm dark:bg-surface dark:text-foreground' : 'text-[#627789] hover:text-[#315d7e] dark:text-muted-foreground dark:hover:text-foreground'}`}
                  >
                    {candidate}
                  </button>
                ))}
              </div>
              {document.data && (
                <span className="font-mono text-[10px] text-[#7890a1]">
                  {document.data.length.toLocaleString()} bytes
                </span>
              )}
            </div>
          </div>
          {document.isLoading ? (
            <div className="flex min-h-64 items-center justify-center gap-2 text-sm text-muted-foreground">
              <LoaderCircle className="animate-spin" size={16} /> Loading the contract…
            </div>
          ) : document.error ? (
            <div className="p-5 text-sm text-destructive">
              Could not load the OpenAPI document: {document.error.message}
            </div>
          ) : (
            <div className="overflow-auto bg-[#fbfdfe] p-4 dark:bg-[#0c1622] sm:p-5">
              {format === 'yaml' ? (
                <YamlCode value={document.data ?? ''} />
              ) : (
                <JsonCode value={document.data ?? ''} />
              )}
            </div>
          )}
        </div>
      </section>
    </main>
  )
}
