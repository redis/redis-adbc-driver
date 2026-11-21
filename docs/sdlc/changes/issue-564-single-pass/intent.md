# Single-pass private Arrow append indexing

Date: 2026-11-20. Source: `1b5918e6`. Requestor: Albert Wong, issue
[#564](https://github.com/redis/redis-adbc-driver/issues/564). Status: implementation
complete locally; awaiting integration review and matched performance evidence.
No human review or performance claim.

Wide managed Flex ingestion currently writes complete private rows, then sends
another bounded notification pass solely to populate the final index. Populate
an eligible append's final index from its original atomic complete-image writes.
Keep private visibility, exact data, cancellation, outcome resolution and final
publication proofs. The full benchmark remains stopped.

Scope is intentionally restricted to ordinary Arrow append/create_append with
only hidden numeric index fields. SQL, replace/create, imported provenance TAGs,
public indexed columns and retained operations keep deferred indexing. No new
storage protocol or restart/resume contract is introduced.

See [spec](spec.md) and [plan](plan.md).
