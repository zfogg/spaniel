import { expect, test } from '@playwright/test'

test('browser API calls start in spaniel-web and continue in spaniel-api', async ({
  page,
  request,
}) => {
  test.skip(process.env.SPANIEL_TELEMETRY_E2E !== '1', 'Requires a live self-monitoring collector')
  const outgoing = page.waitForRequest((req) => new URL(req.url()).pathname === '/api/settings')
  await page.goto('/')
  const apiRequest = await outgoing
  const traceparent = (await apiRequest.allHeaders()).traceparent
  expect(traceparent).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/)
  const [, traceId, browserSpanId] = traceparent.split('-')

  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/traces/${traceId}`)
        if (!response.ok()) return false
        const { data } = (await response.json()) as {
          data: {
            service_name: string
            span_id: string
            parent_span_id: string
            trace_id: string
          }[]
        }
        const browser = data.find((span) => span.span_id === browserSpanId)
        const api = data.find(
          (span) => span.service_name === 'spaniel-api' && span.parent_span_id === browserSpanId,
        )
        return (
          browser?.service_name === 'spaniel-web' &&
          !browser.parent_span_id &&
          api?.trace_id === traceId
        )
      },
      { timeout: 30_000, intervals: [500, 1000] },
    )
    .toBe(true)
  console.log(`Browser-to-API trace: ${traceId}`)
})
