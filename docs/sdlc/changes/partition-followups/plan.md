# Plan and evidence

2026-10-21; baseline `edbd299`; author: Codex for alberttwong.
Status: implemented subset validated locally; design proposals published; human review unassigned.
See [intent](intent.md) and [spec](spec.md).

1. Extend expression validation and portable typed bindings; retain explicit global
   and layout guards. Add fixed-data independent-process schema/multiset checks.
2. Add synchronous incremental terminal state and standard option handling; test
   completion, errors, cancellation, reuse and handle ownership through Go and C.
3. Implement lazy exact cap-bounded ranges with fresh discovery/caller contract;
   test sparse/residual/direct-key coverage, limits, cancellation and early release.
4. Publish safe global SQL/layout/stable-replay designs before enabling new classes.
5. Extend existing validation artifact reporting and add repeatable benchmark
   instrumentation. Record actual runs below and leave unavailable evidence open.

## Verification

See [verification evidence](evidence.md) and [validation summary](validation-summary.json) for actual commands, outcomes, feature skips and remaining gates.

## Remaining gates

Global SQL retained execution, nonnumeric/large-ID layouts and retained replay
are designs only. Cloud and paid infrastructure require an explicitly owned,
funded deployment and credentials; this change provisions none. CI definitions
are not evidence of a successful run. Human review is required before merging
material behavior changes under the proposed repository review policy.
