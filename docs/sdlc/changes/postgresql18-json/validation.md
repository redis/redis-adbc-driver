# PostgreSQL 18 JSON validation

Date: 2026-11-10. Author: Codex. Requestor: Albert Wong.
Status: eight targeted #98 cases implemented; broader issue remains open.
Source: base `9bc9d6200efd952febef906b3b9cc0ab43bb08af` plus files identified by
SHA-256 in [sanitized results](results.json). Depends on
[PR #375](https://github.com/redis/redis-adbc-driver/pull/375).
See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Result

The unchanged, fully fetched corpus reports **597/651 scored passes (91.71%)**,
**54 failures**, two allowed extensions, two explicit exclusions and **zero
infrastructure errors** (655 total cases). All **589 previous passes remain
passing**. The eight gains are `issue-254.json:row-1` and `row-3` through `row-9`.
Corpus SQL, skips, exclusions and allowlists are unchanged. The retained #359
inventory now has 145 passes and 43 deferred failures. The original eleven #360
cases still fail. The full runner returns mismatch exit 1 (`make` exit 2);
this is a curated pass rate, not a passing full suite or feature-coverage claim.

Reference: pinned PostgreSQL 18.6 linux/amd64 with C collation. Target: Redis
Software / Redis 8.8.0, three shards, verified active 4x QPF, six effective Search
workers, mnp routing, 32 connections and noeviction. Each live command entered
through `software-setup.sh --exec` after `make -C go redis-up`. A dedicated
fixture kept test data isolated; operational logs and endpoints remain private.
The corpus retains REL_16_15 source provenance and is compared to the live 18
reference; it is not PostgreSQL's complete regression suite.

## Checks and review

- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: passed. Redis-dependent
  skips in this unit-only run do not establish live compatibility.
- `make -C go pgdiff-test`: 36 harness tests passed, including exact canonical
  Arrow JSON extraction, SQLSTATE and composite comparison checks.
- Verified Software `go test -count=1 -p 1 -run 'Test.*JSON|Test.*Extension' .`:
  passed. Covers SQL storage/catalog identity, exact JSON text and large numbers,
  JSONB parameters, Arrow ingestion/fetch round trips, arrays, invalid-write
  rejection and existing extension-policy behavior. This is a focused suite,
  not the complete Software package sweep.
- Separate `--corpus pgdiff/adversarial/json98`: **39/39** PostgreSQL comparisons
  passed. Covers NULLs, duplicate keys, exact numbers above 2^53, numeric equality
  and hash joins/grouping, container ordering, containment, path mutations,
  negative indexes, signature/root errors, SRFs and `?` parameter ambiguity.
  These cases remain outside the scored corpus and do not change its denominator.
- Full `make pgdiff --report ...`: 597 passes as above; case-by-case comparison
  against the preceding #361 report confirms zero regressions.
- `gofmt` and `git diff --check`: passed. Triage accounting and evidence hashes
  checked. Raw reports and management outputs remain ignored/private.

Review followed [REVIEW.md](../../../../REVIEW.md): correctness/lifecycle,
ownership/security and evidence. Native JSON uses existing authoritative HASH
storage and write admission. No index, ACL or snapshot protocol is relaxed.
Review found missing exact JSONB hash/comparison handling and incorrect raw
JSONB SRF text/error mapping; regressions were added and checks rerun after fixes.
Agent review is not maintainer approval.

Three old expectations changed after independent PostgreSQL 18 verification:
`to_json(float8)` has logical JSON type, CTAS preserves JSON identity, and nested
JSON aggregates embed typed JSON arrays rather than quoting them. A raw Unicode
round-trip comparison now explicitly casts to text because plain JSON has no
PostgreSQL equality operator. Assertions remain active; no skips were broadened.

## Limits

See [spec](spec.md) for remaining JSON scope and Arrow/partition constraints.
The JSON corpus category has no remaining failure, but #98 remains open for its
broader API wishlist. The other 54 corpus failures remain visible. No performance,
full PostgreSQL compatibility or SQL conformance claim is made.
