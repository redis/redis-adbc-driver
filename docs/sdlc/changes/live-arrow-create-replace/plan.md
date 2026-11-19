# Plan

Date: 2026-11-18. Author: Codex. Status: local validation passed; PR checks pending.
See [intent](intent.md), [specification](spec.md) and [validation](validation.md).
PR: [#532](https://github.com/redis/redis-adbc-driver/pull/532).

1. Extend upstream live dispatch with strict source options and create-only
   generated-index registration; preserve the existing live Arrow converter.
2. Share unmanaged dedicated replacement controls with live registration/index
   validation and CAS publication. Preserve unmanaged behavior and recovery.
3. Cover create/create_append, generated keys, replacement schema/empty input,
   source ownership, consent/scope refusals and uncertain deletes/DDL.
4. Run unit-only packages, verified Software 4x QPF snapshot regressions and
   ordinary live/Arrow regressions separately, build native/CLI artifacts and
   validate docs. Review lifecycle, data ownership and evidence.
5. Open PR, monitor checks and fix failures. No merge/release requested.
