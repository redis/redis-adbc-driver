# Intent

2026-11-03. Base `6d7ac409d8c9f11163b4f4cec63e2d4da00e69f2`.
Requested by Albert Wong: complete driver issues #102–#105. Author: Codex.
[Specification](spec.md), [plan](plan.md), [validation](validation.md).

[#105](https://github.com/redis/redis-adbc-driver/issues/105) promises that
materialized-view readers can finish on the old generation while refresh
publishes a new one. A 25,000-row stream currently fails after 10,000 rows when
a second connection refreshes: publication immediately releases/deletes its
generation. Preserve that result without blocking refresh on the reader.

This proposal corrects the generation lifecycle; it does not add general
transactions or cross-table snapshot isolation. Submitted as a draft at the
user's request; #105 stays open for review. dbt
integration is explicitly moved to
[redis-dbt-adapter #38](https://github.com/redis/redis-dbt-adapter/issues/38) and
[#39](https://github.com/redis/redis-dbt-adapter/issues/39).
