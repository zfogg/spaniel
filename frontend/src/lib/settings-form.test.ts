import { describe, it, expect } from 'vitest'
import { SettingsFormSchema, pickFormValues } from './settings-form'
import type { Settings } from './api'

const valid = {
  port: 8080, db_path: '/db', retention_days: 7, max_sessions: 50, max_db_size_mb: 500,
  otlp_grpc_port: 4317, otlp_http_port: 4318, no_browser: false, forward: ['http://tempo:4318'],
  bind_address_v4: '127.0.0.1', bind_address_v6: '::1', forward_sample: 1, source_rps: 0,
  source_burst: 0, self_monitor: true,
}

describe('SettingsFormSchema', () => {
  it('accepts a valid settings form', () => {
    expect(SettingsFormSchema.safeParse(valid).success).toBe(true)
  })

  it('allows empty bind addresses (family disabled) and 0 ports for OTLP receivers', () => {
    expect(SettingsFormSchema.safeParse({ ...valid, bind_address_v4: '', bind_address_v6: '' }).success).toBe(true)
    expect(SettingsFormSchema.safeParse({ ...valid, otlp_grpc_port: 0, otlp_http_port: 0 }).success).toBe(true)
  })

  it('rejects an out-of-range UI port with a helpful message', () => {
    const r = SettingsFormSchema.safeParse({ ...valid, port: 0 })
    expect(r.success).toBe(false)
    if (!r.success) {
      const portIssue = r.error.issues.find(i => i.path[0] === 'port')
      expect(portIssue?.message).toMatch(/Port must be 1/)
    }
  })

  it('rejects forward_sample outside 0–1', () => {
    expect(SettingsFormSchema.safeParse({ ...valid, forward_sample: 2 }).success).toBe(false)
    expect(SettingsFormSchema.safeParse({ ...valid, forward_sample: -0.5 }).success).toBe(false)
  })

  it('rejects a non-URL forward endpoint', () => {
    expect(SettingsFormSchema.safeParse({ ...valid, forward: ['not a url'] }).success).toBe(false)
  })

  it('rejects a malformed IPv4 bind address', () => {
    expect(SettingsFormSchema.safeParse({ ...valid, bind_address_v4: '999.1.1.1' }).success).toBe(false)
  })

  it('rejects negative storage sizes', () => {
    expect(SettingsFormSchema.safeParse({ ...valid, max_db_size_mb: -5 }).success).toBe(false)
  })
})

describe('pickFormValues', () => {
  it('projects the editable subset and defaults missing host strings to empty', () => {
    const s = {
      ...valid,
      tls_enabled: false, bearer_token_set: false,
      bind_address_v4: undefined, bind_address_v6: undefined, forward: undefined,
      runtime: {} as Settings['runtime'],
    } as unknown as Settings
    const v = pickFormValues(s)
    expect(v.bind_address_v4).toBe('')
    expect(v.bind_address_v6).toBe('')
    expect(v.forward).toEqual([])
    expect(v.port).toBe(8080)
    expect('runtime' in v).toBe(false)
  })
})
