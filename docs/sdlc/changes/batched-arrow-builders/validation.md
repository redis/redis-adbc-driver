# Validation and decision

Measured source `2caafbc3cef7005523d4f0eca1631c2646128d4e`, baseline
`24f654b76b98f3d8960af94a3ca0230185d467a1`. The source was frozen and clean for
unit/race/live tests, all 24 construction samples and the native build/112 trials.
No failed/skipped attempt, retuning, selective rerun or removed sample occurred.

- Unit: 20 top-level / 58 including subtests, all passed, zero skips.
- Race: 20 top-level / 58 including subtests, all passed, zero skips.
- Software: six top-level / 21 including subtests, all passed, zero skips.
- Micro: all eight construction cases × three repeats, complete-field reference
  checks before/after timing; scalar control uses candidate with staging disabled.
- Native: actual baseline56 + candidate56, all eight cells and complete 100K×105
  independent output/type/multiplicity validation, all warmups retained.
- Fixture: effective active 4x QPF, strict verified TLS / RESP3, empty/index-free
  after both native blocks; all clients/processes closed. Parent final teardown
  completed: owned container/network and generated TLS/environment/credentials
  removed; preexisting one container and 31 networks preserved. The public proof
  is included with the report.

Wide construction time improved 7.2–12.0% while allocated bytes rose 4.0–11.9%.
Native unmanaged medians improved 2.5–6.7%; managed four-reader improved 2.6–5.1%,
managed one-reader stayed essentially flat. At 4096 rows, managed one-reader and
unmanaged four-reader candidate RSS ranges were entirely above baseline. Keep
production unchanged: higher memory cost fails the predeclared adoption gate.
The exact rejected implementation/tests remain in candidate.patch.

All samples, control caveats, source/binary hashes, full distributions and the
read-only Python audit are linked in the [report](../../../../benchmarks/table-modes/batched-arrow-2026-11-13/README.md).
The parent Codex agent reviewed evidence commit `09a79b9d`: all 136 samples,
all eight cells, tradeoffs and empty production Go diff passed review, no blockers.
This is an agent review, not human approval; no merge or release is claimed.
`git diff --check` whitespace warnings inside the frozen candidate.patch context
blank lines/email footer are an intentional artifact exception: preserve its
exact bytes/hash. Production/evidence source whitespace has no such exception.
