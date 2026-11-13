# Plan

See [intent](intent.md) and [spec](spec.md).

1. Inspect pinned parser/C Data/C Stream implementation and existing APIs.
2. Implement isolated fixtures plus complete result/ownership checks and runner.
3. Run untimed race/semantic checks, then obtain the exclusive measurement slot.
4. Retain/audit all 84 requested microbenchmark samples, separating metadata
   contracts, allocator domains and realistic memory/lifetime limitations.
5. Review evidence and commit the harness/results for parent publication.

Steps 1–5 complete. Correctness checks pass. Initial Arrow ownership checks caught an
ExportRecordReader comment/Retain mismatch; the corrected full ownership chain
passes checked-allocator and counted-reference race tests. The pinned go-redis
parser fixture validates all values/replies plus oversized-payload alignment.
All 84 timing samples are preserved in the sanitized results, with three samples per case.
See [validation](validation.md) and the linked evidence report.
