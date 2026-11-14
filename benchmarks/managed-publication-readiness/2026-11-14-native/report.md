# Managed publication batching native pilot — November 14, 2026

**NOT SELECTED for integration.** This experiment saves one client exchange per
private output, but native results do not establish a consistent latency benefit.
Four-client DELETE p50 regresses **30.3%**, and one-client DELETE p50 regresses
4.8%. The coordinator chose to retain the separately demonstrated fail-closed
safety fixes and omit publication batching and its additional runtime complexity.
This is not a claim that every experimental metric became worse.

This is an uninstrumented native Python/C-boundary **managed-write subset** of
the unchanged standard ScenarioBenchmark setup, operation and independent complete
post-state validation. Four subsets ran baseline/candidate/candidate/baseline;
each retains 24 records (six warmups plus 18 measured trials) over INSERT, UPDATE,
DELETE and one/four clients, 32 operations/trial, 1K rows/four public columns plus
ordinal control. In total: **96 records**, **72 measured trials**, **2,304 measured
operation observations**, all validated with no failed records, retries or
samples discarded. This is not a complete 42-case/168-record matrix, and it has
no redis-py, unmanaged, read/query or capacity comparison.

All native samples and exact source/build/harness identities are retained in
[provenance](provenance.json), the four build/run JSON files and [comparison
JSON](comparison.json)/[CSV](comparison.csv). Each operation/client/build pooled
statistic below has six measured trials and 192 observations. Application clients
persist within a trial; internal driver work is timed, setup/teardown and complete
independent typed mutation-state validation are outside timing. p95 uses nearest
rank. Throughput is median finite-trial operations/s, not saturated capacity.

| Operation | Clients | Baseline p50 ms | Candidate p50 ms | Baseline/candidate | Baseline p95 ms | Candidate p95 ms | Baseline ops/s | Candidate ops/s |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| delete | 1 | 42.479 | 44.524 | 0.954× | 140.837 | 137.646 | 17.165 | 16.920 |
| delete | 4 | 50.380 | 65.633 | 0.768× | 961.512 | 937.081 | 13.238 | 15.948 |
| insert | 1 | 44.649 | 41.318 | 1.081× | 144.832 | 145.538 | 16.762 | 17.200 |
| insert | 4 | 49.466 | 48.939 | 1.011× | 940.211 | 898.337 | 16.322 | 16.761 |
| update | 1 | 45.766 | 44.959 | 1.018× | 119.910 | 121.176 | 17.418 | 17.477 |
| update | 4 | 64.537 | 56.942 | 1.133× | 1265.470 | 840.183 | 14.030 | 16.637 |

Four-client UPDATE p50 falls 11.8%, median throughput increases 18.6% and p95 falls
33.6%. One-client INSERT p50 falls 7.5%; four-client INSERT p50 is nearly flat.
Four-client DELETE median throughput **improves 20.5%** and p95 falls 2.5% despite
its 30.3% p50 latency regression. Its latency regression is not a throughput loss.
All six measured candidate DELETE/four-client trial medians are retained; five
are above 63 ms, compared with a maximum 60.3 ms among baseline measured medians.
No causality, confidence bound or universal performance improvement follows from
this finite sequential pilot. The separate instrumented Go diagnostic is also
mixed and is retained [here](../2026-11-14/report.md).

Baseline is `7b94a93c`; experimental native candidate is `feb325a9`, including
the initial batching and later fail-closed safety guards. Full hashes appear in
provenance. Both clean versioned library/CLI builds completed before any native
pilot timing; no other builds/live tasks ran during the four windows. Post-run
artifact hashes match. Native GOMAXPROCS/GOGC/GOMEMLIMIT/GODEBUG were unset and
client CPU/RAM uncapped. The same coordinator-owned Redis Software fixture was
independently checked before every subset for active/effective 4× QPF, six Search
workers, mnp/32, noeviction, one 4-GiB RAM shard and certificate/hostname-verified
data TLS/RESP3. The agent did not separately capture a full host/server version
inventory or server CPU profile; no broader environment/CPU claim is made.

The [frozen wrapper](harness/managed_subset.py) labels subset scope explicitly;
[frozen scenarios.py](harness/scenarios.py) and [run.py](harness/run.py) are unchanged
from the standard harness. Source-only reproduction of the unselected experiment
is retained in the adjacent [experiment patch](../publication-batching-experiment.patch).
The integrated safety-only source is deliberately different and is not relabeled
as the timed experimental candidate.

Final safety Software race checks at `feb325a9` passed **23 top-level tests,
zero skips/failures**, including native permission compatibility and complete
value/publication proofs; [checks](checks.json) retains the exact command. The
Go diagnostic archive retains the earlier failing regression checkpoints and
subsequent unit correction. Independent source review found no remaining
correctness/security finding in the experiment; its rejection is a measured
performance/complexity decision, not a changed consistency contract.

Every subset's clients exited before ownership-marker-checked data/index cleanup;
[cleanup](cleanup.json) records all four empty outcomes. All pilot processes exited
before the shared live slot returned to the coordinator. Final fixture/network
teardown is coordinator-owned and not claimed here. Raw logs, endpoints,
credentials/licenses, owner IDs and operational artifacts remain private.
