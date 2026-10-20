# Gaps in upgraded HTTP connections

An HTTP span with response status 101 can cover the lifetime of a WebSocket or
another upgraded connection. Waiting between messages is connection idle time;
it does not prove missing instrumentation of request processing.

The tracing-gap detector excludes only that outer upgraded HTTP span. It still
evaluates child message and database operations, and ordinary HTTP requests
(including slow 200 responses) retain the same 50 ms uncovered-time threshold.
Both modern `http.response.status_code` and legacy `http.status_code` attributes
are supported. N+1, SQL latency, error and other detectors remain unchanged.

`make test-tracing-gap` checks upgraded connection idle time, real uncovered
child processing time and ordinary request coverage. This correction prevents
new false positives; previously stored findings remain historical evidence.
