# Validation

Date: 2026-11-19. Author: Codex. Status: local checks passed. PR: [#533](https://github.com/redis/redis-adbc-driver/pull/533).
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

Documentation only. Reviewed against the merged live Arrow guide and dispatch
in go/live_ingest.go. Ownership remains application HASHes/driver indexes, and
source-build scope remains explicit. No runtime behavior or test fixture changes.
Checks passed: documentation staging, strict MkDocs build, and
scripts/check_docs.py verified all 45 public pages and their links/publication
scope; git diff --check passed. No runtime tests were needed for this prose-only
change. Self-review found no remaining actionable ownership/contract discrepancy;
this is not maintainer approval. Remote checks are tracked on the PR.

Follow-up review: reordered comparison cells with their values intact to
Managed (default), Live, Unmanaged; matched the README explanation order and the
detailed guide's introductory/use-case tables. Documentation checks rerun.
Native access wording was checked against docs/native-client.md,
python/adbc_driver_redis/native.py and the read-only native ABI dispatch. No
native writes or application-primary-key lookup claimed. The v0.0.29 tag contains
the wrapper/ABI, and the released macOS arm64 wheel was downloaded and inspected:
NativeClient is packaged and its bundled library loads and exports both
RedisNativeReadV1 and RedisNativeReadFreeV1. This verifies packaged availability,
not a new Redis integration test or an all-platform runtime validation.
