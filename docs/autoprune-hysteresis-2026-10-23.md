# Autoprune recovery hysteresis

The Stonkpile collector was configured with auto_prune=true and a 500 MiB cap.
Live logs showed hard-cap maintenance stepping from 545 MiB down through 500 MiB,
then reopening ingestion at 497.86 MiB despite its 350 MiB reclamation target.
The UI was reporting a real storage-pressure admission pause, not disk exhaustion:
the host still had about 23 GiB free.

The guard used the high-water threshold on every pass rather than retaining an
active low-water cycle. It also discarded its emergency recovery mode as soon
as one phase crossed below the hard cap. That reopened exporters and reduced
maintenance back to its short budget, while a minute-long proactive cooldown
left too little headroom for continuous incoming telemetry. A later WAL-only
checkpoint could independently clear the paused flag.

The policy now latches proactive pruning at 95% until <=70%. If the hard cap is
reached, it separately latches recovery admission and the larger per-phase budget
until the same target is reached. Intermediate below-cap/high-water results and
WAL checkpoints cannot reopen admission early. Proactive phases retain a
five-second read/ingest window rather than a minute per table. Existing bounded
single-table maintenance, oldest-first trace-coherent eviction, reclamation/floor
safety, disabled-autoprune preserve mode, and cap settings remain intact.
Updating the policy resets its latches, so disabling or changing limits remains
an explicit live operator control. A completed cycle logs its measured low-water
size. This changes neither the cap nor the target and does not reset the DB.

Three deterministic regressions failed before the fix: phased recovery at
101 -> 99 -> 94 -> 71 -> 70, proactive pruning at 95 -> 90 -> 80 -> 70, and a
WAL checkpoint during incomplete emergency recovery. They verify admission,
budget selection, completion, idle hysteresis, and switching to preserve mode.
Validation and deployment evidence will be appended after live verification.
