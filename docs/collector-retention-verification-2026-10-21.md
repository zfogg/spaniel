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
