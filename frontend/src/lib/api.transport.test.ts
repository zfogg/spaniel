import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const apiSource = readFileSync(fileURLToPath(new URL('./api.ts', import.meta.url)), 'utf8')

describe('generated API transport boundary', () => {
  it('does not bypass generated operation typing', () => {
    expect(apiSource).not.toContain('as never')
    expect(apiSource).not.toMatch(/openapiClient\.(GET|POST|PUT|PATCH|DELETE)\(path/)
  })

  it('limits raw fetch to non-JSON configuration and bearer-cookie seeding', () => {
    expect(apiSource.match(/\bfetch\(/g) ?? []).toHaveLength(5)
    expect(apiSource).toContain('/api/dashboards/${id}/config')
    expect(apiSource).toContain('/api/alerts/${id}/config')
    expect(apiSource).toContain("fetch('/api/health'")
  })
})
