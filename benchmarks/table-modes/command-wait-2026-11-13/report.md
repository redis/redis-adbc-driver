# Command wait isolation: evidence only

Adding a small Redis request or simply waiting before release moved most of the
observed wait out of the release subspan. This occurred with PING and TIME on
both the same and different actual client TLS connections. Requested 20 ms pure
pacing also reduced release time, but made the whole dense job slower. These
controls do **not** justify a production pacing/probing change or identify server
CPU, queueing, TLS processing or a TCP mechanism. Production Go/modules/default
tests stay identical to baseline `06c76c0b`; the exact diagnostic source/tests are
archived unapplied in [candidate.patch](candidate.patch).

## Seven fixed jobs and complete measurements

All seven jobs passed complete 20K target-row value checks (exact BIGINT/payload)
and the unrelated table's COUNT/MIN/MAX=20000/1/20000. The unrelated aggregate is
not a full unrelated-row fetch. Each job retained exactly 317 normal successful
releases: 314 copy, one index and two install; **all 2,219 observations** passed
strict per-record validity, physical-connection, one-Read/Write and zero release
pool-miss/timeout checks. There were no failed/skipped attempts or removed samples.
Focused unit and race each passed 14 cases including subtests, zero skips/failures.
Every result, all distributions, transports, phase counts and raw-log digests are
in [results.json](results.json). These are seven diagnostic jobs, not 2,219
independent throughput repetitions.

|Fixed job order|Prelude median ms|Release median ms|Paired total median ms|Paired total sum s|Whole job s|
|---|---:|---:|---:|---:|---:|
|control-before|0.000|16.971|16.971|5.106|7.001|
|same-ping|19.059|0.292|19.378|5.503|7.407|
|separate-time|13.145|0.289|13.468|4.955|6.869|
|pacing-20ms|22.023|0.452|22.444|7.197|9.214|
|separate-ping|11.275|0.286|11.665|4.802|6.669|
|same-time|18.741|0.285|19.101|5.446|7.372|
|control-after|0.000|18.484|18.484|5.262|7.169|

The paired total is computed from each record's prelude + release; its median
is not the sum of separate medians. TLS Read is contained in release time and
must not be added again. Whole-job time includes the prelude, normal phase work
and observer bookkeeping, but excludes fixture creation and final SQL validation.
No-prelude controls use the same new telemetry as interventions; they are fresh
controls rather than byte-identical repetitions of the earlier archived profile.

Both bracketing control jobs retained millisecond-scale release waits, with
medians 16.97/18.48 ms and whole jobs 7.001/7.169 s. PING/TIME interventions had
11.28–19.06 ms median prelude waits and 0.285–0.292 ms median releases, across
same/different observed connections. Their whole jobs ranged 6.669–7.407 s.
There is one ordered job per intervention, so the lower separate-client job
elapsed values are not an established throughput gain or a request-type ranking.

Pure pacing requested 20 ms; actual median was 22.023 ms. Median release fell to
0.452 ms, but paired total median was 22.444 ms and the whole job 9.214 s, slower
than both controls. This directly shows why shrinking only the release subspan
would be a misleading improvement claim. The experiment supports sensitivity to
preceding elapsed time/work; no exclusive opcode or client-connection explanation
fits all arms. It does not prove a common cause with the fetch-page regression.

## Intervention, timing and connection controls

The frozen order was control-before, same_ping, separate_time, pacing_20ms,
separate_ping, same_time, control-after. Before any test/timing, parent source
review added pure pacing based only on old five-millisecond worker pacing and
INFO wait-shift evidence. No new measurement motivated that amendment. No order,
parameter, guard or source was tuned after results.

The actual release script, lease checks, pin lifecycle and cleanup are unchanged.
The prelude runs before the original release hook clock. Probe errors are recorded
without replacing the real release return or requesting a cleanup retry. Unit
regressions exercise failure privacy/outcomes and canceled/successful no-client
pacing. Probes use independent 250 ms contexts; pacing respects the original
context. Both still consume part of the already-created original release deadline.
This is an intentional bounded intervention, not a transparent observer.

