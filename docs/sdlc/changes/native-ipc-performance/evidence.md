# Verification and review: native IPC performance

Date: 2026-10-22. Author/reviewer: Codex (agent review, not human approval).
Source: `e2e7725c3b7be15ff9e657b9edc2a1488e676b05`; pinned binary/source checksums
in [manifest](../../../../benchmarks/native-ipc/2026-10-22/manifest.json).
See [intent](intent.md), [spec](spec.md), [plan](plan.md), and the
[report](../../../../benchmarks/native-ipc/2026-10-22/README.md).

## Completed checks

- `make -C go build`: current shared library present; actual binary build info
  pins Go 1.27.2 and Arrow Go 18.8.0. Source-equivalence with merged main checked.
  Do not infer the compiler version from go.mod's lower minimum.
- Small-fixture smoke matrix: 48/48 operations passed, 1,000 rows per source.
  Separate raw smoke results are retained and excluded from measured medians.
- Initial smoke stopped on an assertion assuming SUM(BIGINT) would have a
  decimal Arrow type. The benchmark SQL now explicitly casts SUM to
  DECIMAL(38,0), and the complete smoke matrix was rerun. No driver assertion
  or production behavior was changed to accommodate data mismatches.
- Validator positive and deliberate-corruption checks passed for narrow/wide
  fixtures: a changed text value is rejected, while every correct batch passes.
- Main matrix: **144/144 operations validated**, including 24 warmups and 120
  measured fresh processes, five per method/case. **7,202,328 complete
  output/read-back rows** checked. No measured case failed or was skipped.
- Summary checks assert all 24 expected cases, complete trial numbering,
  counts/schema/encoded-size stability, paired schema/batch equality, and
  identical native/PyArrow import file sizes. All passed.
- Process CPU/RSS captured before validation; full export rereads and import
  SQL read-back checks validate every value against independent Arrow fixtures
  or integer aggregate arithmetic. Source file hashes/sizes retained.
- Ruff, Python formatting, syntax and relative-link checks: passed.
- No production code changed, so the broad Go suite was not repeated for these
  benchmark/report-only files. The actual benchmark exercises both C paths and
  IPC formats with the merged driver; it is not cross-platform validation.

## Environment and fairness review

Owned local fixture: Compose project `adbc-ipc-bench`, one Redis Software
Enterprise 8.6.2 primary shard, 1 GiB, noeviction, persistence disabled.
Active QPF scaling factor 4, six Search workers, mnp routing and 32 connections
were independently verified through the deployment API both before and after
all timings. Local RESP3 TCP; no paid deployment created.

Reviewed under REVIEW.md for correctness/lifecycle, ownership and evidence.
No known actionable finding remains in the comparison scope. Both methods use
the same pinned binary, SQL, source/input files, batching, index policy and
filesystem. Method order alternates; fresh targets include table/index creation.
Temporary files and unique test tables are removed by finally blocks. Exact
schemas and all values, including NULLs, binary, decimals, UTC timestamps,
unicode and NUL strings, passed independent checks.

The comparison measures whole interface operations. Client export includes its
C Stream release callback's forced Go GC; native export uses native release.
Connection/statement setup, verification and teardown are excluded. Files are
closed without fsync and page cache remains enabled. Server CPU/RSS, cold-cache
behavior, standalone serializer cost, steady-state cleanup CPU, WAN/TLS,
concurrent users, compression/dictionary data and full-scale ClickBench are not
measured. Both children import PyArrow for later validation, so native RSS is
not a minimum-runtime application footprint.

All elapsed ranges overlap and medians vary from -3.3% to +3.8%; the report
makes no native speedup, formal equivalence or universal memory claim. Earlier
transfer and UNION ALL reports remain dated historical evidence.

Owned container/network cleanup: complete. Docker Compose removed the owned
container and network; label-filtered Docker inventories returned empty.
