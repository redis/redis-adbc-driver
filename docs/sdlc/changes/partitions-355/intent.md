# Portable ADBC partitioned reads

Date: 2026-10-20. Author: Codex. Requestor: user in this chat.
Source commit: a08573bdc40c07bca5bc3ccf9cee1e59cbc6490c.
Status: implemented and locally verified; human review pending.

[Issue #355](https://github.com/alberttwong/redis-adbc-driver/issues/355)
requests portable ADBC partition descriptors for distributed consumers, including
the proposed Ray integration and Dask. Replace the two unsupported API stubs
with deferred, bounded reads without coordinator result materialization.
Keep a narrow, explicit SQL scope and do not promise snapshot isolation.

See [spec](spec.md), [plan](plan.md), and [verification](validation.md).
