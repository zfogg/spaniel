# Collector retention verification, 2026-10-21

The incident combined storage accounting and retention with long exclusive
maintenance pauses. The session payload estimate did not explain allocation in
ART indexes, deleted tuples, free blocks and WAL. A missed optional reclamation
target could incorrectly stop ingestion below the hard cap. Repeated deletion
without physical progress could erase additional surviving telemetry.

The implementation is described in [storage retention](storage-retention.md).
The hard cap remains 500 MiB. Reclamation precedes coherent oldest-first eviction;
no-progress detection refuses further deletion. Dashboard queries run only while
their dashboard is being viewed. Storage inspection no longer pauses ingestion.

## Qualification

The isolated collector used a consistent copy of Ratio's real database, the
same 500 MiB cap, automatic pruning enabled, four engine workers and a 2 GB
engine memory limit. The fixture was preserved across candidate runs rather
than reset to make a result pass. The final run began with 426,272 stored spans.

- Runtime image: `sha256:5f1aebd132100b50a6600546fa3952c025daf77d57c00ca1919c325fb016b78c` (`spaniel:retention-owned-20261021`).
- Sustained run: 392.36 seconds; concurrent 512-span growth batches,
  dashboard reads and recent traces with events, links, logs and metrics.
- Two oldest-prefix eviction cycles removed 85,931 and 31,034 primary records.
  Measured allocation fell from 428,605,440 to 346,816,512 bytes.
- All 17,728 recent spans / 554 traces, 17,728 events, 17,728 links,
  554 logs and 554 metric points survived exactly. Zero dropped signals and
  `storage_full=false` throughout.
- Maximum dashboard read: 7.203 s; trace export:
  0.219 s; log export: 3.687 s;
  metric export: 1.578 s. Every request stayed below the
  ten-second exporter deadline.
- Abrupt termination with 4,112,015 bytes of committed WAL
  preserved a fresh 32-span witness with all events/links/log/metric records,
  plus all recent qualification traces, on replay. Pruning and the cap persisted.
- All 19 frontend assets exactly matched the existing `827fb21` deployment.

A separate 424,332-span real-database copy retained the hash of all 17 stored
fields, schema/defaults and four indexes through an interrupted eight-second
maintenance pass, abrupt exit and graceful reopen. Mandatory recovery completed
in 8.979 seconds with zero WAL remaining; the optional work returned only
`context deadline exceeded`. This is a soft work-budget timeout, not a claim
that every large table always finishes within eight seconds.

The cancellation regression queues an external reader, cancels inside the
atomic index transaction, and proves the original work connection and all 21
pool leases stay reserved through rollback, index restoration and checkpointing.
It then verifies exact rows/schema/indexes and successful resumed ingestion.

The wide-row copy regression requires both the ordinary physical copy plan and
bounded WAL before checkpointing. Plan names alone are insufficient: an actual
rowid self-join selected ordinary `CREATE_TABLE_AS` but still wrote 591 MB of
logical WAL, so that faster alternative was rejected.

Linux: the complete `internal/storage` and `cmd/spaniel` test packages passed
on the immutable candidate source (117.4 seconds for the build verification
stage). Generated API and query artifacts remained unchanged.

Windows: full `make test GOFLAGS=-p=1` passed, including generated-artifact
verification. The queued-reader cancellation regression also passed natively.


## Local rollout

Source commit `554ab0da0267cec69a941e26a2a35a081b8b3d4e` was pushed to `main`.
The immutable runtime image above contains all 21 storage/CLI source and test
files recorded by the build manifest, with frontend assets from `827fb21`.

Ratio's existing volume was backed up consistently before replacement (714 MB
including the saved inspection). The active session, ports and mounts were
preserved. Its temporary `SPANIEL_AUTO_PRUNE=false` environment override was
replaced with `true`; both environment and API policy remained true with a
500 MiB cap after an additional graceful restart. Allocation fell from about
520 MB to 339 MB while the global span count increased to over 1.19 million.
There were zero dropped spans/logs/metric points and no storage-full condition.
No staging tables or pending index-recovery flags remained.

An independent existing five-span Ratio trace retained exactly the same complete
row-payload digest after rollout. A fresh actual API request also appeared as
new trace telemetry. All 19 served frontend assets retained their exact hashes.
The one-hour observer metrics chart rendered 358 raw points into 60 bins; the
verification browser tab was closed after capture.

Stonkpile was backed up and replaced with the same image. Its original
`auto_prune=false`, 500 MiB cap, active session, mounts and ports were preserved.
It retained 240,515 spans / 39,535 traces / 40,043 logs at verification. All 30
new HTTP exports returned success and their 30 records persisted; maximum export
latency was 0.063 seconds.

The standalone collector's effective image configuration was staged for this
same build. Its existing container remained stopped without recreation/start.
Windows focused retention/recovery/WAL/lease tests passed with the race detector
(storage 49.721 s, CLI 1.262 s); full native `make test` completed with exit 0.

The final 330-second live one-hour metrics probe passed 34 samples. Queries
completed in 20–136 ms; the newest point advanced throughout, remained under
5.455 seconds old, and storage stayed writable with pruning enabled at
500 MiB. No Ratio lifecycle changes occurred during that acceptance window.
