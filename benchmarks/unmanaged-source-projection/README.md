# Uninstalled exact-source HASH projection prototype

Date: 2026-11-14. Driver baseline `d7c0978f` (merged #440). This is an upstream
library primitive and executable contract/repro. **No driver runtime path or
installed Search capability is changed.** No performance gain is claimed.

The [upstream pin/dependency](upstream-source.json), [reviewable patch](upstream-rlookup-source.patch),
[shared production core](source_hash.rs), and [native adapter](source_hash_native.rs)
form one prototype. Apply the zero-context patch to the exact pinned checkout
with `git apply --unidiff-zero /path/to/upstream-rlookup-source.patch`.
The files retain the upstream Search licensing terms;
placing the patch in this repository does not relicense them.

## Why this boundary

The filtered aggregate benchmark still performs about 10,000 authoritative HASH
reads per query. Allocation tuning did not reduce fanout and was rejected.
LOAD aliases are not a guarantee of source bytes: the pinned
[lookup](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/redisearch_rs/rlookup/src/lookup.rs#L487)
can reuse sorting-vector values. The selected-field
[HASH loader](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/redisearch_rs/rlookup/src/load_document/hash.rs#L302)
can coerce numeric schema fields through double. Full LOAD* can retain raw source
values, but returns unrelated fields; an unmanaged HASH may gain arbitrarily
large unregistered fields after adoption. A narrow-table heuristic cannot prove
that LOAD* will stay narrow. Existing full/grouped LOAD* paths are unchanged.

The raw source API deliberately has no schema or sorting-vector parameter.
The adapter opens a native HASH once, uses length-aware field/value RedisStrings,
and never enumerates unselected fields. The same core enforces presence/type,
required authorization/checkpoint gates and all-or-error result publication.
It supports zero selected fields without confusing an existing HASH with a
missing source. Source reads remain live per native locked row observation;
this does not create unmanaged statement snapshots.

Field/request limits are checked before opening the source. Selected-byte limits
admit the accepted result after native get returns an owned RedisString. They
are not a peak-allocation bound: one oversized selected value may materialize
before rejection. The native entry point is unsafe with an explicit context,
thread/lock and non-yielding/non-mutating callback lifetime contract. Debug output
redacts projected source values.

## Deliberately missing production integration

The patch exports Rust rlookup helpers only. It does not implement query syntax,
planner wiring, a real current-user ACL gate, coordinator/cursor serialization,
warning/fatal-error propagation or Software packaging. In particular, OpenKey is
not treated as ACL authorization. The generic callback tests do not establish
actual Redis RBAC or cancellation integration. The existing loader's
[error path](https://github.com/RediSearch/RediSearch/blob/33d90342bc9c601d904fb58446f8bda4eaba5011/src/result_processor.c#L1180)
needs an explicit integration decision rather than silently dropping a fatal
source error. No fake LOADSOURCE command or capability probe is added.

These prerequisites must be implemented and validated before the driver can
consume a new server primitive. Exact signed-integer reducers remain a separate
extension of the existing [Search #12334](https://github.com/RediSearch/RediSearch/issues/12334)
request. No duplicate issue/comment was submitted. Server-side projection could
remove client read commands while still performing the underlying HASH reads;
its server cost and native SQL latency remain unmeasured.

## Verification and reproduction

[Rust verification](checks.json) passed using an isolated official SHA-verified
Rust 1.100.0 toolchain: all seven adversarial core tests passed (zero failures or
ignored tests), and the native adapter type-checked against the real pinned
redismodule-rs dependency. The core tests compile the exact proposed production
file, not a parallel reference algorithm. Applied patch files match those
compiled source hashes. No full Search workspace, module or engine integration
build/test is claimed.

The [adapter harness](adapter-check/Cargo.toml) uses the pinned real native API,
with a checked-in dependency lockfile. Its platform binding-generation features
are recorded in the manifest. The coordinator also completed the [Linux Software repro](live-evidence.json),
using the frozen baseline native driver/CLI; see [live provenance](live-provenance.json).
All seven SQL queries fully fetched and validated physical Arrow INT64/STRING
arrays, exact values and NULLs: 23 projection rows and three aggregate rows.
Independent raw HASH controls, live edits, deleted/non-HASH omission, application
HASH/index ownership after DROP and owner-checked cleanup all passed.

Ordinary aliased LOAD lost numeric precision for three of eight adversarial
source cases: signed-int64 maximum, 2^53+1, and -(2^53)-1. Full LOAD* retained the
selected source bytes but returned 131,230 value bytes (document keys excluded)
when the three selected public fields totaled 126 bytes; each HASH also held an
unregistered 16 KiB field. These are synthetic payload counts, not measured
network traffic, server CPU or a SQL speedup. The existing driver returned all
exact values correctly; this is an upstream API opportunity, not a newly found
driver wrong-result regression.

The all-selected-NULL HASH and a missing key produced identical HMGET vectors;
separate native TYPE controls distinguished them. That does not make the two
separate observations an atomic projection/presence primitive. The proposed
native helper's single held handle is designed to supply that distinction.

The repro client was Linux/Debian bookworm ARM64, Python 3.14.10, with a 4-CPU/
4-GiB client container sharing the Software TLS network namespace. The verified
server container retained 4 CPUs/8 GiB and active 4× QPF. This path does not
traverse the macOS published-port forwarding used in earlier diagnostic rounds.
No latency comparison is made across those paths. Actual driver/CLI/harness
hashes, package versions, source and cleanup metadata are retained in the live
records. The prototype remains uninstalled and untested inside the Search engine.

```sh
source_projection_test_dir=$(mktemp -d)
rustc --edition=2024 --test benchmarks/unmanaged-source-projection/source_hash.rs -o "$source_projection_test_dir/source-core-test"
"$source_projection_test_dir/source-core-test"
rm -rf -- "$source_projection_test_dir"
cargo check --manifest-path benchmarks/unmanaged-source-projection/adapter-check/Cargo.toml --lib
```

The [adversarial repro](repro.py) requires a dedicated initially empty Redis
Software fixture, a freshly verified 4× QPF gate, strict TLS/RESP3, and a frozen
current-baseline driver/CLI. It checks int64 extrema/adjacent values, NULL/empty/
actual NUL, source changes, deletion/type replacement, complete Arrow types and
values, raw native controls, and application ownership after DROP. It separately
records ordinary aliased LOAD byte mismatches and LOAD* unselected data bytes.
Those existing APIs are observations, not an implementation of the prototype.
The script performs no SQL timing and makes no client/server CPU or speedup claim.

Read [SDLC intent](../../docs/sdlc/changes/unmanaged-source-prototype-20261114/intent.md),
[contract](../../docs/sdlc/changes/unmanaged-source-prototype-20261114/spec.md),
[plan](../../docs/sdlc/changes/unmanaged-source-prototype-20261114/plan.md), and
[validation](../../docs/sdlc/changes/unmanaged-source-prototype-20261114/validation.md).
