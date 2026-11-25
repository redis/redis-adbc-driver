# Isolating the response wait after native index proofs

Date: 2026-11-14. Runtime source `d7c0978f` (merged PR #440). These are
single-source native helper diagnostics, not Python/ADBC query latency, saturated
capacity or server CPU. [Source/binary and environment provenance](provenance.json),
[SDLC](../../docs/sdlc/changes/point-valid-isolation/intent.md).

**The response-wait cliff is topology-dependent.** It reproduces outside the
driver in Go and RedisPy on the macOS host/published-port route, but disappears
with the same Go source and unchanged Software server when a Linux client shares
the server's network namespace. Client OS and route changed together; the exact
host/network mechanism remains unproven. No fixed reply-grouping driver workaround
is selected. See [independent topology controls](#independent-client-and-topology-controls).

Pipelined FT.INFO responses reproduce the earlier delay even when the next
command is PING. Three ZCARD or six ZCARD replies do not. Three serial FT.INFO
requests do not. Repeating the same index also reproduces it: different index
identities and the lease-validation script are not necessary for this observation.
The delay remains socket-response waiting; the precise server/proxy/transport
mechanism is not established.

## Complete isolated observations

Version 1 retains [all 160 sequences](v1-profile.json.gz), its
[executed source](executed-isolation-v1.go.txt) and [summary](v1-summary.csv).
It added FT.INFO-only/ZCARD-only, serial and repeated-index arms to the prior
diagnostic. Version 2 adds one/two-response groups, grouping into chunks, complete
sequence elapsed time and proof reply-byte counters. Its
[all 256 sequences](v2-profile.json.gz), [executed source](executed-isolation-v2.go.txt)
and [summary](v2-summary.csv) remain separate. No version-2 annotation is applied
to version-1 measurements. Together all 416 sequences / 2,496 fresh PING or valid
requests passed, including exact native proof and complete typed SQL oracles.

Selected version-2 medians (milliseconds, eight observations per cell):

| Preceding sequence | First valid after | Complete proof + first valid | Complete sequence | Proof reply bytes |
|---|---:|---:|---:|---:|
| None | 0.221 | 0.224 | 1.401 | 0 |
| One FT.INFO in one pipeline | 0.241 | 0.534 | 1.709 | 2,962 |
| Two FT.INFO in one pipeline | 0.232 | 0.569 | 1.712 | 5,866 |
| Three FT.INFO in one pipeline | 13.068 | 13.529 | 14.820 | 8,784 |
| Three FT.INFO, chunks of two | 0.240 | 0.885 | 2.042 | 8,784 |
| Three FT.INFO/ZCARD pairs in one pipeline | 15.704 | 16.175 | 17.390 | 8,796 |
| Three pairs, chunks of two | 0.241 | 0.858 | 2.040 | 8,796 |
| Three pairs, chunks of one | 0.231 | 1.104 | 2.253 | 8,796 |
| Three pairs, six serial commands | 0.215 | 1.676 | 2.820 | 8,796 |

Complete sequence means three fresh downstream calls, the proof sequence, then
three more downstream calls. Proof + first valid includes command construction,
all exact reply checks and the first fresh native validation. Neither is a normal
SQL statement measurement. All PING variants, tails, socket counters and raw
observations remain in the CSV/JSON, including warm connection pre-requests.
No causal size threshold or arbitrary-schema speedup follows from this small
fixture. A wider index can return more bytes for fewer FT.INFO commands.

All observed main/dedicated-pipeline pool waits were zero. Both clients have
dedicated pipeline pools; using the same client does not prove that commands used
the same connection. Existing diagnosticConn read counters are connection-scoped
and may include concurrent maintenance I/O. Native hooks include transport/client
work and do not measure server execution or CPU. No server/SLOWLOG setting changed.

## Resource control

Read-only [CPU counter deltas](v2-cpu-counters.json) around the complete version-2
subprocess recorded zero `nr_throttled` and zero `throttled_usec` increases. The
same container had a four-CPU quota and 8 GiB memory; Docker's VM reported 18 CPUs
and approximately 25.4 GB memory. The delta interval includes SQL fixture setup
and cleanup and is not aligned per sequence. Historical cumulative throttle
counters remain private; they are not evidence of throttling during this run.
Other host/server/network scheduling explanations remain unproven.

Every live invocation was gated on active/effective Redis Software 4× QPF, six
Search workers, mnp/32, one 4 GiB shard, noeviction, strict certificate/hostname TLS
and RESP3. Go 1.27.3/GOMAXPROCS4; production compaction remained enabled. Frozen
source and binary hashes matched after each run. Every process/client/transport
closed; independent coordinator verification observed an empty database/index
set before the next agent's work. Operational identities/credentials are private.

## Rejected fixed grouping

The maximum-two-segments-per-pipeline candidate is **rejected**. Its
[complete source/patch provenance](rejected-max2/provenance.json),
[checks including the failed observer attempt](rejected-max2/checks.json),
[all four raw A1/B1/B2/A2 arrays](rejected-max2/) and
[complete width/count summary](rejected-max2/summary.csv) are retained. Both
matched persistent binaries were rebuilt/frozen before timing; no build or source
change occurred between the four windows. All 256 proof/valid samples and
complete normal SQL/type oracles passed.

| Public columns | Pending native proofs | Baseline A1/A2 p50 ms | Candidate B1/B2 p50 ms |
|---|---:|---:|---:|
| 3 | 3 | 14.017 / 17.504 | 0.757 / 0.762 |
| 3 | 8 | 16.916 / 17.306 | 1.421 / 1.416 |
| 24 | 1 | 17.776 / 16.429 | 16.959 / 17.159 |
| 24 | 3 | 17.350 / 17.076 | 31.336 / 16.625 |
| 24 | 8 | 18.945 / 16.896 | 68.218 / 67.641 |

Eight pending probes repeat three real immutable indexes; they are not an
eight-segment SQL fixture. Production compaction stayed enabled. The wide case
can incur the response wait after each group, amplifying cost by approximately
four times. Even the single wide proof returned roughly 8 KiB and remained slow.
The mixed three-proof wide observations and all tail values are preserved; no
general byte threshold or causal mechanism follows from the grouping experiment.

The new grouping/explicit-per-command-NoRetry contract had four failing baseline
leaf tests. It does not establish an old wrong-row/replay defect: the original
FT.INFO marker already protected the whole pipeline. Candidate race units passed
(27 pass events, one endpoint-dependent skip); live corruption/point/unknown
checks passed. One overbroad new observer incorrectly checked unrelated metadata
ZCARD commands; that failure is retained and its scoped focused rerun passed
before timing. The production source and original assertions were restored
byte-for-byte; no fixed-grouping runtime change is selected.

## Generic large-response routing control

The later controlled routing harness retained 24-column native indexes and added
exact application-owned bulk GETs. Both existing library pool policies were
tested: default dedicated pipeline pools and `pipeline_pool_size=-1`. Each
compared scalar/single-command-pipeline PING through the same execution client or
a separate maintenance client. All 640 sequences / 3,840 fresh PINGs, complete
GET values, native index proofs and normal SQL/type oracles passed. Frozen source
and binary hashes remain in [routing provenance](routing-provenance.json).

Complete data and every descriptive result are retained separately:

- [Default-pool data](routing-default.json.gz), [summary](routing-default.csv).
- [Existing pool-opt-out data](routing-no-dedicated-pool.json.gz),
  [summary](routing-no-dedicated-pool.csv).
- [Standalone Go data](standalone-go.json.gz), [summary](standalone-go.csv).

The [standalone executable](repro/main.go) reproduces with only go-redis v9.23.0,
RESP3 and verified TLS: no ADBC, SQL, Search, Lua, managed storage or Arrow.
All 448 sequences / 2,688 fresh PINGs and complete GET values passed. Selected
default-policy first-after-GET PING medians (ms, eight observations per cell):

| GET payload bytes | Scalar separate client | Scalar proof client | Pipeline separate client | Pipeline proof client |
|---:|---:|---:|---:|---:|
| 8,000 | 0.272 | 0.266 | 0.238 | 0.223 |
| 8,191 | 15.105 | 14.850 | 14.701 | 15.220 |
| 16,384 | 14.831 | 15.724 | 14.758 | 15.272 |

The existing pool opt-out and other sizes/routes also reproduce. There is no
driver-configured 8 KiB read buffer: the observed native option defaults are
32 KiB regular buffers and the library's 128 KiB dedicated pipeline buffers.
Changing the downstream client/pool route does not resolve this observation.
The exact payload boundary and underlying endpoint/host/transport cause remain
unproven. The following coordinator controls narrow this observation to the
recorded host/topology combination rather than a portable driver penalty.

## Independent client and topology controls

The coordinator's independent [RedisPy 8.1.0 data](independent-redispy.json) and
[exact executed source](repro/raw_redispy_route.py) reproduce the cliff using raw
send/read on explicitly held same/different physical connections. All 224
sequences and complete GET/PONG values passed, with no ADBC, Go, Search or Lua.
The [complete summary](independent-redispy.csv) retains one/three-GET sequences,
all sizes, routes, repetitions and tails. The recorded library's TLS defaults
verify hostname; the same private trusted CA and RESP3 endpoint were retained.

The same frozen Go source was then cross-compiled for a separate Linux/arm64
client container sharing the Software server's network namespace. It used two
CPUs/256 MiB, verified localhost TLS and the same trusted CA. The server's
four-CPU/8 GiB configuration, data-plane TLS and 4× QPF settings were unchanged.
All 448 sequences passed in 0.36 seconds; all first-PING observations were below
0.1 ms. [Data](standalone-linux.json.gz), [complete summary](standalone-linux.csv),
[client cleanup/source/binary provenance](linux-control-provenance.json) and
[selected server/VM platform](selected-platform.json) retain the actual settings.

The supplemental pure-RedisPy Linux control also passed all 224 sequences using
the exact same Python source and RedisPy 8.1.0 as the host control. It used
Python 3.14.10 on Linux/arm64 with four client CPUs/4 GiB, whereas the host control
used Python 3.15.2. First-PING medians were approximately 0.018 ms and the maximum
was 0.0445 ms. [Complete data](independent-redispy-linux.json),
[summary](independent-redispy-linux.csv) and
[selected provenance/cleanup](redispy-linux-control-provenance.json) retain these
phase-specific resources. This is distinct from the earlier Go Linux client,
which used two CPUs/256 MiB. Both used the unchanged Software server and verified
TLS in its network namespace; neither weakened certificate/hostname checks.

Go columns use default-policy scalar PING on the proof client; RedisPy columns
use one GET and the same held connection. Medians are milliseconds, eight per cell:

| GET bytes | macOS host/published port, Go | Same host route, RedisPy held connection | Linux/server namespace, identical Go source | Linux/server namespace, identical RedisPy source |
|---:|---:|---:|---:|---:|
| 8,000 | 0.266 | 0.243 | 0.017 | 0.018 |
| 8,191 | 14.850 | 15.931 | 0.017 | 0.018 |
| 8,192 | 15.038 | 15.320 | 0.016 | 0.018 |
| 8,193 | 14.614 | 15.543 | 0.017 | 0.018 |
| 16,384 | 15.724 | 15.080 | 0.017 | 0.018 |
| 32,768 | 15.544 | 15.517 | 0.018 | 0.019 |

This does not isolate which OS, port-forwarding, TCP/proxy or scheduling detail
caused the host wait. It rules out treating that wait as inherently ADBC, Lua,
Search or go-redis algorithm time, or as a universal property of this Software
server. Historical results retain their actual macOS/port-forwarded topology;
do not rewrite them or compose source ratios across these topology controls.
The temporary Linux clients and their environment files were removed;
certificate and hostname verification were never weakened. The RedisPy control
also changed Python runtime and client resource limits; no individual OS,
forwarding, proxy, TLS or runtime mechanism is assigned from these comparisons.

The standalone program deletes only its own seven expiring keys; deletion is
best effort in the program, and independent coordinator verification observed an
empty database/index set after this run. It does not FLUSH/SCAN other keys.
No server configuration was changed. The [upstream draft](upstream-draft.md)
and [selective FT.INFO proposal](selective-info-proposal.md) distinguish observed
behavior from requested future capabilities. Both were submitted on 2026-11-15, as
[docker/desktop-feedback#854](https://github.com/docker/desktop-feedback/issues/854)
and [RediSearch/RediSearch#12337](https://github.com/RediSearch/RediSearch/issues/12337);
both were open when checked on 2026-11-24.

An unrun native Python/ADBC benchmark wrapper was prepared privately and excluded
from this change after all read runtime candidates were rejected. No public
native Python query-latency improvement is claimed.

Reproduce each complete offline summary with:

```sh
python3 benchmarks/point-valid-isolation/summarize.py benchmarks/point-valid-isolation/v2-profile.json.gz
python3 benchmarks/point-valid-isolation/summarize_width.py
python3 benchmarks/point-valid-isolation/summarize_routing.py benchmarks/point-valid-isolation/standalone-go.json.gz
python3 benchmarks/point-valid-isolation/summarize_routing.py benchmarks/point-valid-isolation/standalone-linux.json.gz
python3 benchmarks/point-valid-isolation/summarize_redispy.py
python3 benchmarks/point-valid-isolation/summarize_redispy.py benchmarks/point-valid-isolation/independent-redispy-linux.json
python3 -m unittest discover -s benchmarks/point-valid-isolation -p test_summarize_routing.py -v
```
