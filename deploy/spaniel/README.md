# Spaniel's own dashboards

The local Compose service loads `dashboards/*.yaml` from
`/etc/spaniel/dashboards` at startup. Each file contains a stable `id`, so
reloading or recreating the container updates the same dashboard. The YAML
files are authoritative and mounted read-only; edit these files instead of
changing dashboard definitions in the UI.

The three dashboards are:

| ID | Focus |
| --- | --- |
| `spaniel-self-runtime` | Database budget, Go runtime, telemetry freshness, warnings |
| `spaniel-self-api` | HTTP traffic, response classes, route/storage latency, slow spans |
| `spaniel-self-ingestion` | Accepted points, queue pressure, cardinality, logs, WebSocket delivery |

All queries use the active `$session_id` and a `$service` variable defaulting
to `spaniel`. Windowed panels default to 60 minutes; `window_minutes` is
clamped to 1–180. Time series use approximately 24 buckets per window to
stay within the preview API's row limit. Current gauges use the most recent
sample in the active session. Missing data remains empty instead of being
reported as a healthy zero.

Cumulative counters are shown using the latest sample per attribute set.
The accepted-points series computes differences within each counter epoch,
including the sample preceding the selected window. The first sample of an
epoch has no known delta and is omitted. Latency panels use actual span
durations, rather than the placeholder `value` of native histogram rows.
`go.memory.allocated` is cumulative, so memory composition uses the
non-monotonic `go.memory.used` samples instead.

Database utilization uses the reported configured size budget. The other
gauges represent percentages or the observed 0/1 storage-full state; queue
depth and metric cardinality remain unbounded scalar values.

After editing YAML, recreate only the local Spaniel service to reload it:

```sh
docker compose up -d --no-deps --force-recreate spaniel
```

On a running build with definition-reload support, the files can also be
reconciled without a restart using `POST /api/dashboards/reload`. This is
useful on Docker Desktop, where native Windows bind-mount edits may not
produce Linux filesystem-watch events.
