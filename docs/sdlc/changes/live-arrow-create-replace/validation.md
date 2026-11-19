# Validation and review

Date: 2026-11-18. Author: Codex. Status: local validation; PR checks pending.
See [intent](intent.md), [specification](spec.md) and [plan](plan.md).
PR: pending creation. No maintainer approval or release implied.

## Environment and checks

Redis Software 8.8.0, three shards, Search enabled. `make -C go redis-up`
and the owning checkout's `software-setup.sh --exec` wrapper verify active 4x
QPF, six effective Search workers, mnp routing, 32 connections and noeviction.
Operational identifiers, credentials and raw logs remain in ignored local build
outputs. No Community service, unsupported service, or ClickBench run was used.

- Unit-only `env -u REDIS_URI -u REDIS_SNAPSHOT_URI go test -count=1 -p 1 ./...`
  passed. Integration skips are expected in this invocation.
- Ordinary verified Software race run: `go test -count=1 -race -p 1 -timeout=10m
  -run 'TestLive|TestSQL.*Ingest|TestUnmanagedIngestError' -v ./...` passed,
  with REDIS_SNAPSHOT_URI unset. Thirty top-level tests passed, no test skips.
- Destructive fixture regressions run separately with REDIS_SNAPSHOT_URI set and
  REDIS_URI unset: new TestSnapshotLiveArrow tests plus existing unmanaged Arrow
  creation/append/replacement/source-option tests, under the race detector.
  Four existing restricted-principal permission subtests skip because dedicated
  denied-command principals are not configured. Final race run passed: 48
  top-level tests, including eight live creation/replacement tests.
- `make -C go build adbc-cli` passed on macOS arm64, including the native library.
- Documentation staging, strict MkDocs build and `scripts/check_docs.py` passed
  for all 45 public pages. Formatting and `git diff --check` passed.

## Review

Correctness/lifecycle: preserve upstream live conversion/cancellation/defaults
and acknowledged pipeline replies. New tests cover create and create_append,
explicit/generated/binary keys, exact large integers, text encoding, index
policy, empty replacement, schema replacement, discarded TTLs/unknown fields,
refused consent/scope, unknown delete/DDL replies and partial input writes.
Retain a building descriptor before submitting replacement index creation so its
schema/index identity is recoverable after a lost create reply.

An ordinary join regression exposed that column-based schema checks also compare
query-projected metadata. Replaced that check with an immutable registration
version changed on replacement; existing mixed live/managed joins now pass.
The new stale-writer check covers identical-schema replacement as well.

Ownership/security: reserve a generated index identity create-only, CAS replacement
metadata, never use DD or managed row publication/GC for source HASHes. Reuse
bounded inventory/unchanged-image deletes, scope/consumer/permission guards and
final key verification; keep arbitrary-client exclusivity an explicit operator
attestation. Failure remains partial and non-replaying, without rollback/resume.

Intent/evidence: no throughput claim, benchmark, merge or release. Remote checks
and any review feedback are tracked on the PR.
