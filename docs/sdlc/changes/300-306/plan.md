# Implementation and verification plan

See [intent](intent.md), [specification](spec.md), [validation](validation.md).
Date: 2026-11-04. Status: implementation and local verification complete; PR/CI closeout pending.

1. #306: central cooperative sort helper, error propagation and deterministic
   cancellation/deadline, stable-tie and panic tests; measure sorting overhead.
2. #301: bounded file-wide dictionary construction and remapping, native output
   with independent reader checks and partial-file cleanup.
3. #302: versioned layout replay contracts for live/retained partition readers.
4. #303: select and compute bounded column quantities with standard ADBC keys.
5. #304: explicitly admitted deterministic row-local partition functions.
6. #305: additional explicitly allowlisted extension storage mapping.
7. #300: select another incremental operator and prove semantics/admission.
8. Run Go and native driver-manager validation against Redis Software with
   verified 4x QPF. Review actual diff, push reviewable commits and close only
   issues whose acceptance has been demonstrated.
