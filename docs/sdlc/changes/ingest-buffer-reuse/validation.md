# Validation and outcome

Frozen candidate `553960585f29a5f7b1151742e97d12168f5a9d69`, clean baseline
`88774f10cf16fc4260e6ad5077097f4de8828406`. **Keep the candidate unapplied**:
wide conversion allocated bytes fell 64.7–66.8% and six native reuse cells lowered
RSS 6.4–15.3%, but timing is mixed. Managed stream medians rose 1.0%/6.0% for one/
four writers; these observations do not establish the original no-clear-elapsed-
regression adoption gate. No post-hoc threshold, retuning or selective rerun.

- Focused unit and race: each three top-level / 59 cases passed, zero skips/fails.
- Software: eight top-level / 14 cases passed, zero skips/fails; complete managed/
  unmanaged 4096/3/4097/1-record typed results, defaults, NULL strings and >2^53;
  existing managed 100001-row success/late failure and unmanaged partial effects,
  cancellation, unknown replies, reader errors and changed-schema controls.
- Micro: 24 complete-conversion samples, fresh matrices versus reuse including
  required clearing, initial allocation and owned string cloning. Full-field
  generic reference checks before/after timing. No Redis in this isolated test.
- Native: 64 trials, 32 baseline and 32 candidate, all eight mode/shape/writer cells,
  warmup+three measured samples each, complete 105-field physical types/values/
  multiplicities and ordinal set checked, raw unmanaged values checked too.
  Zero failures/conflicts/retries/discards. Baseline block precedes candidate.
- Versioned Makefile shared-library/CLI builds, Go 1.27.3, clean Git source;
  actual binary/source/helper/input hashes retained. No package/dependency change.
- One controller error after Software tests passed: system Python lacked redis
  for cleanup. Only cleanup+final gate were recovered using the existing benchmark
  virtualenv, with no test rerun or weakened assertions. All records retained.

The exact implementation/tests remain in
[candidate.patch](../../../../benchmarks/table-modes/ingest-buffer-2026-11-13/candidate.patch),
with the frozen candidate source ref retained. The evidence branch has no Go
changes. An independent agent and parent source review found no lifecycle or
semantic blocker; this is not human approval. Insertion and uncertain-outcome
resolution consume command encodings synchronously, retaining numeric ranges/
facts rather than the caller's Value matrix. Reuse clears references before the
next input request, including errors. Clearing does not mutate string bytes.

Actual Software API 8.2.6-25 / Redis 8.8.0 / Search 80808 were verified. Shared Software
fixture used effective 4x QPF, six Search workers, mnp/32, 4 GiB noeviction, verified
strict data TLS/RESP3 and 4 CPU / 8 GiB container limits. The loan returned empty/index-
free with all processes/clients closed and settings reverified. Parent final
teardown removed the owned container/network and generated TLS/test.env/settings/
fixture-env credentials while preserving the pre-existing container and all 31
networks. The [sanitized proof](../../../../benchmarks/table-modes/ingest-buffer-2026-11-13/cleanup-proof.json)
records completed cleanup. Zero generated-credential matches required redaction;
raw log hashes are unchanged. Operational artifacts/identifiers and credentials
remain private. No ClickBench.

[Complete report, distributions, attempts and reproduction](../../../../benchmarks/table-modes/ingest-buffer-2026-11-13/report.md).
Audit with `python3 benchmarks/table-modes/ingest_buffer_audit.py`.
See [intent](intent.md), [contract](spec.md), [plan](plan.md).

Evidence audits, Python syntax, relative links, exact generated-credential scan
(zero matches), patch applicability and scoped whitespace checks passed. The
frozen candidate.patch contains original diff context with leading spaces before
Go tabs; preserve its exact bytes/hash rather than normalize a source artifact.
Parent final evidence review passed at f5e773bb (plus the README-only follow-up
04fd4b26); no blocker was found in distributions, attempt disclosure, lifecycle,
source identity or unchanged production scope. This is agent review, not human
maintainer approval. Final shared-fixture cleanup proof is now attached.

Publication: [PR #429](https://github.com/redis/redis-adbc-driver/pull/429) is open
as evidence only. No merge, release or human maintainer approval is claimed.
