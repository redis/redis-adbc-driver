# Redis in the DuckDB ecosystem

Date: 2026-10-20. Source baseline: e8fddea0b0734bacb2473342803500f5bd3448a5.
Requestor: user in the current Codex chat. Author: Codex. Status: implementation
complete and upstream submissions open; no maintainer approval implied.

The user requested both an integration documentation contribution to
`Query-farm/adbc_scanner` and a standalone `redis_adbc` extension submitted to
`duckdb/community-extensions`.

The existing repository exports an ADBC driver, so a registry descriptor alone
cannot make it a DuckDB extension. Embed that driver in a native extension with
a streaming SELECT interface. Keep the existing adbc_scanner route documented
for attached catalogs, ingestion and writes. Avoid conflicting with the
already registered, separate `redis` command-client extension.

See [specification](spec.md), [implementation plan](plan.md), and
[verification evidence](validation.md). The upstream documentation contribution
is [adbc_scanner #21](https://github.com/Query-farm/adbc_scanner/pull/21).
The implementation is [Redis driver #344](https://github.com/alberttwong/redis-adbc-driver/pull/344)
and the registry submission is [community-extensions #3157](https://github.com/duckdb/community-extensions/pull/3157).
