# Validation and review

Date: November 14, 2026 (America/Los_Angeles). Author/reviewer: Codex.
[Intent](intent.md), [spec](spec.md), [plan](plan.md). Human review pending.

Focused unit/race selection passed:

```sh
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=4 go test -race -count=1 -p 1 \
  -timeout=5m -run 'TestSnapshotDescriptorRead|TestSnapshotAcquireProof|TestSnapshotControlScript|TestSnapshotValidationBatches' .
```

Coverage includes exact queued control/reader keys and arguments; local/expired
lease refusal; populated replies with unknown pipeline error; native NOSCRIPT
combined with I/O loss; metadata failure before digest fallback; direct native
missing/digest-command-denied fallback; no descriptor value or proof reuse across
pins; native GET permission checks; referenced/legitimate absence; WRONGTYPE;
reference failure versus GET refusal; unknown error precedence and no replay.

The coordinator's isolated fixture gate verified Redis Software, active/effective
4× QPF, six Search workers, mnp/32, 4-GiB noeviction RAM, 4-CPU/8-GiB container,
strict data TLS and RESP3 before both diagnostic commands. Baseline profile and
manifest proof tests passed on 0b216478. Candidate profile plus manifest/cow
metadata corruption, old pins, retained streams, expiry/cancellation, shared
metadata/streams/failures, direct partition corruption, unmanaged live-source
writes and prepared definition-freshness checks passed (14.678 seconds).
All harness clients were closed and fixture data/indexes cleaned before releasing
its exclusive live-test slot. Operational endpoints/identities/logs stay private.

Client-call evidence: the candidate reads use 11 foreground round trips instead
of 13 in both modes, with unchanged Redis command counts (20 managed/19
unmanaged). Pipeline shapes are now `hget,zcard,hlen,control_valid` and
`hmget,exists,get`. The four lease validations still execute; grouping their
transport does not weaken them. Tiny instrumented warm medians were 2.790/3.130 ms
managed/unmanaged on baseline and 4.003/3.926 ms on candidate, with possible
concurrent compilation/host activity. They show no measured wall-time improvement
in that sample and are not native benchmark evidence. Isolated integrated native
before/after results must determine actual latency benefit. The candidate live
run preceded final error-precedence-only refinements, which have separate unit
and race coverage; its results are not relabeled as final-code measurements.

Review: no script body, storage format, namespace, caller-visible schema or
row-discovery path changed. Metadata reads remain native ACL-checked commands;
all value owner/presence/ref proofs and statement dependency records remain.
Reader/shared terminal losses and materialized expiry remain distinct. Explicit
full-body fallback is limited to a definite native digest refusal after metadata
proof success; unknown pipeline failures never become successful proofs. Complete
integration, cluster/RBAC capability checks and final native timings remain with
the coordinator before PR completion.

## Final integrated follow-up

The [November 14 combined validation](../point-write-retrieval-20261114/validation.md)
records the final guarded native build, complete paired matrix, corrected boundary
failure, independent review and ownership cleanup. Earlier diagnostics above retain
their actual sources and scope; they are not relabeled as final native measurements.

Implementation and evidence: [PR #439](https://github.com/redis/redis-adbc-driver/pull/439).
