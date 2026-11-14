# Plan and verification

Date: November 13, 2026. Author: Codex; requestor: repository user. Measured source: `ca6858065c4cf02418d55c3aa62eb583590fe252`. See [intent](intent.md) and [specification](spec.md).

1. Build a clean detached measured source and record identities. Provision with make -C go redis-up and verify actual settings.
2. Run the unchanged full SQL/Arrow/native matrix serially with heavy competing tasks paused.
3. Audit every case, repetition, result and median/range/RSS value; publish a separate evidence worktree.
4. Return the owned database empty/index-free, reverify settings, close all clients and remove only owned resources/generated credentials.
5. Update existing native Slides evidence and notes; read back and visually verify all pages, preserving historical content.
6. Review evidence/ownership/security and open a report PR. Do not infer human approval or merge the report automatically.

Final commands, outcomes and limitations will be recorded in validation.md. These changes contain reports and an audit only; production behavior is unchanged.

Review: [PR #436](https://github.com/redis/redis-adbc-driver/pull/436).
