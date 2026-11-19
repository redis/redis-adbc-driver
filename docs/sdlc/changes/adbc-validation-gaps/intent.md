# Intent

Date: 2026-11-19. Requestor: Albert Wong. Author: Codex with parallel subagents.
Baseline: `3e45955b` (v0.0.30). Status: implemented; local verification complete; PR review tracked separately.
Issues: [#535](https://github.com/redis/redis-adbc-driver/issues/535), [#536](https://github.com/redis/redis-adbc-driver/issues/536), [#537](https://github.com/redis/redis-adbc-driver/issues/537), [#538](https://github.com/redis/redis-adbc-driver/issues/538), [#539](https://github.com/redis/redis-adbc-driver/issues/539), [#540](https://github.com/redis/redis-adbc-driver/issues/540), [#541](https://github.com/redis/redis-adbc-driver/issues/541).
See [specification](spec.md), [plan](plan.md), and [validation](validation.md).

Resolve seven explicitly requested ADBC validation gaps through truthful
constraint declaration metadata, tested table-type filtering, and real logical
catalog identity/discovery/selection/Arrow destinations. Multi-statement
transactions (#542/#113) and UNIQUE/FOREIGN KEY enforcement (#114) are excluded.
No disabled feature flag may be enabled without native-boundary evidence.
