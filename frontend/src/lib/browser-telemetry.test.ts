import { describe, expect, it, vi } from 'vitest'
import { SpanKind, SpanStatusCode } from '@opentelemetry/api'
import { InMemorySpanExporter, SimpleSpanProcessor, TracerProvider } from '@opentelemetry/sdk-trace'
import { resourceFromAttributes } from '@opentelemetry/resources'
import { telemetryFetch } from './browser-telemetry'

function fixture(original: typeof fetch) {
  const exporter = new InMemorySpanExporter()
  const provider = new TracerProvider({
    resource: resourceFromAttributes({ 'service.name': 'spaniel-web' }),
    spanProcessors: [new SimpleSpanProcessor({ exporter })],
  })
  return {
    provider,
    exporter,
    fetch: telemetryFetch(original, provider.getTracer('test'), 'http://localhost'),
  }
}

describe('browser trace propagation', () => {
  it('starts a web root and injects its context while preserving request credentials', async () => {
    const original = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}'))
    const f = fixture(original)
    await f.fetch(
      new Request('http://localhost/api/settings', { headers: { Authorization: 'Bearer test' } }),
    )
    await f.provider.forceFlush()
    const [span] = f.exporter.getFinishedSpans()
    expect(span.resource.attributes['service.name']).toBe('spaniel-web')
    expect(span.kind).toBe(SpanKind.CLIENT)
    expect(span.parentSpanContext).toBeUndefined()
    const headers = new Headers(original.mock.calls[0][1]?.headers)
    expect(headers.get('Authorization')).toBe('Bearer test')
    expect(headers.get('traceparent')).toBe(
      `00-${span.spanContext().traceId}-${span.spanContext().spanId}-01`,
    )
    await f.provider.shutdown()
  })

  it('does not trace exports or propagate context to other origins', async () => {
    const original = vi.fn<typeof fetch>().mockResolvedValue(new Response('{}'))
    const f = fixture(original)
    await f.fetch('/v1/traces', { method: 'POST' })
    await f.fetch('https://example.com/api/settings')
    await f.provider.forceFlush()
    expect(f.exporter.getFinishedSpans()).toHaveLength(0)
    expect(original.mock.calls[1][1]).toBeUndefined()
    await f.provider.shutdown()
  })

  it('ends failed requests without recording sensitive error messages', async () => {
    const original = vi.fn<typeof fetch>().mockRejectedValue(new Error('secret'))
    const f = fixture(original)
    await expect(f.fetch('/api/settings')).rejects.toThrow('secret')
    await f.provider.forceFlush()
    const [span] = f.exporter.getFinishedSpans()
    expect(span.status.code).toBe(SpanStatusCode.ERROR)
    expect(JSON.stringify(span.attributes)).not.toContain('secret')
    await f.provider.shutdown()
  })
})
