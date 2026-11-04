# Support classification contract

Date: 2026-11-04. Base: `b1d1bf005780516a17ceaf46ed49840506c77b20`.
Author: Codex. Status: documentation implemented; awaiting PR review.
See [intent](intent.md) and [verification](plan.md).

- Supported means the documented SQL/Arrow contract is implemented, including
  normalization and explicit layout selection. It does not mean preserving all
  original physical encodings or accepting every Arrow type/SQL operator.
- Update 14 completed Partial rows for #198, #200–212. Scope row names for
  incremental execution, dictionary output, list/layout normalization, named
  Struct, UUID/JSON normalization and metadata/row-count statistics.
- Keep precision/range checks, normalization, dictionary budgets, partition
  restrictions, approximate statistics, no source snapshot, autocommit and sort
  cancellation boundaries visible. Do not imply new runtime validation.
- Retain Map/Union #267/#268, constraints #114, transactions #113, Substrait #213
  and CDC #196. Redis wire decoding remains unsupported zero-copy transport.
- Link experimental device/GPU rows to existing open #291–295; completion of
  #215 is a scoped experimental milestone, not general production support.
- Add a separate remaining-work inventory for #300–306. Each new issue has an
  existing enhancement label, pinned source evidence, bounded acceptance and
  exclusions. Do not turn intentional normalization or source-snapshot scope
  exclusions into an unbounded implementation promise.

## Audited completion evidence

- [#270](https://github.com/redis/redis-adbc-driver/pull/270) and its
  [acceptance mapping](../arrow-remaining-closeout/spec.md): #198, #200–207, #209.
- [#269](https://github.com/redis/redis-adbc-driver/pull/269) and
  [layout contract](../../../arrow-interoperability.md#layout-assessment-and-decoded-input-limits): #208.
- [#260](https://github.com/redis/redis-adbc-driver/pull/260) and
  [closeout validation](../210-212/validation.md): #210–212.

## New follow-up issues

| Issue | Work |
|---|---|
| [#300](https://github.com/redis/redis-adbc-driver/issues/300) | Incremental execution beyond the finite-streaming contract |
| [#301](https://github.com/redis/redis-adbc-driver/issues/301) | Bounded dictionary IPC file output |
| [#302](https://github.com/redis/redis-adbc-driver/issues/302) | Explicit result-layout partition replay |
| [#303](https://github.com/redis/redis-adbc-driver/issues/303) | Column-level statistics |
| [#304](https://github.com/redis/redis-adbc-driver/issues/304) | Broader bounded partition SQL |
| [#305](https://github.com/redis/redis-adbc-driver/issues/305) | Nested extensions/additional explicit mappings |
| [#306](https://github.com/redis/redis-adbc-driver/issues/306) | Cooperative interruption during sorts |
