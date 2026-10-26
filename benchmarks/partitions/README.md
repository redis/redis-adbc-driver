# Native partition measurements

Run against a dedicated Redis with Search; the benchmark creates, writes and
removes its own UUID-named table. It never provisions infrastructure. Build the
library and use the validation environment's Python (ADBC manager, Arrow, redis):

```sh
make -C go build
export REDIS_URI=redis://localhost:6379/0
# Use .dylib on macOS. Run each case at least three times with a fresh output.
go/validation/.venv/bin/python benchmarks/partitions/run.py \
  --driver "$PWD/go/build/libadbc_driver_redis.so" --rows 100000 --width 64 \
  --layout dense --partitions 1 4 8 32 --workers 1 4 8 --output /tmp/dense-64.json
```

Repeat for widths 0, 64, 1024, row counts appropriate to the deployment, and
`--layout sparse` / `skewed`. Those layouts delete 90% of allocated IDs, evenly
or from the first 90% of the sequence. Test configuration-hidden deployments
with `--scan-cap N` only when N is a guaranteed positive lower bound. To test TLS
and independent worker identities, use the separately configured native tests;
this benchmark intentionally uses one supplied identity on every process.

Each run records versions, revision, library hash, ordinary query time,
planning time, end-to-end multi-process consumption time, throughput, lifetime
peak RSS per worker/coordinator, server memory samples and network byte counters.
Every measured read fully drains its readers; schema and exact sorted values
are checked afterward, outside the timer. The multi-process timer includes
process startup, connection/authentication, Arrow IPC writes and teardown.
The ordinary timer includes fetch/materialization but no IPC write. Compare
these as end-to-end workloads, not a pure Search speedup.

RSS is the process high watermark (workers may consume multiple descriptors),
not incremental allocation. Server memory is before/after; used_memory_peak is
server-lifetime peak, not per-query peak. Network counters are server-wide,
including other clients. Use an isolated database/process and external sampled
server/container monitoring for attributable peaks. Denied INFO gives null
metrics, never invented zeros. Do not publish production performance claims
from the small checked-in smoke measurements.

Cloud/Software resource owner must supply a disposable database, supported
version, TLS trust, worker identities, quota/budget and expiry/cleanup owner.
New Cloud benchmark deployments default to Redis Cloud Pro.
No paid resources or credentials are checked in. Native CI uses existing release
builds; see [validation and ownership](../../docs/partition-validation.md).

For immutable final results, add `--mode retained`. The runner creates a fresh
owned namespace, opens reader-only worker connections, revokes completed jobs,
and verifies namespace cleanup. `--tls-ca-file` supplies explicit roots to every
client. `--execution-limits` accepts a recorded partial JSON override; for example
100k rows with a 64-byte payload in the local runtime pilot used
`{"transport_bytes":536870912,"source_bytes":268435456,"working_bytes":1073741824,"work":10000000}`.
Default limits are intentionally conservative and can reject wide/large inputs.
A resource rejection is not a valid throughput result. No Redis policy or memory
setting is changed by the runner. See the [runtime measurements](retained-2026-10-21/README.md).

The ClickBench API pilot in `benchmarks/clickbench/partition_profile.py` also
accepts `--partition-mode retained` and `--execution-limits`. It measures unchanged
official queries that pass admission, plus explicitly labeled supplemental scans,
against the independent typed-source/DuckDB oracle. Cross-engine generated column
labels are matched by ordinal; every ADBC partition schema must still exactly
match ExecuteQuery. Plan-only acceptance of another official query is not a full
fetch or correctness result.
