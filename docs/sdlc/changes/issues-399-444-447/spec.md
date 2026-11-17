# Specification

Date/source/requestor/status: see [intent](intent.md). See [plan](plan.md).

- #399: deliver a [review proposal](../../../unmanaged-arrow-replace-proposal.md)
  covering deletion, dedicated/shared scope, schema/index ownership, TTLs,
  concurrency and partial outcomes. Keep replace refused before consumption.
- #444: retain root/bucket/object and lease/native permission proofs. Do not
  reapply the rejected cross-statement routing cache. Seek ≥10% lower one-client
  point-read p50 independently in both modes; preserve full controls and tails.
- #445: distinguish foreground stages from overlapping maintenance/admission.
  Seek ≥10% lower managed one-client INSERT/UPDATE/DELETE p50 independently;
  inspect four-writer throughput/tails, sustained backlog and retained bytes.
- #446: inventory the existing bridge and foreign-buffer owners first. Correct
  Go BindStream to retain caller references and pair it with explicit imported
  reader release in the C bridge, including rejection, rebind and nil unbind. No forced
  GC policy change until race/checkptr, CGO, ASAN/UBSAN, retained consumer buffers,
  imported callbacks, cancellation/Close and sustained accounting pass.
- #447: use pinned public DBAPI executemany(sql, None), which executes once with
  no result stream. Only explicitly selected INSERT/UPDATE/DELETE scenario calls
  choose it. Reads, raw controls, SQL generation, counts, timers and full state
  oracles remain with the existing scenario harness. No SQL classification,
  private DBAPI API, additional close or retry. Freeze the same driver binary for
  both caller paths. Proposed gate: ≥10% lower one-client p50 for each mutation
  in each mode, six independent cases, with complete 42-case ABBA matrices.

Finite samples cannot establish regression-free behavior. Correctness checks and
instrumented diagnostics run outside timing windows. New native performance
evidence requires a Linux client, trusted TLS/RESP3 and an exclusively owned
Software fixture with active/effective 4× QPF. Keep operational artifacts private.
