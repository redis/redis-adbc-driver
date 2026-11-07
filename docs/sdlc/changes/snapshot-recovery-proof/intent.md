# Recovery metadata proof investigation

Date: 2026-11-06. Requestor: repository owner (user). Author: Codex. Source: `bbc0c0a099dba585a41dadf355a6b8324e3ead1c`. Issue: [#345](https://github.com/redis/redis-adbc-driver/issues/345). Status: investigation in progress.

Explain and correct the retained metadata-bucket failure after promotion, all-node RDB restart and resumed compaction publication. Repeated healthy runs alone cannot establish its cause or close the issue. Work on #341 follows only after #345 is delivered and confirmed closed.

See [spec](spec.md), [plan](plan.md) and [evidence](validation.md).
