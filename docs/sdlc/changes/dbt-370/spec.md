# Specification: dbt comparison scope

Date: 2026-10-21. Source: `95d6365bb76212a49888bfec423cf8c8b15e61a7`.
Requestor: repository owner, via chat. Author: Codex. Status: proposed for review.
Issue: [#370](https://github.com/alberttwong/redis-adbc-driver/issues/370).

## Contract and boundaries

- Row 7 is ✅ 🔌 and links Core 1.10–1.x and the experimental v2 adapter.
- The v2 implementation is compiled into `alberttwong/dbt`; stock v2 does not
  include Redis. Neither adapter establishes Flight SQL support.
- Cite the companion validation record and successful Linux run, including
  tested engine commit and versions. Separate external evidence from this
  repository's captured-SQL replay.
- Preserve community provenance, unsupported grants/materialized views and
  disabled v2 static analysis. Do not claim universal feature parity.

## Risks and checks

Mutable documentation can drift; pin the tested engine commit and link the
recorded CI run. Check table structure, links/evidence and whitespace.
See [intent](intent.md) and [plan and evidence](plan.md).
