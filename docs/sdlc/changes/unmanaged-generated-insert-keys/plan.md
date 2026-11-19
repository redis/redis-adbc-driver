# Implementation plan

Date: 2026-11-18. Source: `818063a8`. Requestor: repository user. Author: Codex.
Status: implemented locally; no issue/PR created.

1. Extend the existing input-only SQL key default in `go/adopt_write.go`.
2. Update the adoption checklist and unmanaged/ownership documentation.
3. Exercise generated keys on adopted and foreign unmanaged registrations,
   validating unique UUID v4 keys, source values, no internal fields, no managed
   publication, explicit NULL refusal and the existing write boundaries.
4. Run focused tests with Redis Software and verified effective 4x QPF; review
   the diff for source ownership and SQL default semantics.

See [intent](intent.md), [specification](spec.md) and [validation](validation.md).
