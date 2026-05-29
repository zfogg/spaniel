import { z } from 'zod'
import { SettingsSchema, type Settings } from './api'

// Validation for the editable settings fields. Built by reusing the response
// SettingsSchema (#78) and tightening the editable subset with the same ranges
// the inputs and backend enforce. Validation is advisory: the UI shows inline
// errors, but the server stays authoritative (values are still sent, so a
// backend rejection — e.g. a port clash — still surfaces).

const isUrl = (v: string): boolean => {
  try { new URL(v); return true } catch { return false }
}
// Lenient host checks — empty disables the family; otherwise a plausible shape.
const isV4 = (v: string): boolean =>
  v === '' || (/^(\d{1,3})(\.\d{1,3}){3}$/.test(v) && v.split('.').every(o => Number(o) <= 255))
const isV6 = (v: string): boolean =>
  v === '' || (v.includes(':') && /^[0-9a-fA-F:]+$/.test(v))

export const SettingsFormSchema = SettingsSchema.pick({
  port: true,
  db_path: true,
  retention_days: true,
  max_sessions: true,
  max_db_size_mb: true,
  otlp_grpc_port: true,
  otlp_http_port: true,
  no_browser: true,
  forward: true,
  bind_address_v4: true,
  bind_address_v6: true,
  forward_sample: true,
  source_rps: true,
  source_burst: true,
  self_monitor: true,
}).extend({
  port: z.number().int().min(1, 'Port must be 1–65535').max(65535, 'Port must be 1–65535'),
  otlp_grpc_port: z.number().int().min(0).max(65535, 'Port must be 0–65535'),
  otlp_http_port: z.number().int().min(0).max(65535, 'Port must be 0–65535'),
  retention_days: z.number().int().min(0, 'Must be ≥ 0').max(3650, 'Max 3650 days'),
  max_sessions: z.number().int().min(0, 'Must be ≥ 0').max(10000, 'Max 10000'),
  max_db_size_mb: z.number().int().min(0, 'Must be ≥ 0').max(102400, 'Max 102400 MB'),
  forward_sample: z.number().min(0, 'Must be between 0 and 1').max(1, 'Must be between 0 and 1'),
  source_rps: z.number().min(0, 'Must be ≥ 0').max(1_000_000, 'Too large'),
  source_burst: z.number().int().min(0, 'Must be ≥ 0').max(1_000_000, 'Too large'),
  db_path: z.string().min(1, 'Required'),
  forward: z.array(z.string().refine(isUrl, 'Each endpoint must be a valid URL')),
  bind_address_v4: z.string().refine(isV4, 'Invalid IPv4 address'),
  bind_address_v6: z.string().refine(isV6, 'Invalid IPv6 address'),
})

export type SettingsFormValues = z.infer<typeof SettingsFormSchema>

// Project a server Settings object onto just the editable form fields.
export function pickFormValues(s: Settings): SettingsFormValues {
  return {
    port: s.port,
    db_path: s.db_path,
    retention_days: s.retention_days,
    max_sessions: s.max_sessions,
    max_db_size_mb: s.max_db_size_mb,
    otlp_grpc_port: s.otlp_grpc_port,
    otlp_http_port: s.otlp_http_port,
    no_browser: s.no_browser,
    forward: s.forward ?? [],
    bind_address_v4: s.bind_address_v4 ?? '',
    bind_address_v6: s.bind_address_v6 ?? '',
    forward_sample: s.forward_sample,
    source_rps: s.source_rps,
    source_burst: s.source_burst,
    self_monitor: s.self_monitor,
  }
}
