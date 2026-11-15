# First-valid sequence investigation: intent

Date: 2026-11-14. Requestor: Albert Wong's performance work, narrowed by the coordinating Codex agent. Author: Codex. Source: `7fc608ea`. Status: diagnostic only; no production change or human maintainer approval inferred.

The prior point profile isolated most overlay request time to the first standalone lease validation. This change tests whether that delay depends on preceding native segment proofs, while preserving actual pins, proof helpers, complete SQL correctness oracles and privacy. It does not reduce or cache validation and does not claim server CPU or end-to-end SQL improvement.

See [specification](spec.md), [plan](plan.md) and [evidence](../../../../benchmarks/point-valid-sequence/README.md).
