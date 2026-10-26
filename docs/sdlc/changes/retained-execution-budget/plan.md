# Plan

2026-10-21. Author: Codex; requestor: Albert Wong. Status: runtime implemented;
validation and review recorded; [PR #430](https://github.com/alberttwong/redis-adbc-driver/pull/430) open as a draft. Base: `9510b9d`.

1. Completed: pre-decode bounded RESP transport, cumulative credits, TLS/RESP3,
   socket cancellation and failure normalization.
2. Completed: exact authoritative capture for managed/large IDs, adopted,
   application/foreign and legacy layouts; byte-key preservation, ownership,
   readiness, source identity/cardinality and Cluster routing guards.
3. Completed: global SQL with per-operator admission and explicit unsafe-plan
   rejection; exact Arrow equivalence through Go and native ADBC.
4. Completed: public options, retained publication/descriptors, reader-only
   bootstrap, explicit revocation, process death/cancellation and minimal ACL.
5. Completed: configured full Go/native suites, local standalone and verified
   three-primary TLS coverage; CI collection and minimum-client release wiring.
6. Completed: repeated end-to-end measurements and agent review/evidence.
   Runtime PR #430 is open. Post-merge Go/native/TLS validation passed;
   continue packaged/deployment coverage and retained routing checks under #363.

See [intent](intent.md), [spec](spec.md), [evidence](evidence.md).
