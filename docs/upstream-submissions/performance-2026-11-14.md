# Upstream performance follow-up — November14,2026

Opened after checking existing reports:

- [RediSearch #12334](https://github.com/RediSearch/RediSearch/issues/12334): exact int64 source-HASH grouping/reductions, with full64-bit/cancellation/overflow semantics and a request for existing-API guidance. Workload evidence distinguishes approximate native baselines from exact live reads; no promised upstream speedup.
- [go-redis #4315](https://github.com/redis/go-redis/issues/4315): retire pooled connections after failed WATCH/MULTI cleanup. Actual go-redis protocol regressions and source are linked; this is not mislabeled as a new native Software ACL reproduction.
- [Existing indexing barrier #12220](https://github.com/RediSearch/RediSearch/issues/12220#issuecomment-6880832784): added current managed-write/command-count evidence. Scoped backfill already has #12328; no duplicate opened.

Existing point-read views/server-managed-table requests remain redis/redis#16180 and redis/redis#16238. Driver-owned admission and metadata optimizations remain in this repository.

Both new issue creation requests included verified existing labels. GitHub omitted them despite accepting the issues. The user explicitly approved keeping both open without labels on November14,2026, an exception to the project default for these two reports. No credentials, endpoints or operational artifacts were published.
