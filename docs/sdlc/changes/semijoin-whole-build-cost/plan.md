# Plan

2026-11-25; see [intent](intent.md), [spec](spec.md), [validation](validation.md).

1. On current main (with #631), measure the subset's whole-body read and the probe cost per
   distinct key at 1 / 4 / 8 workers, net of the same join without the EXISTS.
2. Add `wholeBuildByCost` and its call in `scanMem` (see spec), with measured constants.
3. Add `TestSQLSemiJoinWholeBuildByCost`; show its above-threshold case fails with the switch
   off; keep the probe-mechanics tests on probes (`keepSemiProbes`).
4. Run the semi-join / subquery selections under `-race` and the unit-only pass.
5. Re-measure the high-key-count join with and without the switch, and A/B TPC-H Q4 / Q21.
6. Run repeated fresh-process and in-process Q21 series with execution diagnostics for the
   first-run outlier.
7. Open a PR for #662 with the evidence.
