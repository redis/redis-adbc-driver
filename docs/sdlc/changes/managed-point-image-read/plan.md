# Plan

Date: November 14, 2026. Author: Codex. Base: 0b216478.
[Intent](intent.md), [specification](spec.md), [validation](validation.md).

1. Profile the unchanged production source with worker enabled and matching
   1,000-row/four-public-column INSERT/UPDATE/DELETE fixture and known row IDs.
2. Use client call counts and native slowlog separately to choose the target;
   do not infer Lua CPU from client-side waiting.
3. Fuse only each one-row segment read and its conditional absence proof;
   retain visibility order and all surrounding execution/publication fences.
4. Add focused exactness/corruption/permission/no-replay/fallback checks, compare
   the identical diagnostic workload, then run relevant snapshot regressions.
5. Parent coordinator reviews and combines this with other scoped changes,
   freezes source and runs isolated complete native before/after matrices.
