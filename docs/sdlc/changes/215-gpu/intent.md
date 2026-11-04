# Intent: GPU feasibility for #215

Date: 2026-11-03. Source: `95d6583`. Requestor: repository user in this chat.
Status: native implementation under validation, following the investigation.

Determine whether GPU execution could improve Redis-backed query performance,
while distinguishing Arrow C Device interoperability from SQL execution.
Issue: [#215](https://github.com/redis/redis-adbc-driver/issues/215).

Deliver a source-grounded [specification](spec.md), runnable CPU/CUDA comparison,
independent exact result validation and an [implementation/validation plan](plan.md).
The user subsequently requested native GPU execution and C Device support.
Implement opt-in CUDA integer predicate execution and explicit numeric/boolean
C Device import/export extensions, preserving CPU defaults and exact reducers.
The standard ADBC function table remains unchanged. Real-device validation of
the native implementation is required separately from the earlier prototype.
