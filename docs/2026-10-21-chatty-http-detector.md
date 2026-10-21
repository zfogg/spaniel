# Outbound HTTP detector correction

Stonkpile trace `a792be73ceed0f7796b76f193f1d68ad` reported 60 HTTP calls to
`127.0.0.1`. These were PostgreSQL client spans: the HTTP detector used the
shared semantic-convention `server.address` attribute without checking protocol
or span kind. Commit `27046db791ecdb565f784f45c8717e4f700be93d` counts only
outbound HTTP client spans. Real broker chattiness remains visible.

The focused Makefile-driven Windows/UCRT64 `TestChattyHTTP` suite passed.
Regression coverage excludes 60 SQL clients, 60 Redis clients and 60 incoming
HTTP requests, then confirms five actual outbound requests produce count five.
No model, OpenAPI or frontend inputs changed.

Pi deployed image `stonkpile-spaniel:27046db`, image/index digest
`sha256:dc7fb6ab0e195627dd0119a15803e14b4a7c1d0c24dc91154664a43df150f127`,
started at `2026-10-21T18:09:47Z`, with zero restarts. MCP reports version
`27046db`, active session `9f295f2a-3909-4de2-948e-70519fa06453`, healthy
ingestion, no storage-full state and zero telemetry drops. The original trace
retains all 94 spans. New trace `b03a9584564a11efd8e37fe9561f2dbf` reports only
the real Alpaca host for HTTP chattiness (21 calls), with no database-host issue.

The original volume/mounts, port bindings, command and resource configuration
were compared before/after and preserved. The image selection is persisted in
the Pi's existing `.env`; its private backup and container metadata are at
`/home/alarm/.stonkpile/spaniel-detector-27046db.YP2yUk`. No uncommitted
ingestion-batching or frontend sources were included in this image.
