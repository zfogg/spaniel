import { expect, test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'

test.describe.configure({ retries: 0 })

test('named tokens protect the live API, UI, MCP and ingestion', async ({
  page,
  browser,
  request,
}) => {
  test.skip(
    process.env.SPANIEL_E2E_SECURITY !== '1',
    'Requires an isolated live Spaniel server with security initially disabled',
  )
  test.setTimeout(90_000)
  const shots = process.env.SPANIEL_SECURITY_SCREENSHOTS ?? test.info().outputPath('screenshots')
  await mkdir(shots, { recursive: true })
  await page.setViewportSize({ width: 1440, height: 1200 })
  await page.goto('/settings/security')
  await expect(page.getByText('No API tokens yet')).toBeVisible()
  const name = page.getByPlaceholder('e.g. backend, keycloak, temporal')
  await name.fill('Administrator · demo')
  await page.getByRole('button', { name: 'Create token', exact: true }).click()
  const admin = await page.getByLabel('Created API token').inputValue()
  expect(admin).toMatch(/^spn_/)
  await page.getByText('I saved it · dismiss').click()
  await page.getByRole('switch', { name: 'Enable API token security' }).click()
  await expect(page.getByRole('switch', { name: 'Enable API token security' })).toHaveAttribute(
    'aria-checked',
    'true',
  )
  const headers = { Authorization: `Bearer ${admin}` }
  for (const route of ['/api/settings', '/api/security/tokens', '/mcp', '/debug/pprof/']) {
    const response = await request.get(route)
    // pprof is not registered unless debug mode is enabled, but it still must
    // be rejected by the outer authentication boundary before dispatch.
    expect(response.status(), route).toBe(401)
    expect((await response.json()).code).toBe('token_required')
  }
  for (const signal of ['traces', 'metrics', 'logs']) {
    const response = await request.post(
      new URL(`/v1/${signal}`, process.env.SPANIEL_E2E_OTLP_HTTP ?? 'http://127.0.0.1:18318').href,
      { data: {} },
    )
    expect(response.status()).toBe(401)
    const wrong = await request.post(
      new URL(`/v1/${signal}`, process.env.SPANIEL_E2E_OTLP_HTTP ?? 'http://127.0.0.1:18318').href,
      { headers: { Authorization: 'Bearer wrong-example' }, data: {} },
    )
    expect((await wrong.json()).code).toBe('token_invalid')
    const good = await request.post(
      new URL(`/v1/${signal}`, process.env.SPANIEL_E2E_OTLP_HTTP ?? 'http://127.0.0.1:18318').href,
      { headers, data: {} },
    )
    expect(good.status()).toBe(200)
  }
  const context = await browser.newContext({ viewport: { width: 1440, height: 1200 } })
  const locked = await context.newPage()
  await locked.goto(new URL('/settings/security', test.info().project.use.baseURL!).href)
  await expect(locked.getByRole('heading', { name: 'Unlock Spaniel' })).toBeVisible()
  await locked.getByText('Locked out? Recover access').click()
  await locked.screenshot({ path: path.join(shots, 'token-required.png'), fullPage: true })
  await locked.getByLabel('API token', { exact: true }).fill('wrong-example-token')
  await locked.getByRole('button', { name: 'Unlock', exact: true }).click()
  await expect(locked.getByRole('alert').filter({ hasText: 'invalid' })).toBeVisible()
  await locked.screenshot({ path: path.join(shots, 'invalid-token.png'), fullPage: true })
  await locked.getByLabel('API token', { exact: true }).fill(admin)
  await locked.getByRole('button', { name: 'Unlock', exact: true }).click()
  await expect(locked.getByRole('heading', { name: 'Unlock Spaniel' })).toBeHidden()
  await expect(locked.getByText('Administrator · demo', { exact: true })).toBeVisible()
  for (const service of ['Backend · demo', 'Keycloak · demo', 'Temporal · demo']) {
    const response = await request.post('/api/security/tokens', {
      headers,
      data: { name: service, no_expiry: service.includes('Temporal') },
    })
    expect(response.status()).toBe(200)
  }
  await locked.reload()
  await expect(locked.getByText('Temporal · demo', { exact: true })).toBeVisible()
  await locked.screenshot({ path: path.join(shots, 'security-settings.png'), fullPage: true })
  const tokens = (await (await request.get('/api/security/tokens', { headers })).json()).data
  expect(JSON.stringify(tokens)).not.toContain(admin)
  const collector = await request.post('/api/security/tokens', {
    headers,
    data: { name: 'Revocation test' },
  })
  const created = (await collector.json()).data
  expect(
    (await request.delete(`/api/security/tokens/${created.token.id}`, { headers })).status(),
  ).toBe(200)
  const revoked = await request.get('/api/settings', {
    headers: { Authorization: `Bearer ${created.secret}` },
  })
  expect(revoked.status()).toBe(401)
  expect((await revoked.json()).code).toBe('token_revoked')
  expect((await request.put('/api/security', { headers, data: { enabled: false } })).status()).toBe(
    200,
  )
  const replacement = (
    await (
      await request.post('/api/security/tokens', { data: { name: 'Recovery replacement' } })
    ).json()
  ).data.secret
  expect(
    (
      await request.put('/api/security', {
        headers: { Authorization: `Bearer ${replacement}` },
        data: { enabled: true },
      })
    ).status(),
  ).toBe(200)
  expect(
    (
      await request.get('/api/settings', { headers: { Authorization: `Bearer ${replacement}` } })
    ).status(),
  ).toBe(200)
  // The isolated collector is disposable; leave security off for follow-up
  // local inspection without publishing any still-required credential.
  await request.put('/api/security', { headers, data: { enabled: false } })
  await context.close()
})
