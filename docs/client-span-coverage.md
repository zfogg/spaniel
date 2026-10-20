# Client latency and tracing gaps

OpenTelemetry CLIENT spans measure an outbound request to a remote service.
Their duration includes transport and remote waiting. A remote SERVER span
cannot cover that transport time, and its wall-clock timestamps may differ
from the caller's host clock. Uncovered client duration is therefore not a
measurement of missing application instrumentation.

The tracing-gap detector skips CLIENT parents for every service and protocol.
It still analyzes their SERVER and INTERNAL descendants and still reports
uncovered application work around a client call. The 50 ms application-work
threshold is unchanged, as are semantic linting and N+1 detection.

Regressions reproduce a 369 ms call with 224 ms of remote work, check a remote
host clock offset, and preserve real server/internal gaps. Run
`make test-ingest-admission` for native race coverage of these tests alongside
ingestion cancellation, upgraded HTTP connections and HTTP/DB semantic rules.

The synchronous-I/O detector likewise uses observed timing rather than merely
counting children: overlapping direct DB calls form one phase. A prerequisite
lookup followed by three concurrent reads is two phases and does not fire.
Three serial phases still fire, including three waves of parallel calls. Time
is counted as the union of DB intervals inside the request, so parallel calls
do not inflate the reported wait. Calls from a different service's clock are
not evidence of inline work on this handler. The threshold remains three phases.
