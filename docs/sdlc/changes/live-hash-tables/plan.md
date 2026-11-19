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

Pull request: [#510](https://github.com/redis/redis-adbc-driver/pull/510).

Follow-up: use a live-only SQL default for omitted/DEFAULT `__key`; verify
per-row UUID generation in VALUES and INSERT SELECT, generated-key RETURNING,
implicit target lists, prefix quoting, explicit NULL/collisions and source HASH
identity. Rebuild the source binaries and update PR #510.

Documentation follow-up: rewrite the README and ownership comparison, preserve
incoming documentation anchors, and check local links plus shell/Python example
syntax. Update the existing PR after documentation review.

Remove the standalone live-access README section and its navigation entry,
move the end-to-end example to the live HASH guide, and give each ownership
mode a brief description and links to its detailed documentation.
