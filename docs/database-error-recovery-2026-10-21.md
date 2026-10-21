# Pi5 database error and scrolling recovery, 2026-10-21

The trace page rendered an enormous DuckDB invalidation chain verbatim inside a
centered, non-scrollable flex child. Both the headline and Retry could leave the
viewport. MCP also returned the same recursively wrapped backend diagnostic.

Source `ae9b9ab` collapses repeated DuckDB invalidation wrappers into one
unavailable-database message and its innermost cause. Other external diagnostics
are bounded to 4096 bytes on HTTP/MCP and 4096 code units in the UI. The shared
error panel has a constrained scroll container, wrapping for long tokens, and
accessible heading/Retry. The trace page permits the flex child to shrink.
No stored telemetry or internal exception details are rewritten by this formatter.

## Recovery and deployment

Pi5 was still running `ghcr.io/zfogg/spaniel:navbar-74deabb`; the compose image
override also pointed at an older `65b6769`. This predates the retention and
exclusive-maintenance repair in `554ab0d`. The invalidated database was stopped,
then the complete existing `/data` was copied consistently to the workstation:

- `.local/pi-spaniel-before-error-fix.tar`, 527451136 bytes.
- SHA256 `dff0b1a1d6b00e4ae6301e74ce0fa3671a99dce279e7e7f9f66b6cad11bbfc3f`.

Restarting the same volume restored queries before upgrading. This confirms
reopening cleared the invalidated engine state; it does not establish which
original engine operation triggered the fatal error. The image update includes
the existing tested retention/maintenance repair as well as this display fix.

An immutable archive of the committed source built the ARM64 image on the
workstation, using a native cross compiler. No unrelated dirty UI/ingestion work
entered the image. The Pi had less than 700 MB free, so neither the source build
nor the database backup was placed there. Temporary image-transfer files were
removed afterward; the workstation database backup remains.

Installed `stonkpile-spaniel:ae9b9ab` image digest:
`sha256:c8c81a1fd10fda5c4b38a86e898d08c72bfa6dde98e7ece715d35b1a727462eb`.
Container started at 2026-10-21 16:26:20 UTC (12:26:20 EDT). Compose's image
override now persists this tag; the previous env/container metadata is preserved
privately at `~/.stonkpile/spaniel-error-deploy-20261021.J5VoDj`.
Only the collector was recreated. Its volume, ports, 4-CPU configuration, PID
limit128, 500 MiB cap and enabled automatic pruning remain. Trading services
were not restarted by this rollout.

## Verification

- MCP reports `version: ae9b9ab`; container healthy with zero restarts.
- Original trace `92c4bece51fa4eacb93edd67df0d0591` still has all257 spans.
- Fresh worker/server telemetry arrives; ingestion healthy, storage_full=false,
  zero dropped spans/logs/metric points in the new collector. Allocation ~308 MiB.
- Existing in-app trace tab was reloaded and its waterfall rendered successfully.
- Nine ErrorState unit regressions pass, including10000 nested wrappers and
  a huge ordinary error. Two production-bundle Playwright tests pass at560x460,
  including actual scrolling to Retry on a long multiline error.
- Format/check, ESLint and production bundle pass. Focused Go formatter/API
  regressions pass. Full make test first collided with a rebuilding frontend;
  after rebuilding it progressed past that check but stopped on the unchanged
  `TestForwardSamplesAtRate` one-second timing condition. No test was weakened.
- Pi deployment ownership was released to the batching chat after verification.
