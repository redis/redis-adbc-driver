# Evaluation contract

Keep current-master parser and pinned-v9.23 native backport as separate version
comparisons. Require owned byte/NULL results, hook/cache isolation, existing
error/routing/lifecycle semantics and complete value/type/multiplicity validation.
Record compound driver-base + API patch + Go overlay + module/sum provenance;
clean base VCS metadata alone is insufficient.

Use Redis Software with active/effective 4x QPF and strict data TLS/RESP3 for
live checks/native tests. Parser microbenchmarks use no server. Serialize heavy
work and retain all attempts/warmups. Do not trade ownership, cancellation or
snapshot/readiness proofs for throughput. A parser gain is not a driver gain;
keep the experimental adapter/dependency unapplied after a negative native result.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
