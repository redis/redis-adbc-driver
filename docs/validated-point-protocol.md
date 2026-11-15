# A bounded validated point lookup

Proposal, 2026-11-14, based on `b904cf8d` (protocol 17). No new Redis command or driver mode is implemented. Existing upstream discussions are [views #16180](https://github.com/redis/redis/issues/16180) and [server-managed tables #16238](https://github.com/redis/redis/issues/16238); this is a more concrete driver requirement for those discussions, not another issue.

## Implemented native step

The [measured candidate](../benchmarks/validated-point-protocol/README.md) implements a smaller driver-owned improvement: the existing published missing-row ZCARD and ZSCORE execute in one native pipeline, with unchanged command identities/keys, typed decoders, NoRetry and count-before-score failure precedence. Private-owned rows retain score-only verification. Only direct native Nil with no unaccounted outer failure proves absence. Baseline fault injection reproduced wrapped/joined Nil incorrectly exposing an older image; candidate rejects it. No Lua scope extension or new server API is involved. Saved round trips reproduce in frozen-binary ABBA; mixed timings do not establish a general latency gain.

The larger proposal below is not implemented. It remains gated on server-supported native authorization and bounded indirect-key/routing semantics.

## Where the dependency is

The [selected full native matrix](../benchmarks/oltp-query-efficiency/2026-11-14-final/report.md) has managed point-read p50 2.882 ms and redis-py p50 0.236 ms at one client; at four clients, 5.487 and 0.445 ms. Those figures measure full Python entry points. They do not establish how much one proposed operation would save.

| Current source | What the response determines |
|---|---|
| `go/snapshot.go: acquireSnapshotMode` | A fresh root and live server-side pin; starts the existing heartbeat/lifecycle. |
| `go/snapshot_metadata.go: validateSnapshotMetadataAndPin` | Committed manifest, metadata directory/map cardinalities, fresh pin validity. Existing reads share one pipeline. |
| `go/snapshot_metadata_cow.go: resolveSnapshotMetadataRead` | Root map selects bucket owner; bucket proof plus field lookup selects value owner; object presence/ref proof plus native GET returns the descriptor. Those latter three stages depend on prior responses. |
| `go/store.go: getTable` and `go/snapshot.go: validateSnapshotTable` | Descriptor identity/layout plus segment ownership, directories and native index evidence. |
| `go/snapshot_delta.go: fetchSnapshotVisible` | Newest-to-oldest physical row selection, tombstone suppression, exact identity and absence checks. |
| Statement/stream lifecycle | Fresh final lease checks, reader exhaustion and pin release; these must remain accounted for. |

The root-prefetch experiment moved an entire map into an earlier pipeline. Its mixed tail/concurrency results caused its removal. Reintroducing that probe, retaining a lease across statements or trusting cached native permission does not satisfy this proposal.

## Minimal useful primitive

Start with **validated descriptor resolution**, retaining current pin acquisition, row lookup and final validation. This addresses the dependent root/bucket/value chain without requiring Search calls or SQL interpretation in a server operation. The operation consumes a pinned root, the logical metadata suffix, protocol version, caller identity, pin token/namespace and explicit resource bounds. It returns the exact raw descriptor bytes and the identities/proof facts needed by the driver, or a typed terminal failure/definite unsupported response. No JSON decode/re-encode or floating-point conversion of returned descriptor data is permitted.

The server follows root slot → bucket owner → value owner, validates every identity before key construction, and performs the existing manifest/cardinality, bucket count/reference/directory and object presence/reference checks. The native descriptor GET authorization remains mandatory even if a server operation can internally read a broader metadata namespace. The server must retain pin validity throughout the operation or establish the existing equivalent protection against reclamation; a pin valid only at the start is insufficient if the operation can yield past its deadline.

A second phase may append **one exact managed point lookup** after the driver has decoded and validated the descriptor. Its explicit input lists every candidate physical row, segment directory and ownership record. It checks all supplied identities against the pinned descriptor rather than trusting caller-provided counts/prefixes. It stops at the first registered newest image or tombstone, preserves int64/NULL/empty/binary bytes, and returns an explicit absence outcome distinct from a missing registered row. It must not inspect older rows after a winner. This phase can be built independently; it must earn its value on multi-segment misses and tails rather than assume a one-segment hit improves.

A fully combined descriptor+row operation needs the server to understand protocol-17 descriptor layout and exact row-prefix derivation. That is a larger server-managed-table API, not a small Lua wrapper. Do not couple a generic Redis primitive to the driver's JSON layout without upstream agreement.

## Authorization and declared keys are implementation gates

All metadata and managed row keys share `{meta}` in the current format. Same-slot placement alone is not authorization. A regular EVAL key list or a broad namespace grant cannot be assumed equivalent to the existing native commands when selectors permit the commands/keys separately.

For the descriptor phase, future keys are selected by data read from the root and bucket. The API must therefore provide one of these explicit server-supported contracts:

1. An authorized root capability permits discovery, with per-discovered-key native command/key authorization under the caller, same-slot enforcement and auditable key extraction. The capability itself must have well-defined revocation, lifetime and namespace boundaries.
2. A two-step request first obtains the exact discovered key list, then executes the bounded validation/read using all keys declared explicitly. That saves less network latency but keeps the current declaration boundary. A server reply alone is never an authorization token, and the second request revalidates root/pin/ownership.

Neither capability is asserted to exist today. A client implementation may not manufacture an unbounded allowlist, access an undeclared key, combine independent ACL selectors into broader rights, or infer native GET permission from EVAL permission. For shared/restricted readers, preserve their reader namespace controls and terminal-loss semantics; initially return unsupported locally and use the current path. Do not fall back after a submitted operation with an unknown outcome. An explicit unsupported result must mean no data was read or side effect performed before choosing compatibility.

## Bounds and replies

Specify maximum request/response bytes, maximum key count, maximum field count, maximum work per call, and cancellation/deadline behavior before implementation. These are admission bounds: check a raw object's byte length/type before materializing or decoding it. The 256-bucket root limit is a cardinality limit, not a byte limit. A single arbitrarily long map value or descriptor defeats a count-only bound. Reject oversize inputs with a distinct definite response; do not return truncated data. Current 1 MiB batch targets and 64 MiB row ceilings are not evidence that a 64 MiB atomic read is acceptable.

For an initial prototype, use this deliberately narrow admission envelope; these are proposed limits, not measured optimal settings:

| Limit | Descriptor phase | Explicit-key point phase |
|---|---:|---:|
| Total encoded request | 64 KiB | 64 KiB |
| Declared/discovered physical keys | 16 | 32 |
| Individual derived key or metadata suffix | 1 KiB | 1 KiB |
| Raw descriptor bytes before decode | 1 MiB | 1 MiB |
| Candidate segments | N/A | 8, preserving the protocol's actual descriptor limit |
| Requested fields | N/A | 256 |
| Total encoded response, including framing | 1 MiB | 1 MiB |
| Native read/proof steps | 64 | 64 |

Never scan the whole root map to resolve one suffix: HLEN proves its bounded count and HGET selects its one fixed slot. Size checks must use server-internal length/type inspection before allocating the object reply, without requiring a broader caller command permission. Exceeding any limit returns a definite bound refusal with no pin mutation or partial data response. The caller may choose the existing path only when that native response unambiguously proves that no logical operation or mutation executed; unknown or composed errors remain terminal. Large valid tables/rows retain the existing supported path rather than inherit these experimental fast-path limits. Measure blocking duration and tail effects before proposing a server execution-time budget.

Responses distinguish found descriptor/row, logical absence, invalid protocol/layout, expired pin, unauthorized native operation/key, corruption, unsupported operation and admission bounds. Redis/native errors must remain directly classified; composed transport plus refusal outcomes fail. A lost read response is a failed statement, with ordinary bounded cleanup and no replay. Acquisition and release remain existing explicit operations in phase one; claiming one total round trip would be incorrect.

## Test protocol before production selection

| Probe | Required outcome |
|---|---|
| Root, bucket or value ID malformed; UUID alias points outside registered prefix | Reject before derived-key access. |
| Missing manifest/map bucket/value; zero/bad refs; mismatched HLEN/ZCARD/EXISTS | Fail closed; no zero-value proof or logical absence substitution. |
| Native GET denied; helper allowed; native commands allowed via different ACL selectors | Preserve each native authorization outcome; no combined-key selector escalation or silent data exposure. |
| Pin expired/revoked before or during operation; permission revoked between statements | Reject using current server state; no cross-statement permission or lease cache. |
| Shared binding/restricted reader; borrowed/shared view terminal loss | Existing path until separately proven; never upgrade to broader catalog authority. |
| Row ID >2^53, int64 endpoints, embedded NUL, absent field, empty string | Exact raw bytes and SQL types, distinguishing NULL, empty and absent row. |
| Newest tombstone/replacement, oldest hit, complete miss, missing registered HASH | Correct newest-winner result; tombstone stops; corruption is never absence. |
| Oversize map field/descriptor/row; huge caller field list | Admission failure before unbounded materialization; no partial successful image. |
| MOVED/ASK, migration, disconnect after submission, outer error plus success replies | Statement fails unless existing protocol proves a direct nonexecuted refusal; never replay uncertain reads or mutations. |
| Context canceled or connection closed during operation | Bounded completion/join and existing pin cleanup; no retained goroutine or Arrow allocation. |

Run the opt-in `TestSnapshotPointProtocolProfile` on an exclusively owned, policy-verified Redis Software fixture. It records seven SQL point scenarios on a pre-acquired execution transport and native command/pipeline categories with 16 samples each, after four warmups. It asserts exactly three overlay segments and checks every result. Its per-query timing excludes pool acquisition, transport owner registration and finish/return/drain because `statisticsHookedTransport` supplies one execution transport for the whole profile. This is partial execution attribution. Its fixed-order Go HGETALL control reads one independent application HASH with a known physical key. It performs no logical segment resolution, pin, managed ownership or tombstone validation and is not semantically equivalent to any managed scenario. It is a transport diagnostic, not redis-py or causal A/B evidence. The observed hook includes pool wait/transport/decode; it is not server CPU. Runtime identities, SQL literals, keys, command arguments, credentials and row values are absent from the emitted profile.

After an implementation exists, require interleaved baseline/candidate full native runs with raw redis-py controls at one and four clients. Publish complete fetch correctness, all p50/p95/throughput observations and refusal counts, not only point medians. Add longer overlay miss/tombstone and ACL/cancellation runs. Compare against both the selected #439 baseline and the unchanged raw controls; reject a median gain with materially worse tails or control-normalized regression. A numerical acceptance threshold should be set from repeated baseline variance before selecting the candidate, not after inspecting its result.
