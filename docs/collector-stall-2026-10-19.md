# Collector ingestion stall, 2026-10-19

Pi5's Spaniel container at `ac6291334964` reported HTTP health success while
BeaglePlay's OTLP traces, metrics, and logs repeatedly timed out. Empty OTLP/HTTP
requests also timed out from Pi5 itself. This isolated the failure to ingestion,
not BeaglePlay transport or exporter configuration.

A SIGQUIT stack dump captured before recovery showed retention's
`deleteOldestTelemetryBatch` transaction stuck in DuckDB's native commit for
243 minutes. Retention held `Batcher.Maintenance`; appenders and flushes waited
on that mutex. Concurrent metadata/read queries were stuck in native prepare.
The 64 ingestion slots filled, and thousands of canceled exporter retries
remained blocked in `beginIngest` because admission ignored request cancellation.
The dump was saved on Pi5 as `/tmp/spaniel-stall-stack.log`. Restart recovered
OTLP ingestion using the existing database volume; no telemetry database was
reset. In-memory buffered records at termination cannot be guaranteed retained.

## Repair

Maintenance now closes appenders and drains/reserves the entire bounded SQL
pool before running retention or checkpoint work on one of those connections.
This excludes concurrent native SQL prepares and transaction commits throughout
maintenance. Other SQL callers queue in `database/sql`, which honors their
contexts. The pool reservation and maintenance SQL have a 30-second context;
failure releases every acquired connection and the batcher reopens appenders.
Periodic WAL checkpoints use this same isolation boundary. The specific native
DuckDB lock cycle is not proven by Go stacks alone; exclusion removes the
observed overlap without changing database format or weakening retention.

Admission now selects between an available slot and caller cancellation and
balances pending-request accounting on both paths. Exporters can retry without
retaining canceled requests indefinitely during a slow maintenance pass.

Regression coverage checks cancellation of queued trace/log/metric requests,
SQL exclusion while maintenance owns the pool, and resumed ingestion afterward.
Existing file-backed concurrent checkpoint/retention tests verify persistence
and native appender reuse. HTTP health remains a process-liveness check; an OTLP
round trip and recent received telemetry are required for operational verification.
