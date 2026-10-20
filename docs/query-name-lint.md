# Storage query identity

`db.storage_name_collision` warns on every span in a trace whose
`<codebase>.storage.<QueryName>` name identifies more than one nonempty
`db.query.text` value. Comparison is exact and trace-local. Identical repeated
templates are allowed; different names and different traces are independent.
Missing query text and names outside the storage namespace are ignored.

Analysis revisits earlier spans when later spans arrive. Every offending span
gets one persisted warning. Re-analysis inserts only missing warnings and uses
one notification identity per trace/rule, avoiding a notification per SQL call.

Prefer generated named-query methods. With raw GORM, attach an explicit
source-owned name to each logical query. Keep SQL verbs, tables and sanitized
templates in attributes. Bulk operations should use fixed templates (for example
JSON recordset parameters) so batch size does not change SQL text.

## Local verification, 2026-10-20

Ingestion regressions cover every offending occurrence, identical-template
controls, cross-trace separation, late arrivals and idempotent persistence.
Buffered sampling, late-batch persistence and concurrent re-analysis are covered.
`make test-ingestion`, `make test-query-name-race` and `make test-storage` passed.

Deployment exposed an existing Ratio DuckDB ART index/checkpoint failure. A
stopped-file backup, including its WAL, was retained. A logical copy rebuilt the
indexes; all 21 base tables retained identical row counts and per-column hash
aggregates, including 308,848 spans, before/after copying and reopening. The copy
was opened independently using its actual `spaniel` catalog name before
installation. Original files remain backed up. The collector uses its bundled
engine's current storage format and drains SQL activity during maintenance.
Retention materializes its coherent trace batch once rather than regrouping the
full span table for every child-table delete. The acquisition deadline no longer
cancels maintenance work midway through a pass.

Local Compose deployments retain their existing volumes and ports. Image
override `bin/query-names-image.yml` follows their prior `bin/sql-fix-*.yml`.

All three containers were healthy with zero restarts on image
`sha256:565d7ef5853f2bf8430c3fae85bf050f78dbb9d88ecab12f80c0a6ce886562d5`.
Live two-batch OTLP probes, read back through each collector's MCP, produced
exactly three collision warnings and left two distinct-name controls clean:

- Spaniel: `http://localhost:8346/traces/e686cf729e3045f4a94d7e25b1797425?view=waterfall`
- Stonkpile: `http://localhost:8345/traces/9797a39073584f38b386da3270c7a67a?view=waterfall`
- Ratio: `http://localhost:8285/traces/c3e4558126bc45eab21d968e64104755?view=waterfall`

Configured retention still expires old telemetry; original files remain backed
up separately from the live collector volume.
