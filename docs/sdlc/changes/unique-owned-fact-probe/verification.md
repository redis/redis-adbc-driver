# Verification

Date: 2026-11-24. No live SQL, native library build, Redis/container operation or
full benchmark suite was run for this change. Endpoint-free mocked transport is
not Redis compatibility evidence.

Fresh decode plus join allocation control uses identical raw HASH input, six
fact fields (including exact DECIMAL values), two singleton dimensions, physical
row ID and a 17-field emitted map. It includes actual decodeRow work on every
iteration, so map growth and decimal decode allocation are counted. Fanout is
covered separately and does not reuse the fact map.

Go 1.27.4, darwin/arm64, Apple M5 Max; GOMAXPROCS=2, GOMEMLIMIT=768MiB.
Three 300ms samples, benchmem:

| Decoder plus join | B/op | allocs/op | Median ns/op |
|---|---:|---:|---:|
| Fresh leaf control | 5,592 | 58 | 3,111 |
| Owned fact, normal growth | 4,872 | 52 | 2,791 |
| Owned fact, bounded reserve | 4,216 | 49 | 2,387 |

Timing is a local microbenchmark, not a full-query or native speedup claim.
Initial isolated-prototype race tests passed (4.278s), including singleton transfer and physical-ID
overwrite, duplicate fanout independence, late missing match, pre-cancel source
immutability, shared-safe default orchestration, bounded capacity compatibility,
native 9,001-row multi-page residual/pruning and consumer failure/grant cleanup,
and existing parallel source-error/cancel/panic tests. The native test clears each
emitted map and checks exact ordered progress and unchanged raw/dimension maps.

All tests unset both REDIS_URI and REDIS_SNAPSHOT_URI. Initial isolated-prototype endpoint-free `go test -count=1 -p 1 ./...` passed
(root package 29.950s). These checks and allocation controls predate clean-public
extraction; they do not establish that the extracted public head passed.

The clean-public change is stacked on public #620 and targets main. #620 supplies
the coordinator ownership marker/representative optimization. Source extraction
copies only this allocation change, its focused tests and these SDLC records;
it introduces no private investigation ancestors. Current clean-public focused race checks passed (4.901s); all endpoint-free unit
packages passed (root package 30.678s), with GOMAXPROCS=2, GOMEMLIMIT=768MiB,
-p 1 and -count=1. Formatting, diff and local SDLC link checks passed. An
independent agent static review found no demonstrated blocker; that review is
not human maintainer approval. No live query has tested
this exact public delta, and no Q4 resolution or whole-query speedup is claimed.
