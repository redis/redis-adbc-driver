# Plan and verification

Date: 2026-11-18. Status: implementation and focused verification complete.
See [intent](intent.md) and [specification](spec.md).

1. Audit existing key-row reads/writes and managed ownership boundaries.
2. Add explicit live registration, driver-owned index lifecycle and opt-in schema
   discovery without publishing mutable HASHes as managed generations.
3. Route live SQL writes through the key-row executor and reject operations that
   would imply managed atomic publication or source ownership.
4. Add CLI access and user documentation for registration and SQL/Redis writes.
5. Verify focused integration cases on Redis Software with active 4x QPF, run
   unit checks and review lifecycle, correctness and ownership.

See [validation and review](validation.md) for actual results and limits.
No maintainer approval or release is implied.
