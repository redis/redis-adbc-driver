# Validation and review

Date: 2026-11-18. Author: Codex (implementation and self-review; no maintainer
approval or release implied). See [intent](intent.md), [specification](spec.md)
and [plan](plan.md).

## Evidence

- Captured the failing regression before implementation: live append returned
  `managed writes cannot access live tables` without inserting the Arrow rows.
- Redis Software 8.8.0, three shards, Search enabled; `make -C go redis-up`
  provisioned the local fixture. The Software setup wrapper verified active
  effective 4x QPF and noeviction before each integration invocation. Credentials,
  management identifiers and raw logs remain local.
- Focused race checks passed with `go test -count=1 -race -p 1 -v -run
  'TestLive|TestSQL.*Ingest|TestUnmanagedIngestError' -timeout 8m ./...`, launched
  through the owning checkout's verified Software wrapper. The source checkout
  was isolated after a concurrent task changed the original checkout's branch;
  the test endpoint remained the owned fixture and no snapshot database was set.
- Live coverage includes reordered fields, exact large integers, binary keys
  and payloads, UUID defaults, multiple batches, missing values, schema/key/mode
  refusals, TTL and unmodeled-field preservation, unchanged registration/index
  and managed root, 4,096-row conversion slices, 128-row and byte-limited
  pipelines, duplicate keys in later slices, collisions after preflight,
  acknowledged replies after a collision, unknown replies without replay,
  cancellation, connection close, reader failure, schema drift and input release.
- Existing live SQL tests and ordinary managed ingestion/drop/truncate/temp
  regressions passed under the race detector. `TestLiveArrowSharedUnmanagedWriter`
  exercises the shared pipeline through existing unmanaged Arrow append with a
  dedicated prefix and application index. The broader unmanaged snapshot suite
  was not run: it requires a separate destructive `REDIS_SNAPSHOT_URI` fixture.
- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  passed; integration skips in this invocation are expected.
- `make -C go build adbc-cli` passed on macOS arm64, including the native shared
  library. A Python 3.13/PyArrow smoke test against that source build passed Arrow
  Table append, C-stream create_append, generated and explicit keys, exact
  integers, SQL/direct Redis visibility, no hidden fields, and managed temporary
  Arrow append. The smoke fixture was unregistered and its source keys removed.
- Documentation staging, `mkdocs build --strict`, `scripts/check_docs.py`, Go
  formatting and `git diff --check` passed. No performance claim or ClickBench
  run is part of this change.

## Self-review

Correctness/lifecycle: reuse existing conversion, defaults and constraints;
validate schemas before advancing input; release every owned slice/reference;
retain all positive acknowledgments in a submitted pipeline; stop on uncertainty
without submitting subsequent writes. Cancellation and close tests complete
without a leaked execution owner.

Ownership/security: live dispatch precedes managed admission, uses a separate
execution-local store and the execution-owned non-replaying transport. The
single-key insert script refuses collisions atomically. Prefix/key validation,
registration checks, and existing field encodings remain shared with live SQL.
No source replacement, managed publication/GC, index-policy changes, hidden
fields, or destructive registration operation was added.

Intent/evidence: append and create_append require an existing registration;
creation/replacement remain explicit exclusions. Documentation describes counts
as lower bounds after uncertain replies, and does not promise generated-key
results or statement rollback. No remaining actionable self-review finding.
Remote checks and any PR feedback are tracked on the PR.
Review: [PR #522](https://github.com/redis/redis-adbc-driver/pull/522).
