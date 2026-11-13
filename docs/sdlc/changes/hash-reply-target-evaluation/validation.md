# Validation and rejection

Final candidate `b3afc72367bd03dfc2f5da8a081079dde7e5e5ca` (production `2520eb1b`)
compared with merged baseline `ba855f7995b2685182ed1331bcf6703d73089c4a`.
All 56 native trials validated complete types/values/multiplicities; no failed,
retried or discarded trial. Final focused race selection: 49 pass events, two
expected unset-server skips; verified Software live race selection: 47 pass
events, zero skips/failures/races. Test parent/nested events are counted.

Median peak worker RSS fell 8.9–10.8%, while single-reader elapsed time rose
32.7–34.0%. Reject as the default; production remains unchanged. No lower-target
retuning. Four-reader timing changes are small and not claimed as improvements.
Baseline-first/candidate-second order and small local samples limit inference.

See the [complete report](../../../../benchmarks/table-modes/hash-reply-target-2026-11-13/report.md)
and its all-sample results and exact build/source hashes. Raw artifacts remain
private; the loaned Software fixture was returned idle/empty without teardown.
This evidence-only branch requires no production rebuild or new server test run.

Whitespace validation excludes the unapplied nested unified-diff artifact:

```sh
git diff --check ba855f79 HEAD -- \
  ':!benchmarks/table-modes/hash-reply-target-2026-11-13/candidate.patch'
```

That scoped check passes. Checking the patch as ordinary added text reports its
unified-diff context prefixes before Go tabs and empty context lines; those
prefixes are required patch syntax and are intentionally preserved. The patch
itself passed `git apply --check` against the baseline, and the sample audit
verifies its recorded SHA256. No unscoped all-file whitespace-clean claim is made.
