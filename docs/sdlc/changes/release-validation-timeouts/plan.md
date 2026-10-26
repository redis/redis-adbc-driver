# Implementation and evidence

Date: 2026-10-20. Source: `95d6583`. Status: local checks passed; CI pending.
PR: see the containing PR. See [intent](intent.md) and [spec](spec.md).

1. Cancel run 39482514313, preserve the job log and retry only job 118839998087.
2. Add job/step deadlines, separate image pull, expose setup/test progress and
   retain process/log diagnostics before unconditional container cleanup.
3. Validate workflow YAML, shell syntax (including the nested container script),
   timeout values, cleanup condition and unchanged matrix/test directory.
4. Publish a follow-up PR and inspect the targeted retry's outcome.

Evidence: cancellation completed and the targeted rerun command succeeded. The
preserved log shows the image ready at 19:32:13 UTC, then no command output until
cancellation at 19:42:35 UTC. The first post-install platform message never appeared;
this suggests the suppressed dnf install was waiting, but does not prove its cause.
The retry uses the original workflow revision; new deadlines apply to the follow-up.

Actionlint 1.7.12 passed. Ruby Psych parsed the workflow; bash -n passed all platform
shell snippets; sh -n passed the nested validation script. Timeout/cleanup checks
and git diff --check passed. REVIEW.md review found no change to driver semantics,
permissions, matrix coverage or upload prerequisites. Full hosted validation remains
pending; no timeout recovery or speedup is claimed from local syntax checks.
