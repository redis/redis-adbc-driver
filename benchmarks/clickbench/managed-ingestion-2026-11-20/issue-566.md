# Full managed-load measurement: incomplete

The first full canonical one-writer attempt stopped at its configured memory
reserve threshold. It did not publish or reach independent validation. Issue
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
After its terminal outcome was established, cleanup of only the disposable
measurement fixture was authorized. A separately labeled 24/4 GiB server/client
attempt with the same 12 GiB cache and one writer is planned; it is not a
completed result or a change to this attempt's recorded limits. No AWS resources
were started. See [acceptance](acceptance.md) and [method/hashes](method.json).
