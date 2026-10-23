# Alert lifecycle

Condition alerts describe ongoing problems. Acknowledging a pending/firing
instance moves it out of Needs attention, stops repeat delivery, and leaves its
underlying state intact. A false evaluation resolves it; the next true episode
clears acknowledgement and can notify again. Acknowledged active instances are
available in the Acknowledged filter. Recovery notifications are still delivered.

For discrete failures use `condition: {kind: event}`. Return a nonempty string
`event_id` (maximum 512 bytes) per match, plus any `group_by` columns. Identity is
rule ID + group labels + event ID. Duplicate rows and repeated evaluations fire
only once. Each new event creates a separate persisted instance and may notify
without waiting for older events to leave the query window. Pending, repeat,
threshold and instance-discovery settings are not supported for event rules.

When converting an acknowledged aggregate rule, an optional numeric
`event_time_ns` column carries acknowledgement forward for events at or before
the old acknowledgement time. Later events still fire.

Acknowledgement survives process restart. Missing events resolve, but replaying
the same ID does not rearm them. Instances are the durable deduplication ledger:
they remain until their rule is deleted. Changing SQL preserves identities;
changing a rule ID or event identity intentionally starts a new identity. Queries
have the existing 1,000-row limit: use a bounded lookback and a deterministic
ORDER BY. Use a trace ID for errored traces rather than a changing aggregate
count; multiple error spans in a trace then count as one failure.

Example:

```yaml
version: 1
id: errored-traces
name: Errored traces
query: >-
  SELECT DISTINCT service_name, trace_id AS event_id
  FROM spans WHERE status_code=2
  AND end_ns >= epoch_ns(current_timestamp)-300000000000
  ORDER BY service_name, event_id
condition:
  kind: event
group_by: [service_name]
pending_for: 0s
cooldown: 0s
repeat_interval: 0s
severity: warning
```
