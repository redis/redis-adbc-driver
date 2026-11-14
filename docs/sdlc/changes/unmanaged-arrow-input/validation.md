# Validation and adoption decision

Measured clean source `67264020f524f62a431ef00585a218c6a63660f5`, actual clean
baseline `06c76c0b68258862d091487c3eb280b961f4fb8d`. All 112 native and 24 micro
samples are retained, with no retuning/retries/discards. Unit/race each passed
70 cases / 24 top-level tests, Software 27 / 10, zero skips/failures.

The new unmanaged-only candidate passes its original gate: all four unmanaged
median elapsed times decreased 0.27–7.47%, geometric-mean speedup 4.22%. The
weakest −0.27% cell is essentially flat. Managed medians range −2.99%..+4.67%;
the +4.67% control has overlapping ranges, so it does not trip the original
>3% plus nonoverlap condition. Its first-batch median is also 7.50% higher, with
overlapping ranges. All RSS ranges overlap (medians −0.63%..+0.28%).
No universal/causal speedup claim follows from one ordered shared-host experiment.

A test/docs-only supplement `1e112158028d60757c2555f11d8376d206da9332` added actual
zero-byte unit/race/Software regressions, each passing once with no skips/failures.
All 270 production Go/module files remain identical to measured source; no native
binary was rebuilt/relabelled. This corrects demonstrated old report NUL wording
without changing historical sources/results/attempts. The measured new fixtures
contain literal backslash-x00 text; only the supplement proves actual NUL.

Parent and sibling source reviews found no blockers; they are agent reviews,
not human approval. Complete [results/provenance](../../../../benchmarks/table-modes/unmanaged-arrow-input-2026-11-13/README.md)
retain all tails, first-batch/RSS distributions, exact attempts and limitations.
The loan ended empty/index-free with all clients/processes closed and effective
4x QPF/strict TLS/RESP3 verified. Parent final shared teardown remains pending.
See [intent](intent.md), [contract](spec.md) and [plan](plan.md).
