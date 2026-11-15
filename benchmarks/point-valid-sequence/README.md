# First-valid response wait after native index proofs

Date: 2026-11-14. Source: `7fc608ea` plus only `go/snapshot_point_valid_profile_test.go`; no production change. [Frozen diagnostic source/binary and run gate](provenance.json), [complete sanitized observations](profile.json), [summary](summary.csv), [SDLC](../../docs/sdlc/changes/point-valid-sequence/intent.md).

The controlled native sequence reproduces the earlier slow first lease validation without SQL execution inside the measurement. With the same real pin, immutable three-segment descriptor and Redis Software fixture, the first standalone `valid` reply after three ordinary FT.INFO/ZCARD proofs takes approximately 12.6–13.6 ms; following validations take approximately 0.26–0.29 ms. Zero or one proof does not show that delay.

| Preceding proof sequence | First valid before proofs, median ms | First valid after proofs, median ms | First valid socket-read elapsed after proofs, median ms |
|---|---:|---:|---:|
| None | 0.225 | 0.242 | 0.214 |
| One index, execution client | 0.289 | 0.259 | 0.246 |
| Three indexes, execution client | 0.243 | 13.555 | 13.536 |
| Three indexes, maintenance client | 0.245 | 12.585 | 12.565 |

All valid-call main-pool waits were zero, as were observed main/pipeline proof-pool waits. Both clients report a dedicated pipeline pool: using the same client does **not** imply using the same physical connection pool. Every measured valid call observed four RESP reply bytes and 433 request bytes. The request/reply data and native script keys were unchanged; no full-body script fallback or caller replay occurred.

The long interval is therefore observed while reading the tiny native response, rather than waiting to acquire a client connection. This does **not** identify server CPU, the precise transport/proxy/server scheduling cause, or a production SQL latency improvement. Socket counters are connection-scoped, can include concurrent maintenance I/O, and are not an additive decomposition of query wall time. Native request hook time also includes client/transport work. This diagnostic does not time the normal per-statement transport lifecycle.

Read-only SLOWLOG was available with a 10,000-microsecond threshold, but returned no threshold-selected sample during these sequences. That is unavailable server-duration evidence, not zero server time. No SLOWLOG setting or server configuration was changed. The profile retains explicit availability, threshold and omission metadata without command arguments, keys, SQL, client identities or operational identifiers.

The test alternated forward/reverse order across four arms for eight rounds. Each arm issued three fresh server lease validations before and after zero, one or three exact native segment proofs. The ordinary proof helper checked real index readiness and directory cardinality; the pin retained its native ownership/lifetime checks. Normal SQL full-fetch/type/result oracles ran before and after the helper diagnostic and retained their original proofs. They checked int64 extrema, values above 2^53, SQL NULL, empty strings, deletion visibility and exact remaining rows. All 32 sequences / 192 fresh valid requests passed; the test reported 0.41 seconds, zero failures/skips. No SQL throughput, p95 confidence or general latency claim follows from this bounded helper test.

The single diagnostic binary and source were frozen before measurement. The coordinator verified active/effective 4x QPF, six Search workers, mnp/32 connections, 4 GiB/noeviction and strict TLS/RESP3 on the exclusively owned Software fixture. Go 1.27.3 / GOMAXPROCS 4 / telemetry disabled; production workers remained enabled. Sibling builds/live work were paused. Every process/client/worker closed and the harness cleaned its database/indexes before fixture release. The coordinator's [end-of-round environment observation](../performance-round2/2026-11-14/environment.json) describes the same fixture at the end of the round; it supplements this run's original gate and source/binary hashes.

Reproduce the numeric summary offline:

```sh
python3 benchmarks/point-valid-sequence/summarize.py
```

A useful next experiment separates three FT.INFO-only requests from three/six ZCARD-only requests, with the same routes, typed oracles and server-duration caveats. The current experiment deliberately stops before that expansion. A public/native SQL benchmark is still needed before selecting any later runtime change.
