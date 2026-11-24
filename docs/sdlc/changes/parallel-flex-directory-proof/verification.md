# Verification

Date: 2026-11-23. Status: expanded synthetic race and unit checks passed.
See [intent](intent.md), [specification](spec.md), [plan](plan.md).

Tests use synthetic ordinary/Flex native-directory transport and in-memory rows;
no endpoint is contacted. A synthetic selective source sized one row above the installed candidate cap checks one
coordinator COUNT0, no candidate fetch, two exact-directory workers, full predicate
recheck and all duplicate results. Admitted selective queries remain unconsumed;
ambiguous proof failures cannot fall back. Other tests cover exact count/byte
boundaries, malformed/warning/native-capability replies, later-source validation
and overflow saturation, alongside existing complete-candidate and parallel
lifecycle regressions.

One initial fixture used mismatched synthetic index/data identities; another
used an overlay layout that intentionally disables predicate compilation. Both
were corrected to represent the intended published plain single-source layout.
These were fixture corrections, not weakened assertions. No native library
build, live SQL, container operation or benchmark run was performed.

Original integrated-source focused race checks passed (6.194s):

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -race -count=1 -p 1 \
  -run '^(TestCompleteFlex|TestCompleteSnapshotBudget|TestFlexComplete|TestParallelStar|TestStar|TestUnionStar|TestDeferredUnionStream)' .
```

All packages passed the same endpoint-free `go test -count=1 -p 1 ./...`
command (root package 29.260s). Applicable endpoint integration skips are not
native compatibility evidence. Formatting, diff and local SDLC-link checks pass.

## Clean public-branch checks

The original PR612 public branch retained the then-installed 65,536-candidate cap. Its byte-boundary
fixture uses longer synthetic prefixes and explicitly asserts that the byte
threshold is reached before the count cap. The earlier short-prefix fixture
correctly hit the lower count cap first; no production budget or assertion was
weakened.

Endpoint-free focused race checks including the standalone HASH transport
regression and count/byte boundaries passed (4.205s):

```
env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB \
  go test -C go -p 1 -race -count=1 \
  -run '^(TestParallelStar|TestFlexComplete|TestCompleteFlex|TestCompleteSnapshot|TestSnapshotPage|TestStar|TestUnionStar|TestDeferredUnionStream)' .
```

All endpoint-free packages passed on the clean public branch using
`env -u REDIS_URI -u REDIS_SNAPSHOT_URI GOMAXPROCS=2 GOMEMLIMIT=768MiB go test -C go -p 1 -count=1 ./...`
(root package 28.587s). Integration skips without an endpoint are expected;
these checks do not establish Redis Software compatibility. Formatting and
`git diff --check` passed. The earlier integrated-source timings above are
historical checks, separate from this public-branch verification.

Published for human review as [PR612](https://github.com/redis/redis-adbc-driver/pull/612), stacked on PR607.
