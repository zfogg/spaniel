# Metric cardinality growth

Spaniel records the server admission time of each indexed metric series. OTLP
point timestamps may be late or future-dated and are not used for this detector.
The existing per-stream limit bounds the number of admission records. Catalog
entries created before the migration have unknown admission times and contribute
only to the baseline count.

Each session, service, and metric is observed separately. An observation begins
when a stream is evaluated and needs a full hour before a finding can be made.
The two half-hour intervals are `[now-60m, now-30m)` and `[now-30m, now]`.
Growth requires at least 10 admissions in each half, at least 50 in total, and
growth of at least 25% of the count at the beginning of the window. Independently,
the stream is approaching its configured limit at 80% utilization. A zero or
missing limit disables the utilization check. These thresholds apply after the
one-hour observation period so a newly discovered stream is not a surge.

Evaluations run at most once per minute for streams receiving metrics, with a
fixed-size throttle. The cardinality view reports insufficient history until
the observation period is complete. Findings report counts, the hour window,
the limit, and indexed attribute keys, never label values. They link to the
selected stream in Metrics and are deduplicated per observation period and kind.

Deleting a session or resetting telemetry removes its observation state and
admission catalog. Metric retention that removes points invalidates the
affected session's observation state, requiring a fresh hour of evidence.
