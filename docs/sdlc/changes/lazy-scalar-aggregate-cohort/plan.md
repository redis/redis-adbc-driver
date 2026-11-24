# Plan

See [intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Add statement-local cohort registration at the end of successful outer SELECT planning and activation at the existing uncorrelated subquery cache boundary.
2. Add an isolated cohort helper with strict source/member/field limits, raw exact-directory pages, shared field decoding, per-lane deferred errors and existing exact accumulators.
3. Mark partition-acquired scopes privately so the new helper explicitly excludes them; change no snapshot layout or public option.
4. Exercise real parsed CASE/outer-table planning plus activation, and the ordinary streaming SELECT route, with nondelegating command hooks. Assert registration, no pre-execution reads, zero scans for outer/nested CASE gates and exactly one fact scan for invoked siblings.
5. Compare rejected-row decode/error order, defaults/NULL markers and exact AVG result types/values against the serial reference. Test cancellation, later-page/source-control failure, unsupported sources/shapes, 32-member/eight-field caps and statement/snapshot separation.
6. Run focused and full unit-only checks, freeze source, and hand it to root for independent review. Do not build/run a native library, publish a PR or run any live query in this prototype task.
