# Plan

Date: 2026-10-21. Source: `70a0d43`. Author: Codex; requestor: Albert Wong. Status: implemented; review pending. See [intent](intent.md) and [spec](spec.md).

1. Extend the owned OSS TLS checker with bounded persistent worker handshakes and scoped migration.
2. Verify ASK denial/grant, ASK/MOVED results, post-migration Search and physical ownership.
3. Add management safety tests, document ASKING ACL requirements and evidence limits.
4. Run live automatic/explicit Cluster API checks, preserve evidence, remove all owned resources and open a PR.

The existing Test and installed-wheel Release invocations reuse this checker;
no new CI matrix jobs or broad packaging triggers are needed.

Completed: owned-slot migration, bounded persistent workers, ASKING denial/grant,
real ASK/MOVED probes, exact native and broad Search results, all-primary physical
ownership, five management safety cases, documentation, two final client runs,
30 unit/fault checks and owned-resource cleanup. See [evidence](evidence.md).
