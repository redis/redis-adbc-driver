# Current baseline diagnostic evidence

These are **baseline diagnostic summaries**, not final projection-only candidate
results. Production runtime source is merged main
`3b42245402a2d7cf6f363faf7ac713cd36621f90`. All workloads used owned native Linux
clients against verified Redis Software 4× QPF with trusted TLS/RESP3. See the
[diagnostic methods](../../diagnostics/README.md) for timing and lifecycle scope.

- [Go baseline](go-baseline-summary.json): 12 managed/unmanaged/raw CRUD cases,
  396 complete operations including 12 warmups; 32 measured operations per case.
  Command categories, hook counts and wall stages remain distinct. Managed and
  unmanaged point reads observed 10 client-hook calls and 19/18 commands per
  operation; the raw control observed one HMGET. These are not packet RTTs.
- [Outer native cursor stages](native-baseline-summary.json): one-client measured
  summaries from a complete 48-trial one/four-client CRUD diagnostic. The retained
  underlying run contains 1,536 operations, including 768 warmup operations.
- [Nested native stages](native-nested-summary.json): a separately frozen wrapper
  checkpoint and fresh complete 48-trial run. Actual prior-result close,
  AdbcStatementPrepare, AdbcStatementExecuteQuery and Arrow read-all are measured
  inside their normal cursor boundaries, without changing resource lifetime.
- [Exact checkpoints and input hashes](provenance.json), [independent full-grid
  audit](audit.json), and [pinned installed driver-manager source
  hashes](dbapi-pinned-source-inspection.json) identify the distinct observers and
  source inspection. No endpoint, owner/infrastructure ID or raw operational log
  is published here.

For one-client point reads in the nested run, managed/unmanaged total p50 was
2.002/2.032 ms, actual ADBC execute-query p50 was 1.522/1.509 ms and previous-result
close p50 was 0.447/0.438 ms. Prepare and Arrow read-all each occupied smaller
observed spans. Closing the previous result is a measured optimization
opportunity; it does not explain the entire difference from a separate Go run.

Each one-client trial has **31** prior-result close observations, and each
four-client trial **28**: the first operation of each client has no preceding
result; final cursor close occurs outside operation timing. Other operation and
execute-query summaries have 32 observations per trial. Nested spans overlap
outer stages, and separately pooled medians must not be added/subtracted as a
wall-time or CPU decomposition.

Installed `adbc-driver-manager==1.13.0` source inspection found no implicit
ExecuteSchema/GetParameterSchema call for ordinary unbound literal SQL. DBAPI
execute clears the prior result, prepares changed SQL and calls ExecuteQuery
(including DML). The lazy result is imported on fetch and closed at the next
execute or cursor teardown. Current driver stream release explicitly invokes
`runtime.GC()`, but the measured close span is not an exclusive GC/CPU profile.
No GC policy, lifetime, prepared-plan or DBAPI execution semantics change is
selected from these observations. The earlier negative native bulk/GC experiment
and its safety limitations remain intact.

These finite instrumented observations include observer overhead, lazy first-
operation transport work and different Go/native mutation APIs. Original typed
outputs and complete mutation post-state oracles passed; the independent audit
also verified unique operation indexes, metadata identity and outer-timer bounds
against the original retained results. Keep warmups and original runs separate
from summaries, and use the final uninstrumented native comparison for selected
runtime performance claims. Counts do not constitute wire-retry telemetry,
server CPU, a production-capacity result or sanitizer/lifetime proof.

## Reproduce the diagnostic summaries offline

Run `python3 benchmarks/crud-execution/results/diagnostics/audit.py`. The auditor
reads five retained sanitized deterministic-gzip inputs: complete Go observations,
and original results plus operation observations for both native runs. Gzip
mtime is zero and no original filename is embedded; decompressed JSON bytes are
preserved exactly. Provenance records both original input hashes and published
compressed/uncompressed hashes, including whether the gzip bytes match the
original source exactly. No private paths are needed.

The auditor reproduces all three summaries, including per-stage observation
counts, and checks the original complete grids, result validation/status,
metadata identities, worker-order/index mapping and timing bounds. It uses
rounding tolerance only for machine-level floating arithmetic, not displayed
precision. Nested close percentiles exclude absent first-call observations;
they are not zero-imputed. This is an arithmetic/record-completeness audit of
the retained owner-produced evidence, not independent binary authentication or
a new live correctness test.
