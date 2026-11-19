# Released-driver performance refresh — November 18, 2026

All **324 trials validated** using the official **v0.0.29** Linux arm64 shared library and CLI: 84 scan, 40 existing-table write, 32 creation and 168 point/query records. This includes 72 warmups and 252 measured trials. No failed trial, conflict or discarded sample.

The released artifacts were verified against published SHA-256 files; the benchmark did not rebuild or modify the driver. The library reports v0.0.29, with release tag source `30a9eefbf13d19b3e3129171f8ecdc5579b852a2`. See [release](https://github.com/redis/redis-adbc-driver/releases/tag/v0.0.29), [provenance](provenance.json), [all chart values](charts.csv), [bulk samples](table-modes.json) and [scenario samples](scenarios.json).

## Environment and interpretation

Redis Software 8.2.6-58 / Redis 8.8.0; active/effective 4× QPF, six Search workers, mnp/32, one 4-GiB noeviction RAM shard. Server four CPUs/8 GiB; Linux arm64 client four CPUs/4 GiB, sharing the server network namespace with hostname/certificate-verified TLS and RESP3. Host Apple M5 Max, 18 logical CPUs, 36 GiB, macOS 26.7.3. Exact packages are recorded in provenance.

Fresh released-driver reference, not a matched speedup comparison. Software build differs from November 14. No ClickBench rerun. Existing other-task containers were left running; host scheduling was not isolated. Source deck export/render work overlapped part of the scan window; no causal small-delta claims. All samples retained.

Managed snapshots/publication, live unmanaged HASH writes and raw commands provide different guarantees. Known-address points exclude mapping lookup; no multi-statement transactions are measured. Finite p95 and closed-loop throughput do not establish production capacity, confidence intervals, SLA tails or statistical equivalence. Automatic retries=0 describes no caller replay, not measured wire reconnect behavior.

## Point operations and analytical queries

Point fixture: 1K rows ×4 columns, three measured rounds ×32 calls (96 pooled latencies per cell). Analytical fixture: 100K ×4, three rounds ×8 complete queries (24 latencies). Each cell has one warmup. Persistent per-thread clients; execute/full typed Arrow fetch or acknowledged write plus conversion timed. Setup, independent validation and teardown excluded; lazy first-use transport remains included.

| Operation | Mode | Clients | p50 ms | p95 ms | Median ops/s |
|---|---|---:|---:|---:|---:|
| filtered_count_sum | managed | 1 | 3.679 | 7.795 | 252.813 |
| filtered_count_sum | managed | 4 | 7.924 | 10.755 | 478.628 |
| filtered_count_sum | redis_py | 1 | 2.873 | 5.894 | 304.306 |
| filtered_count_sum | redis_py | 4 | 9.789 | 12.623 | 392.537 |
| filtered_count_sum | unmanaged | 1 | 33.458 | 36.184 | 29.651 |
| filtered_count_sum | unmanaged | 4 | 55.142 | 59.887 | 72.470 |
| filtered_projection | managed | 1 | 3.117 | 6.454 | 273.910 |
| filtered_projection | managed | 4 | 7.686 | 10.783 | 499.608 |
| filtered_projection | redis_py | 1 | 0.857 | 3.103 | 857.591 |
| filtered_projection | redis_py | 4 | 4.326 | 8.672 | 820.285 |
| filtered_projection | unmanaged | 1 | 2.674 | 5.639 | 335.821 |
| filtered_projection | unmanaged | 4 | 7.471 | 9.975 | 511.943 |
| grouped_count_sum | managed | 1 | 12.768 | 18.184 | 74.400 |
| grouped_count_sum | managed | 4 | 19.200 | 23.740 | 194.823 |
| grouped_count_sum | redis_py | 1 | 38.222 | 42.590 | 26.180 |
| grouped_count_sum | redis_py | 4 | 106.741 | 198.281 | 34.473 |
| grouped_count_sum | unmanaged | 1 | 299.229 | 313.560 | 3.365 |
| grouped_count_sum | unmanaged | 4 | 506.964 | 562.312 | 7.406 |
| delete | managed | 1 | 6.503 | 41.835 | 80.568 |
| delete | managed | 4 | 9.965 | 282.840 | 68.556 |
| delete | redis_py | 1 | 0.044 | 0.168 | 8930.233 |
| delete | redis_py | 4 | 0.210 | 4.538 | 5726.727 |
| delete | unmanaged | 1 | 2.399 | 4.340 | 384.836 |
| delete | unmanaged | 4 | 6.027 | 10.703 | 589.703 |
| insert | managed | 1 | 6.471 | 41.182 | 75.101 |
| insert | managed | 4 | 8.044 | 192.350 | 69.557 |
| insert | redis_py | 1 | 0.049 | 0.171 | 7704.656 |
| insert | redis_py | 4 | 0.163 | 4.739 | 5217.994 |
| insert | unmanaged | 1 | 2.479 | 6.331 | 401.355 |
| insert | unmanaged | 4 | 5.763 | 10.635 | 632.876 |
| point_read | managed | 1 | 1.998 | 3.330 | 460.457 |
| point_read | managed | 4 | 5.310 | 8.342 | 672.879 |
| point_read | redis_py | 1 | 0.057 | 0.490 | 7086.767 |
| point_read | redis_py | 4 | 0.404 | 4.978 | 3892.747 |
| point_read | unmanaged | 1 | 2.122 | 4.277 | 409.027 |
| point_read | unmanaged | 4 | 5.120 | 9.659 | 670.023 |
| update | managed | 1 | 7.080 | 36.180 | 76.157 |
| update | managed | 4 | 9.122 | 280.270 | 66.779 |
| update | redis_py | 1 | 0.047 | 0.120 | 8365.285 |
| update | redis_py | 4 | 0.138 | 4.387 | 5654.834 |
| update | unmanaged | 1 | 2.344 | 4.060 | 394.787 |
| update | unmanaged | 4 | 5.808 | 10.502 | 641.672 |

Projection returns 100 rows; grouped COUNT/SUM returns 16 groups, filtered COUNT/SUM matches 10K rows. Outputs use exact int64 Arrow types. Synthetic data omit NULL/floating-point edge cases; raw reducer results do not prove safety for arbitrary integer sums.

## Bulk scans, writes and creation

Wide scans: canonical 100K ×105 typed source, six measured repeats after warmup; one or four workers (four readers use eight disjoint statements). Writes/create: 10K total rows ×105 plus ordinal, up to 1K rows/call, three measured repeats after warmup. One/four writers process the same total rows.

| Suite | Mode | API/batch | Readers/writers | Median s | Min–max s |
|---|---|---|---:|---:|---|
| reads | managed | 512 | 1 | 1.485 | 1.414–1.560 |
| reads | managed | 4096 | 1 | 1.416 | 1.392–1.528 |
| reads | managed | 512 | 4 | 1.290 | 1.258–1.684 |
| reads | managed | 4096 | 4 | 1.331 | 1.202–2.412 |
| reads | unmanaged | 512 | 1 | 5.026 | 4.845–5.261 |
| reads | unmanaged | 4096 | 1 | 4.700 | 4.662–5.030 |
| reads | unmanaged | 512 | 4 | 1.675 | 1.615–1.920 |
| reads | unmanaged | 4096 | 4 | 1.758 | 1.519–1.920 |
| reads | redis_py | 512 | 1 | 4.372 | 4.334–4.431 |
| reads | redis_py | 4096 | 1 | 4.232 | 4.180–4.423 |
| reads | redis_py | 512 | 4 | 2.012 | 1.933–2.259 |
| reads | redis_py | 4096 | 4 | 2.059 | 1.984–2.206 |
| writes | managed | sql / append | 1 | 2.875 | 2.845–2.929 |
| writes | managed | sql / append | 4 | 4.790 | 4.640–5.370 |
| writes | unmanaged | sql / append | 1 | 1.890 | 1.880–1.919 |
| writes | unmanaged | sql / append | 4 | 1.185 | 1.117–1.225 |
| writes | managed | arrow / append | 1 | 2.453 | 2.403–2.576 |
| writes | managed | arrow / append | 4 | 4.510 | 4.506–4.652 |
| writes | unmanaged | arrow / append | 1 | 1.128 | 1.074–1.131 |
| writes | unmanaged | arrow / append | 4 | 0.863 | 0.857–0.893 |
| writes | redis_py | hset / append | 1 | 1.128 | 1.101–1.221 |
| writes | redis_py | hset / append | 4 | 0.844 | 0.784–0.853 |
| creation | managed | arrow / create | 1 | 2.504 | 2.384–2.662 |
| creation | unmanaged | arrow / create | 1 | 0.663 | 0.653–0.663 |
| creation | managed | arrow / create_append | 1 | 2.403 | 2.398–2.444 |
| creation | managed | arrow / create_append | 4 | 4.467 | 4.284–4.507 |
| creation | unmanaged | arrow / create_append | 1 | 0.650 | 0.637–0.666 |
| creation | unmanaged | arrow / create_append | 4 | 0.491 | 0.460–0.498 |
| creation | redis_py | hset / create | 1 | 0.709 | 0.701–0.713 |
| creation | redis_py | hset / create | 4 | 0.432 | 0.427–0.439 |

Bulk wall time includes fresh worker startup/imports, source loading/conversion, connections, complete reads/writes, IPC output and shutdown. Setup and independent typed-value/count/multiplicity validation are outside timing. Creation includes table/index work; managed indexed publication and membership-only unmanaged/raw creation differ. RSS values in summary retain maximum individual worker lifetime high-water marks, not summed process memory or Go heap.

## Slides, reproduction and verification

Update the existing timing tables, labels and speaker notes on slides 4, 19–22 and 25. Historical ClickBench slides 23–24 retain their original values, actual distribution/settings and unequal-resource caveats.

Provision an owned fixture with `make -C go redis-up`, verify the settings above, install the exact checksummed release library/CLI, and use the Linux benchmark image. Run `arrow_compare.py --suite all` with the canonical source, defaults of six read/three write repeats, 10K write rows and 1K-row calls; then owner-check and empty the dedicated database. Run `scenarios.py --suite all` with its unchanged default 1K/100K fixtures, 32 calls/eight queries and three repeats. Use fresh work directories and the same released binaries for both suites. Credentials, endpoint addresses and CA files remain private.

Audit with `python3 benchmarks/table-modes/release-v0029-2026-11-18/audit.py`. Full matrices, individual validations, arithmetic and exact chart regeneration are checked. Cleanup and slide verification are recorded separately.

## Release-refresh stage verification and cleanup

All five native numeric tables exactly match regenerated chart values and ratios.
All six date/version labels and speaker notes were updated, retaining the earlier
notes under an explicit archive marker. The final 29-slide PDF rendered correctly;
all six edited slides passed visual review without repair. The other 23 slides,
including both historical ClickBench slides, are pixel-identical to the source.
See [slide verification](slides-verification.json).

Both suites' exact owner markers were checked before emptying the dedicated
database. Postflight verified an empty/index-free database, active 4× QPF, six
Search workers, mnp/32 and certificate/hostname-verified TLS/RESP3. Separate Linux
PING and 128-KiB/1-MiB ECHO reply controls passed; these are postflight transport
observations, not SQL performance evidence. See [postflight](postflight.json).
The task-owned server/network, client image tag, generated environment/owner files
and fixture TLS files were removed. Preexisting running containers and unrelated
network configurations were preserved. See [cleanup](cleanup.json).

The official CLI embeds the tag revision and `vcs.modified=true`; the shared
library does not embed a VCS revision. Source attribution uses the release tag
and checksummed official archives. This report makes no clean-source-build claim.
The evidence in this directory records the release-refresh stage before the
subsequent ClickBench changes. The [final ClickBench report](../../clickbench/software-ram-c8g-main-14b4a494-2026-11-18/README.md)
records the later main-build comparison, slide 24 deletion and final 28-slide
verification. Historical 29-slide assertions above describe this earlier stage.
