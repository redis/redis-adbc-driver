# Plan

2026-11-17. Source `b63a010d`. See [intent](intent.md), [spec](spec.md) and
[validation](validation.md).

Status: implementation, local verification and agent review complete. The
disposable Software fixture and its temporary ACL principals were removed.

1. Read #399 and PR #456's proposal; confirm dedicated-only scope with the user.
2. Add explicit consent, bounded authoritative inventory and guarded source
   deletion/index transition in unmanaged_replace.go.
3. Add an expected-descriptor condition to unmanaged registration; reuse Arrow
   append with per-pipeline replacement guards and progress tracking.
4. Exercise scope/config refusals, independent final schema/data/index/TTL
   inspection, races, reader/conversion/cancel failures and dropped replies.
5. Run existing unmanaged and managed replacement/Arrow ownership regressions;
   run real Software ACL principals and source permission refusals. Use an
   isolated Software fixture through redis-up with verified effective 4x QPF.
6. Document destructive consent, bounded limits, partial visibility, errors and
   recovery; review ownership, lifecycle and intent/evidence per REVIEW.md.
7. Build the shared library and run the relevant unit/race checks. Record actual
   evidence and remaining limits; do not infer a human review or release.
