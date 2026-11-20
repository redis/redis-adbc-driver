# Integration plan

1. Review each feature and its focused Redis Software evidence.
2. Integrate the verified commits onto current main in an isolated worktree.
3. Run a combined open-stream/evolution/MERGE regression on a dedicated fixture
   after verifying active 4x QPF and retained-control policy.
4. Run unit checks and relevant existing live/source-key/stream/MERGE regressions;
   record actual results and review findings in validation.md.

See [intent](intent.md) and [spec](spec.md). Operational fixture configuration
remains private; unrelated worktrees and Redis instances are left untouched.

All four steps completed. See [validation](validation.md) for measured outcomes,
review scope and remaining validation limits.
