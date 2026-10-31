# Implementation plan

2026-10-30. Author: Codex. Base: `9124f0f`.

1. Reproduce blocked cursor cleanup with an actual localhost protocol fixture.
2. Bound cleanup in aggregate.go; run cancellation/transport/stream fault tests
   with race detection and RESP3 only.
3. Add independent-principal native metadata validation to the existing TLS
   checker; extend retained prepublication integration checks.
4. Run the source library, published wheel, full Go and native validation on an
   isolated Redis Software 8.6.2 fixture with verified 4x QPF. Keep credentials,
   certificates, management identifiers and raw operational reports private.
5. Record exact results and limitations, update feature docs, and self-review
   lifecycle, security and evidence according to REVIEW.md.

See [intent](intent.md), [spec](spec.md), and [validation](validation.md).

Implementation and focused/native checks are complete. Full Go EOF failures and the separate TLS-fixture setup attempts remain visible in the [validation record](validation.md); hosted CI and maintainer review are pending.
