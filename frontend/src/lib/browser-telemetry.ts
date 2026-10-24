import { ROOT_CONTEXT, SpanKind, SpanStatusCode, type Tracer } from '@opentelemetry/api'
import { resourceFromAttributes } from '@opentelemetry/resources'
import { BatchSpanProcessor, TracerProvider } from '@opentelemetry/sdk-trace'
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http'

// Only same-origin API traffic belongs to Spaniel. Exports use the original
// fetch and /v1/traces is excluded, so observing the UI cannot trace its exporter.
export function telemetryFetch(
  original: typeof fetch,
  tracer: Tracer,
  origin: string,
): typeof fetch {
  return async (input, init) => {
    const rawURL = input instanceof Request ? input.url : String(input)
    const url = new URL(rawURL, origin)
    if (url.origin !== origin || !url.pathname.startsWith('/api/')) return original(input, init)

    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    const span = tracer.startSpan(
      `HTTP ${method}`,
      {
        kind: SpanKind.CLIENT,
        attributes: { 'http.request.method': method, 'url.path': url.pathname },
      },
      ROOT_CONTEXT,
    )
    const headers = new Headers(
      init?.headers ?? (input instanceof Request ? input.headers : undefined),
    )
    const context = span.spanContext()
    headers.set(
      'traceparent',
      `00-${context.traceId}-${context.spanId}-${context.traceFlags.toString(16).padStart(2, '0')}`,
    )
    try {
      const response = await original(input, { ...init, headers })
      span.setAttribute('http.response.status_code', response.status)
      if (response.status >= 400) span.setStatus({ code: SpanStatusCode.ERROR })
      return response
    } catch (error) {
      // Do not record URLs, request bodies, credentials, or raw error messages.
      span.setStatus({ code: SpanStatusCode.ERROR, message: 'API request failed' })
      span.setAttribute('error.type', error instanceof Error ? error.name : 'Error')
      throw error
    } finally {
      span.end()
    }
  }
}

export function startBrowserTelemetry() {
  const original = window.fetch.bind(window)
  const provider = new TracerProvider({
    resource: resourceFromAttributes({ 'service.name': 'spaniel-web' }),
    spanProcessors: [
      new BatchSpanProcessor({
        exporter: new OTLPTraceExporter({
          url: new URL('/v1/traces', window.location.origin).href,
          headers: { 'x-spaniel-self-telemetry': 'true' },
        }),
        scheduledDelayMillis: 1000,
        maxExportBatchSize: 64,
      }),
    ],
  })
  window.fetch = telemetryFetch(original, provider.getTracer('spaniel/web'), window.location.origin)
  return () => {
    window.fetch = original
    void provider.shutdown()
  }
}