Both client pools are explicitly warmed before capture when separate clients
are needed. Actual local dial ordinals, global across the two profile clients
within a job, verify same/different physical TLS connection relationships. They
are not endpoints, descriptors, packet identities or server-internal/backend
connection IDs; ordinals restart in each test process and cannot be compared
across jobs. Normal pre-capture acquisition opened additional connections, all
retained in transport metadata. Every observed release had no pool miss/timeout.

Counters wrap the TLS plaintext side, preserving the native NetConn chain. Their
Read/Write counts and bytes are not packet counts. Every dial verified native
TLS certificate chains; actual protocol version was TLS 1.3. The portable Go API
has no read-only TCP_NODELAY getter, so tcp_nodelay_known=false marks that flag
**unobserved**; the companion false placeholder is not an observed disabled flag.
No socket option, TLS/security policy, host networking or server scheduling was
changed. No packet capture or server trace was collected; backend routing/queue
and TLS processing were not separated. No unavailable-permission claim is made.

## Bounded capture follow-up: capability available, correlation not established

After the seven jobs completed, parent requested a capability-only inventory for
one possible encrypted-port trace. Default non-root exec lacked effective NET_RAW.
A separately authorized root process inside the same owned disposable container
had existing NET_RAW; a protocol-zero AF_PACKET socket opened and closed without
receiving frames, and its temporary socket accepted SO_TIMESTAMPNS. This checked
option availability, not delivery/format of actual packet timestamps. No host sudo,
capability addition, installation or host/server network change occurred.

Executable lookup found no tcpdump/tshark on the default container exec path;
root Python library/module lookup found no libpcap, dpkt or scapy. Existing release telemetry
contains elapsed spans and job-local ordinals, without private endpoint/wall-clock
write tags. Published-port forwarding prevents assuming client-side and container
flow identities are identical. A trustworthy bound would need new private flow /
dial-write-read wall-clock telemetry, clock calibration and TCP/TLS framing or
segmentation ambiguity handling. Those were not established within the bounded
minimal-instrumentation supplement. Parent approved stopping before capture.

[trace-capability.json](trace-capability.json) retains sanitized capability facts
and private inventory hashes. No packets were captured, no clock calibration or
additional workload was run, and no server-ingress/egress timing or permission
blocker is claimed. Existing seven-job source, observations and conclusions are
unchanged. Raw interface/process/capability inventory stays private.

## Environment, reproduction and cleanup

Measured clean source `dc6cceb5c9304a1f2bad740ec43e00cb162dff3b`, baseline
`06c76c0b68258862d091487c3eb280b961f4fb8d`; source `0408724d` was not executed.
Go 1.27.3, GOMAXPROCS=4, packages sequential and test caching disabled. Race
instrumentation applies only to the focused race test, not diagnostic timings.
[results.json](results.json) records actual sanitized environment and fixed source,
patch/controller/resource-inspection/private-log hashes.

Redis Software API 8.2.6-25 / Redis 8.8.0 / Search 80808, active/effective 4x QPF,
six workers, mnp32, one 4-GiB noeviction RAM shard. Data TLS certificate/hostname
verification and RESP3 stayed enabled. Root provisioned through make redis-up;
each job ended empty/index-free with effective configuration reverified. The
same owned container/image and four-CPU/eight-GiB limits were checked at loan
boundaries; no during-job resource polling was added. Fixed local image identity
and operational artifacts remain private. Host Apple M5 Max, 18 logical CPUs,
36 GiB, macOS 26.7.3 arm64. This is one ordered local shared-host experiment;
bracketing controls do not eliminate server/cache/order or host activity effects.

See [reproduce.md](reproduce.md) for exact commands/guards. Reproduction must use
a new owned verified Software fixture and actual run identities/settings. Do not
replace old measurements or use another distribution. Audit without Redis or Go:

```sh
python3 benchmarks/table-modes/command_wait_audit.py
```

All subprocesses/clients ended and final loan return was empty/index-free with
configuration verified. The parent completed final shared teardown: owned
container/network and generated TLS/test environment/settings/fixture credentials
removed, pre-existing container and 31 networks preserved. See the
[sanitized proof](cleanup.json). Generated-credential scans found zero matches;
raw log hashes remain unchanged. Raw operational data/IDs, credentials and license
files stay private. No ClickBench was run. Parent source and final artifact reviews
at `6366fbd3` / `80da7b28` found no blockers; these are agent reviews, not human
approval. The trace follow-up stopped before capture as explicitly approved.
