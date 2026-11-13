# Unmanaged direct Arrow projection

Requested November 13, 2026, by Albert Wong through parallel performance work.
Baseline: merged source `ba855f7995b2685182ed1331bcf6703d73089c4a`.

Reduce unmanaged simple-read conversion cost without changing live application
HASH ownership or Search retrieval. The full merged benchmark observed a 5.962s
one-reader wide scan and 449 MiB highest-worker RSS. Those observations motivate
measurement; they do not isolate conversion as the complete bottleneck.

See [contract](spec.md), [plan](plan.md) and [validation](validation.md).
