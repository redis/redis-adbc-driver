# Plan and checks

See [intent](intent.md) and [spec](spec.md).

1. Inspect README, repository guidance, PostgreSQL comparison, native-client
   contract, unmanaged ingestion and table-mode documentation.
2. Rewrite the rationale with linked, bounded claims.
3. Check local link targets, percentage arithmetic and whitespace; review the
   diff for consistency with storage ownership and historical evidence.

Documentation-only validation: local relative links and referenced headings
checked; 604/651 rounds to 92.78%; `git diff --check` passes. No runtime tests
or new benchmarks required or run. Automated self-review found no remaining
ownership or compatibility claim conflicts; no human review claimed.
