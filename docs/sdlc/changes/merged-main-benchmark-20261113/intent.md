# Intent: merged-main benchmark refresh

Date: November 13, 2026 (America/Los_Angeles). Requestor: repository user. Author: Codex. Source: `ca6858065c4cf02418d55c3aa62eb583590fe252`. Status: evidence complete; human approval pending.

After merging PRs #433–435, the user requested a benchmark rerun and an update to the existing Google Slides deck. Measure current managed/unmanaged SQL and Arrow paths against redis-py+hiredis using the established full matrix. Preserve historical results and report actual sources/settings. No 100M ClickBench rerun, new feature, or inferred per-change speedup.

See [specification](spec.md), [plan](plan.md), and the linked PR once created.

Review: [PR #436](https://github.com/redis/redis-adbc-driver/pull/436).
