# Validation and decision

November 13, 2026. Proposed adoption passes the original quantitative gates;
human review is pending. Measured clean source `85cf660b`, baseline `06c76c0b`.
No production guard, cap or implementation changed after timing.

- Corrected unit and race: each six top-level / 66 cases, zero failures/skips.
- Software: eight top-level / 14 cases, zero failures/skips, including actual NUL,
  String/LargeString ownership/offset/cap/error checks and managed/unmanaged full
  multi-record values/defaults, atomic/partial/unknown-reply/cancellation controls.
- Construction: all 48 samples retained, integer/mixed, 8/105 columns, 63/1024
  rows, complete-field independent references before/after each timed operation.
- Native: actual versioned baseline32 + candidate32, all eight cells/warmups,
  complete physical types/values/count/multiplicity/ordinal validation and raw
  unmanaged checks, zero failures/retries/discards.

Mixed allocation medians fell 89.2–99.3%, beyond the fixed 50% target. Mixed
conversion time fell 23.0–32.4%; integer allocation/byte controls are identical.
No micro elapsed/allocated-byte regression exceeded 3% with nonoverlapping ranges.
Native elapsed medians range −4.79% to +1.06%; the small unmanaged one-writer
one_batch slowdown has nonoverlapping ranges but stays below the fixed 3% gate.
RSS medians range −0.30% to +0.61%, below the fixed 1% gate. All quantitative
criteria pass; this does not claim universal native speedups or memory savings.

The first unit attempt failed to compile because a test-only explicit []byte{0}
NUL fixture was declared const. Only the declaration changed to a local variable,
then unit-02 passed before any micro/native timing. The original failed source
`e7803023`, Go/controller exits and digest remain in checks. All measurements use
`85cf660b`; production Go remains byte-identical to initially reviewed `cd44471b`.
No test assertion or threshold was weakened; integer controls and quantitative
thresholds were fixed before the first execution.

Root and sibling source reviews found no blockers before execution. These are
agent reviews, not human approval. The [full report](../../../../benchmarks/table-modes/ingest-strings-2026-11-13/report.md)
retains all distributions, ownership/retention limits, build/helper/input hashes,
raw private log digests, reproduction and shared-host/ordered-block limitations.
Read-only data/hash audit, syntax, relative links and exact patch application
checks passed. Parent final artifact review at `246b65fe` found no blockers and
independently recomputed the quantitative gates. This is agent review; source
code remains proposed, not merged/released.

Every loan ended with all subprocesses/clients closed, empty/index-free DB and
effective 4x QPF / strict TLS / RESP3 reverified. Root final shared infrastructure
cleanup is pending; operational artifacts, credentials and license files remain
private. No ClickBench or alternative server/provider was used.

See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
