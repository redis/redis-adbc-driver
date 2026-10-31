# Intent: Arrow interoperability issues 198–209

- Date: 2026-10-30; requestor: Albert Wong; implementation: Codex.
- Base: `de3404298309d46d17a8dce0518806c434454cda` (available origin/main).
- Request: work on GitHub issues [198–209](https://github.com/redis/redis-adbc-driver/issues/198).
- Status: implementation and focused verification; not a release or issue-closure decision.

Consumers need lossless values, predictable Arrow schemas, explicit layout
selection and correct buffer lifetimes. The change extends existing SQL storage
and scan paths, with explicit rejection for unsupported semantics. It introduces
no transaction/consistency feature or zero-copy/performance promise.

The [specification](spec.md) defines implemented boundaries. The [plan and evidence](plan.md)
tracks actual validation and remaining acceptance.
