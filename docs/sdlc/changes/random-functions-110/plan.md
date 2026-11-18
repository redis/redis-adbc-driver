# Plan

Date:2026-11-17. Requestor:Albert Wong. Author:Codex.
Base:`5b0840ce`. Status:implemented; local verification complete; PR review pending.
Issue:[#110](https://github.com/redis/redis-adbc-driver/issues/110).

1. Probe PostgreSQL18 source/oracle and retain focused failing signature/seed/random cases.
2. Implement session PRNG, function registry/type/parameter checks, execution and logical void mapping.
3. Test seeded stream vectors, draws/interleaving/reseed/isolation, strict NULLs, lazy/empty/failed statements, bound Arrow input/reuse and races on owned Software4xQPF.
4. Review ownership/side effects and errors, document actual source/evidence, clean own fixture and open PR referencing#110. Coordinate #479 DEFAULT definition-time evaluation separately without duplicating defaults.go changes.

See [intent](intent.md), [spec](spec.md), [plan](plan.md).
