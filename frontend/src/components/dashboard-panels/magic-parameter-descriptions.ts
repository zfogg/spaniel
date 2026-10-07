const descriptions: Record<string, string> = {
  session_id:
    'Built-in: the active ingestion session ID, resolved by Spaniel each time the query runs.',
  service:
    'Service name to compare with spans.service_name (also available on logs and metrics). Define its value in Reusable parameters; it is not filled automatically.',
  operation:
    'Span operation name to compare with spans.name. Define its value in Reusable parameters; it is not filled automatically.',
  status_code:
    'OpenTelemetry span status: 0 = unset, 1 = OK, 2 = error. Not an HTTP response status. Define its value in Reusable parameters.',
  severity:
    'OpenTelemetry severity number: 1–4 TRACE, 5–8 DEBUG, 9–12 INFO, 13–16 WARN, 17–20 ERROR, 21–24 FATAL; 0 is unspecified. Define its value in Reusable parameters.',
}

export function parameterDescription(name: string) {
  return descriptions[name]
}
