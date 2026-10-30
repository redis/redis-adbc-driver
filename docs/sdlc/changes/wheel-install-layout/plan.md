# Plan and validation: wheel installation layouts

Date: 2026-10-29. Base: `9f18518`. Inputs: [intent](intent.md), [spec](spec.md).

1. Inspect the failing publisher assertion and an actual published wheel.
2. Capture failing regression cases for purelib/platlib layouts, ambiguity and
   unrelated prefixes before correcting the validator.
3. Support only the three recognized library installation locations, remove the
   duplicate root-only assertion, and retain binary/checksum validation.
4. Verify all three released wheels and retry the same stable publication after
   the workflow fix is merged.

Regression test run before the fix failed on both standard .data locations and
failed the new location-rejection expectations. After the fix all **4 tests**
passed, covering supported formats/architectures, truncation, the wheel install
schemes and ambiguous/unrelated paths. All three actual v0.0.19 platform wheels
passed the corrected validator. No release files were modified. The
[release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.19)
will retain the same artifacts when publication is retried.

Review: focused inspection against REVIEW.md confirmed checksum/platform gates
remain unchanged and only permitted install locations are accepted. No human
approval is inferred from these local checks.
