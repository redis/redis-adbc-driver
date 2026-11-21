# Full managed-load measurement: incomplete

Two full canonical one-writer attempts failed: the first at its configured
memory reserve, the second during native Search-on-Flash index compaction after
all source rows were staged. Neither published or reached independent validation. Issue
[#566](https://github.com/redis/redis-adbc-driver/issues/566) remains open;
`backup_ready=false`. No full-scale query result or reusable backup is claimed.

## Profile and result

The target was exactly 99,997,497 source rows and 105 columns, using the unchanged
upstream ClickBench DDL. Unlike the earlier fixed-policy performance controls,
this attempt did **not** add `NOINDEX` to public fields. A preceding 512-row
functional check passed exact all-105-value/schema/multiplicity validation and
confirmed 77 NUMERIC and 28 TAG public attributes, with no indexing failures.
Flex plain indexes omit SORTABLE/UNF.

The Linux ARM64 client used the server's network namespace, Redis Software
8.2.6-58 / database 8.8.0, bigstore v2, Search on Flash and freshly verified
4× QPF. Production Go sources matched `4faa8806` and the then-current main
`646e60a2`. The server/client hard limits were 20/4 GiB, with a 12 GiB Flex RAM
cache. One append consumed original Parquet row groups with bounded 4,096-row
conversion and normal driver maintenance. Statement and validation budgets were
six hours each; successful data was to be retained.

| Observation | Result |
| --- | ---: |
| Case elapsed time through cancellation and joined cleanup | 4,502.426 s |
| Source rows yielded | 37,380,988 |
| Actual staged directory members at the subsequent read-only audit | 37,378,684 |
| Positively acknowledged published rows | 0 |
| Effective server working memory at guard crossing | 17.062 GiB |
| Raw server cgroup memory at crossing | 17.495 GiB |
| Summed observed Redis-process RSS at crossing | 15.285 GiB |
| Docker VM available memory at crossing | 11.679 GiB |
| Server Flash filesystem free at crossing | 1,326.714 GiB |
| Observed OOM / OOM-kill events | 0 / 0 |

The configured effective-memory stop was 17 GiB. Its heuristic credited only
inactive clean file cache, bounded by file minus shared memory and reduced by
dirty/writeback bytes; it also retained `anon+kernel` as a floor. A targeted
3 GiB reserve was not a guarantee of immediate cache reclaim. Raw memory earlier
approached the 20 GiB hard limit while reclaiming cache: the case recorded 2,273
`memory.events.max` increments, without OOM. These are distinct from the later
effective-memory guard crossing. Full-memory PSI avg10 was zero at the crossing.
This demonstrates the configured reserve was exhausted, not host RAM exhaustion.
The [sanitized attempt summary](full-load-attempts.json) includes the driver,
physical DDL and frozen harness hashes; operational artifacts remain private.

The management-API statistics observer missed approximately the first ten
minutes because its finite-duration limit rejected the requested 13-hour bound.
The independent Redis-free resource watchdog and its sampled cgroup/CPU/RSS/I/O
series remained active throughout. The API gap is not filled with estimates.

## Outcome and limits

Cancellation returned SQLSTATE 57014 and the writer/controller joined. Because
the client initially reported an unknown acknowledgement, no input was replayed.
A subsequent raw, read-only metadata audit found the workspace in durable
`deleting` state and the committed table still at zero rows with its initial
empty data/index prefix. The staged output was not published. The final
105-field index had not yet been built; do not attribute this stop to its
postings. There were no observed indexing failures, but full-scale indexed
sealing remains unmeasured.

An earlier isolated synthetic directory test created all 99,997,497 fixed-width
IDs successfully, with an actual 9.635 GiB `MEMORY USAGE` footprint and an
11.828 GiB server-cgroup peak. It tested hot directory allocation only: no
105-column row HASHes, completed native ClickBench load, cold promotion,
internal Flash-value encoding or restore compatibility. Its bounded 3.84M-ID
RDB screen likewise did not prove Flex's internal representation.

The aborted attempt's private audit and measurement artifacts are retained.
After its terminal outcome was established, only its disposable measurement
fixture was recreated. The next attempt is separately recorded below; its
different limits and fresh allocator/cache state do not change this first
attempt's provenance.

## Fresh 24 GiB attempt: all input staged, native index crash

On November 21, a fresh server used the same pinned Software/database build,
12 GiB Flex cache, canonical DDL, driver, one writer and six-hour operation
budget, with a 24 GiB server hard limit and a 21 GiB effective-memory guard.
The client remained capped at 4 GiB. Fresh management-API checks verified
4× QPF and Search on Flash. API statistics recorded from the start. This was
a new authorized attempt after the earlier abort was confirmed, rather than a
replay of an unresolved operation.

All **99,997,497 source rows** were supplied in **13,191.641 s**
(3 h 39 min 51.6 s), and the observed private directory count matched.
This is input completion, not acknowledged publication or a successful full
load. The final 105-field index then began its explicit population pass.
The last pre-crash observation was 1,584,640 indexed documents, with zero
reported indexing failures. A 308.547-second window measured 3,772.6 indexed
documents/s. The fixed six-hour deadline was a separate risk at that rate,
but it did **not** cause the observed failure.

Native boot logs identify Redis database 8.8.0 and Search module 8.8.8.
Native logs show an inverted-index merge-order error, followed by failed
TAG-column-family compaction and a Search panic. Redis terminated with
**SIGABRT (signal 6)** and restarted internally in the same container.
The Docker container itself did not restart. Pre-crash telemetry showed
14.694 GiB effective / 18.954 GiB raw server memory, approximately 14.10 GiB
globally available, and 1,178.84 GiB Flash filesystem free. Recorded cgroup
max/OOM/OOM-kill counters were zero, and there was no full-memory pressure
alarm. Thus neither the resource guard nor the six-hour deadline explains
the native abort.

The case ended after **13,630.547 s**, with **zero acknowledged rows** and no
independent full validation. Its client process peak RSS was 661.09 MiB;
the Arrow pool peak was 196.59 MiB. These cover complete source consumption
and the observed index interval, not a successfully published database.

Existing index-phase samples measured approximately 3.11 data-Redis CPU cores
over 374.80 seconds, versus 0.049 client cores. Container block I/O in that
window was approximately 133.7 MB/s read and 86.6 MB/s written. These are
sampled, shared-container observations, not causal attribution or per-process
disk I/O. In particular, this index interval was not confined to one CPU.

After restart, raw reads found no keys, catalog, metadata graph or indexes.
Data persistence had been disabled: staged data was not recovered. No usable
full backup exists. Native logs and forensic evidence were secured privately;
no full load was replayed after this crash. No more full-scale attempts are
planned until a bounded investigation establishes a viable path.

The fixture stored Flash data in its Linux container's writable overlayfs
layer, rather than a dedicated NVMe device. This is material environment
provenance; no evidence establishes it as the cause of the merge-order error.
The deeper trigger and a compatible fix remain unverified. Internal research
and operational identifiers are excluded from this public report.

No AWS resources were started. See [acceptance](acceptance.md),
[sanitized attempts](full-load-attempts.json) and [method/hashes](method.json).
