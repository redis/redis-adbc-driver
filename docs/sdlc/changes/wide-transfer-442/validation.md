# Verification and review

Date: 2026-10-22. Baseline: `aab9612`. Author/reviewer: Codex (self-review).
Human review/CI: pending; no approval inferred.

## Checks performed

- `make -C go build` with `GOCACHE=/tmp/redis-442/go-cache`: baseline and both
  isolated patch binaries build with the normal version linker flag. All patch
  changes to production files were restored after their binaries were saved.
- Three complete cross-database matrices (wide public APIs, narrow public APIs,
  wide explicit-range control): each has 72 exact-value/multiplicity validations,
  including 12 warm-ups and 60 measured trials. Same source and server limits.
- Separate wide byte diagnostic: 12 validated executions, including six warm-ups;
  its proxy timings are excluded from direct timing claims.
- Two independent Go experiment screens: 16 validated executions apiece,
  including four warm-ups and 12 measured trials. Both failed the elapsed gate;
  neither is promoted. Total primary executions: 260 (50 warm-ups / 210 measured).
- `REDIS_REPLY_PROFILE=1 REDIS_URI=... go test -C go -run
  '^TestSearchReplyScanProfile$' -count=1 -v -cpuprofile=... -memprofile=...`:
  the existing read-only diagnostic passes, three scans of 100K rows. Fresh
  allocation/CPU evidence is retained separately from timed matrices.
- `python -m unittest discover -s benchmarks/wide-transfer -p 'test_*.py'`:
  six tests pass. Checks include exact BIGINT changes, dropped/duplicated rows,
  binary NUL/null values, spawned schema nullability, safe read-only HTTP retry,
  SQL bounds/quoting, unsupported load types before writes, exact bidirectional
  byte counts, and meter process shutdown.
- `python -m py_compile` for the new runner and meter passes. Evidence audit
  recomputes trial medians/counts, fixture/binary/runner identities and snapshot
  patch hashes. Local Markdown target checks and `git diff --check` pass.
- Three task-owned containers and their anonymous volumes removed. No Cloud
  resources created. Existing unrelated resources were not modified.

Two incomplete measurement attempts are retained separately and excluded from
primary comparisons: worker schema-nullability loss and HTTP keep-alive expiry.
Both have regression coverage and complete replacement matrices. Loading/smoke
runs are setup, not additional primary statistical evidence.

Full Go/ADBC/Cluster/TLS correctness suites were not rerun: supported driver
source is unchanged, and the experimental patches are not proposed as supported
driver features. They have immutable-fixture value validation only. Do not infer
their production semantic, security or deployment compatibility from this report.

## Review passes

1. **Correctness/lifecycle:** verified full multiset oracle, signed integer and
   temporal normalization, source schema preservation across spawned workers,
   owned connections per reader, context-managed readers/writers and process
   pools. Joined all persistent futures before connection cleanup on failures.
   Public Redis partition planning is timed; explicit-range generic execution is
   a separate control. Failed/retried runs are not silently combined.
2. **Security/ownership:** fixture loading is explicit and documented to replace
   only named benchmark tables in disposable servers. URI credentials are not
   written into results. Proxy binds only localhost and preserves endpoint auth
   without logging it. No new production ACL, storage, SQL or network behavior.
   EVAL_RO remains an unsupported patch with Cluster/ACL/atomicity limitations.
3. **Intent/design/evidence:** checked actual trial settings and APIs against
   the report; matched engine limits do not imply identical storage or internal
   work. First-batch sizes differ and are reported. Proxy times, cgroup CPU,
   lifetime RSS and allocation diagnostics are qualified. Unsuccessful screens
   stay visible. Specialized typed appending/overlap and a supported typed server
   export remain open under #442; no speedup claim or issue closure is proposed.

No remaining actionable finding in the scoped benchmark/report deliverable.
This self-review is not maintainer approval. Main residual risks are portability
of the Docker cgroup-v2 sampler, the small local fixture, different engine/API
work, and lack of production validation for the retained experimental patches.
