# Primary DuckDB 2.0 target

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Status: implementation and submission update in progress; review pending.

The user asked to focus on DuckDB v2.0 after the community maintainer
[clarified](https://github.com/duckdb/community-extensions/pull/3157#issuecomment-6301682096)
that descriptor `repo.ref` now deploys 2.0. The standalone extension previously
used 1.5.6 for its default native build and runtime CI, with a separate 2.0
compile check and manually recorded runtime verification.

Make the default build and native runtime CI match the registry's 2.0 source.
Keep the submission on `repo.ref`; no 1.5 deployment is requested. Describe
the 2.0 target explicitly in the source guide and community PR.

See [spec](spec.md), [plan](plan.md), and [verification](validation.md).
