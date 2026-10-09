import type { Log } from './api'

// Log records do not have a persisted ID. Timestamp, span, and body alone
// collide for concurrent Spaniel access logs, which makes React reuse a Motion
// row for a different record. Keep every record identity field in the key so
// arrival animations and the selected inspector stay attached to the right row.
export function logKey(log: Log): string {
  return [
    log.session_id,
    log.timestamp_ns,
    log.received_at,
    log.trace_id,
    log.span_id,
    log.severity,
    log.service_name,
    log.body,
    log.attributes,
  ].join(':')
}
