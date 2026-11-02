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

## Reproducing a deployment

Build from a committed source archive so unrelated working-tree changes cannot
enter the image. The initial diagnostics rollout used source
`1325fa3391c0f693ea614eb13fc6ad6ccd2ff982` and local image
`spaniel:query-diagnostics-1325fa3` (Linux AMD64 image digest
`sha256:ef849a7f535f04e6aa24a407eb3fa95a7aaf0073dde0633d8c15ac7a9426e0cb`).
Its Go dependencies pin Gotel commit `d74c4536472d189e52a634341d061d1b16fc236c`
and Sequel commit `77a21f8c8dee0672609b5a9be3a0958e1871fe07`.
The final Gotel `go/v0.13.0` release points to that exact commit. The runtime
image's immutable Go pseudo-version therefore contains the same release code;
current Spaniel source uses the final `v0.13.0` module tag. Keep this shared
image/source pair when reproducing the rollout rather than rebuilding from an
uncommitted checkout.

For any selected full Spaniel commit, run these commands in Bash from this
repository; use the resulting image name again for subsequent Compose commands:

```sh
revision=1325fa3391c0f693ea614eb13fc6ad6ccd2ff982
image="spaniel:query-diagnostics-${revision:0:7}"
context="$(mktemp -d)"
git archive "$revision" | tar -xf - -C "$context"
docker build --pull --build-arg VERSION="$revision" -t "$image" "$context"
SPANIEL_IMAGE="$image" docker compose up -d --no-deps --force-recreate spaniel
```

On Pi5, build that same archive natively to produce Linux ARM64, then run the
Compose command from the Stonkpile checkout. Its image override is also
`SPANIEL_IMAGE`; Ratio uses `RATIO_SPANIEL_IMAGE`. Recreate only Spaniel and retain
the existing data volume, ports, environment and bind mounts. An AMD64 image is
not an ARM64 deployment artifact.

The default image runs with `HOME=/data` and persists its generated encryption
password in `/data/.spaniel/config.yaml`, alongside the retained telemetry and
security databases. Preserve that config with the volume. Deployments that
override the home/config location should retain their existing private
`SPANIEL_ENCRYPTION_PASSWORD` setting. An existing encrypted security database
requires its original password; startup fails if it cannot be unlocked.
