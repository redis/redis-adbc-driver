# Plan: candidate integration follow-ups

Date: 2026-10-30. Source: `91064e6`.
[Intent](intent.md) · [Specification](spec.md) · [Validation](validation.md)

## Implementation and verification

1. Confirm the live issues in redis/redis-adbc-driver. Historical corpus issue
   numbers in the former fork refer to different work and are not this task.
2. Extend R regressions; reproduce on installed adbi and current upstream source;
   install the proposed patch in an isolated R library and verify real binding.
3. Add independent and ADBC Datasets regressions; retain explicit empty sharding;
   verify the proposed patch in a private package copy, including multiprocessing.
4. Add the owned `ray-tls` launcher, integrate it into the native sweep/report
   summary, and verify trusted/untrusted remote worker paths and fixture cleanup.
5. Update usage, compatibility claims and dated evidence; review lifecycle,
   security and exact-failure boundaries. Preserve the historical full sweep.

## Remaining external work

Submitted [adbi #127](https://github.com/r-dbi/adbi/issues/127) and
[Datasets #8912](https://github.com/huggingface/datasets/issues/8912) with repros,
proposed fixes and regression checks. The user authorized both reports and an
exception to the label requirement because external contributors cannot assign
labels in those repositories. Fix PRs [adbi #128](https://github.com/r-dbi/adbi/pull/128) and
[Datasets #8913](https://github.com/huggingface/datasets/pull/8913) are submitted
and linked to those issues. Neither dependency fix has been released. Remove expected failures and the empty-shard mitigation only after
supported upstream packages pass. The full ten-profile sweep has not been rerun.
