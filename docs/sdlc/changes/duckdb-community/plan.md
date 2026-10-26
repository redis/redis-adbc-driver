# Implementation and submission plan

Date: 2026-10-20. Author: Codex. Status: implemented and submitted; remote CI
and maintainer review remain pending.
See [intent](intent.md), [spec](spec.md), and [evidence](validation.md).

1. Contribute Redis installation, read/join and autocommit examples to
   adbc_scanner. Execute every example on a disposable server and open a PR.
   Done: [#21](https://github.com/Query-farm/adbc_scanner/pull/21).
2. Add an optional DuckDB build over the existing Go driver, a parser guard,
   C++ Arrow lifecycle handling, pinned toolchains, documentation and CI.
   Done: distributable build and local verification completed.
3. Open a reviewable implementation PR in the Redis driver repository.
   Done: [#344](https://github.com/alberttwong/redis-adbc-driver/pull/344).
4. Add one `extensions/redis_adbc/description.yml` in the community repository,
   pinning the public implementation commit. Validate and submit it.
   Done: [#3157](https://github.com/duckdb/community-extensions/pull/3157).
5. Record observed checks, review findings and remaining CI/maintainer decisions.
   Do not claim community installation works before acceptance and deployment.
   Done: [local verification](validation.md) and PR descriptions record actual
   checks. Remote CI and upstream review/deployment remain external decisions.
