import type { Settings } from '@/lib/api'

export function OTLPEndpoints({ settings }: { settings: Settings }) {
  let host = window.location.hostname
  try {
    if (settings.public_url) host = new URL(settings.public_url).hostname || host
  } catch {
    // Settings updates are optimistic; rejected URLs must not break the page.
  }
  const scheme = settings.tls_enabled ? 'https' : 'http'
  const receivers = [
    { label: 'HTTP/protobuf', protocol: 'http/protobuf', port: settings.runtime.otlp_http_port },
    { label: 'gRPC', protocol: 'grpc', port: settings.runtime.otlp_grpc_port },
  ]

  return (
    <div className="flex w-full flex-col gap-3">
      {receivers.map(({ label, protocol, port }) => (
        <div key={protocol} className="min-w-0">
          <div className="mb-1 font-mono text-xs text-muted-foreground">
            OTLP {label}
            {port === 0 ? ' · disabled' : ''}
          </div>
          {port > 0 && (
            <pre className="m-0 overflow-x-auto rounded-lg border border-border bg-card px-4 py-3 font-mono text-xs leading-relaxed text-foreground">
              {`export OTEL_EXPORTER_OTLP_ENDPOINT=${scheme}://${host}:${port}\nexport OTEL_EXPORTER_OTLP_PROTOCOL=${protocol}`}
            </pre>
          )}
        </div>
      ))}
      {settings.bearer_token_set && (
        <p className="m-0 text-xs text-muted-foreground">
          Authentication is enabled. Set OTEL_EXPORTER_OTLP_HEADERS to Authorization=Bearer
          YOUR_TOKEN.
        </p>
      )}
    </div>
  )
}
