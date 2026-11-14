# Specification

[Intent](intent.md), [plan](plan.md), [validation](validation.md).
Author: Codex. Human review pending.

Search still discovers candidate keys. A per-key read-only Lua invocation checks
TYPE and reads only needed HASH fields via HMGET atomically; no permanent field
is assumed. Existing HASHes with all selected fields missing remain rows. Missing
and non-HASH keys retain the established fetchKeyRows omission behavior. Return
native strings with exact missing/empty/NUL and integer precision; recheck the
whole SQL residual and decode needed columns in existing metadata order.
Application fields named __key must never replace the actual source address.

Every invocation declares one application key, so source keys need no common
Cluster hash tag. Bound commands to at most 256 requested fields and 1000 keys
per pipeline, targeting at most 1 MiB of request arguments per pipeline. Larger
requests, layouts requiring missing-through defaults and full scans keep the
existing HASH path. Source-prefix isolation remains checked before any read.

Use EVALSHA_RO and EVAL_RO with NoRetry markers. Only a native NOSCRIPT refusal
permits one full-body attempt. Only native fixed TYPE/HMGET ACL-preflight denial
tokens or exact top-level read-only-script command permission refusals permit
HGETALL compatibility fallback. Inspect every command and the pipeline outcome
before repair/fallback; unknown replies, cancellation, malformed data, unrelated
server errors and unaccounted pipeline errors terminate without replay. Capability
state belongs only to this scan, not a later catalog pin or changed application ACL.
