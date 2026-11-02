# Database query diagnostics

Spaniel stores `db.query.diagnostics` as an array of structured Sequel diagnostic
objects in database span attributes. Clean queries, unavailable SQL and non-SQL
database spans have `[]`. These checks cover lexical issues and delimiters, not
database grammar or whether a query will execute successfully.

Gotel's Go and Python SDKs encode each diagnostic as a JSON string in an OTLP
string array. Spaniel validates and converts those strings into objects before
persisting the span. Native structured OTLP arrays are also accepted. A valid
incoming array, including `[]`, is authoritative: its ranges describe original
SQL, which may differ from the sanitized template or may be omitted entirely.
Offsets are UTF-8 bytes with exclusive ends; lines and Unicode scalar columns
are one-based. Do not apply these ranges to a formatted or sanitized template.

Unknown codes, malformed objects and invalid ranges are rejected. Incoming
message, explanation and hint fields are replaced with trusted descriptions to
prevent SQL literals, bind values or injected prose from entering diagnostics.
At most 64 diagnostics with 16 KiB per object are accepted.

When instrumented diagnostics are missing or invalid, Spaniel analyzes available
`db.query.text`, falling back to legacy `db.statement`. It selects PostgreSQL,
MySQL, SQLite, DuckDB or ClickHouse rules from `db.system.name` or `db.system`,
with generic SQL rules for other recognized SQL databases. Redis and other
non-SQL databases are never analyzed as SQL. Query templates marked
`gotel.db.query.truncated: true` are not analyzed because truncation can introduce
false delimiter errors. Valid original diagnostics remain available on those
spans.

REST span and trace responses retain the existing JSON-encoded `attributes`
field; decoding it reveals the structured array. The span inspector displays
these objects in its attributes view, with SQL templates in a separate
highlighted block below the JSON tree. MCP `get_span` and `get_trace` expose the
diagnostics as an actual array, and OTLP trace export preserves nested objects.

Run `make test-query-diagnostics` for normalization, persistence, REST, MCP and
real DuckDB checks. The deliberately malformed query only runs in a temporary
test database. To export the bounded probe to a local collector, set
`SPANIEL_DIAGNOSTICS_PROBE_ENDPOINT=127.0.0.1:4317` before running that target.
