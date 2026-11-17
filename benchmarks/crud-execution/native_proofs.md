# Native proof-call attribution

This is an instrumented diagnostic for [#444](https://github.com/redis/redis-adbc-driver/issues/444),
not an optimization or baseline/candidate performance comparison. The
[change record](../../docs/sdlc/changes/native-proof-attribution/intent.md)
defines its boundaries. No shipping Go/C source or public API changes.

`native_proofs_build.py` archives an explicit source commit into a **new private
directory outside Git**. Exact checked insertions add one hook per client and
mark the actual Go ExecuteQuery context. The two `.go.txt` templates compile
only in that private archive with `driverlib,nativeproofprobe`. The original
archive, overlaid files, library and toolchain are identified in a private
manifest. Linux and the single Software proxy endpoint are required; this probe
deliberately refuses an OSS Cluster API client to avoid incomplete node coverage
or double-counted hooks. It supports multiple Software shards behind that proxy.
The builder follows the archived module's Go toolchain rather than forcing the
historical experiment's toolchain. Use Python 3.14 or newer for the builder and native runner. Archive extraction
uses the standard `data` filter, without an older-Python fallback.

From a Linux build environment, with source/Git metadata available read-only:

```sh
python3 benchmarks/crud-execution/native_proofs_build.py \
  --base FULL_SOURCE_COMMIT --work /private/new-proof-build
```

Provision an exclusively owned Software fixture using `make -C go redis-up` and
verify its effective 4x QPF. The runner also checks that configuration, six search
workers, mnp32, noeviction and TLS before/after the diagnostic. Supply private
`REDIS_URI` (authenticated `rediss://localhost:PORT/...?...protocol=3`) and
`SSL_CERT_FILE`. Use a native Linux container sharing only that owned Software
container's network namespace; use its internal data/API ports and a read-only
CA mount. The route is recorded explicitly; it is not host-published networking.
Build and dependency installation finish before the runner starts.

```sh
python -c 'import subprocess,sys; subprocess.run(sys.argv[1:], timeout=300, check=True)' \
  python benchmarks/crud-execution/native_proofs.py \
  --build /private/new-proof-build --output /private/new-proof-observations.json \
  --software-api-port OWNED_INTERNAL_API_PORT \
  --software-data-port OWNED_INTERNAL_DATA_PORT --owned-software-fixture \
  --route linux-container-shared-software-network-namespace
```

Install the recorded Python packages separately. The retained run uses
adbc-driver-manager 1.13.0, PyArrow 26.0.0 and redis-py 8.1.0. For a separate
strict correctness attempt, build into another new directory with `--race` and
run the same runner; never compare its times with the normal diagnostic. Existing
bridge defects can stop that attempt before any sample. A failure is not
permission to disable checkptr, remove GC or patch the tested bridge silently.
Use `--checkptr` for a separate non-race build with `-d=checkptr=2` when the race
runtime itself cannot start. These are distinct checks; one cannot stand in for
the other. Both selections are recorded in the result and private manifest.

The runner retains 66 samples including both warmups, complete typed-data and
raw-state checks. It creates scoped schema/source/index objects and removes them
in `finally`; it never flushes the fixture or replays SQL. A supervisor kill
requires the fixture owner's teardown. Exceptions print only their class;
`NATIVE_PROOF_PRIVATE_FAILURE_LOG` optionally retains a private traceback. All
attempts and operational manifests stay private until sanitized for publication.

`foreground` means the call carries this sample's ExecuteQuery context. It does
not prove synchronous execution, exclusive resource use or server attribution.
Unmarked overlapping calls remain explicitly separate. Calls starting before
Begin are outside observation even if they finish during a sample. Client IDs
identify pools only; they are not endpoints. Hook durations include pool,
transport and decoding, and overlapping/nested intervals must not be added as
exclusive wall time. Instrumentation/ctypes overhead is included. `execute_ns`
and `fetch_ns` measure this actual native caller; they cannot be combined with
independent Go test medians. No packet RTT, Redis CPU, release-GC attribution or
latency improvement is inferred. Nonzero `dropped_calls` invalidates a sample;
`inflight_at_end` explicitly records observations incomplete at the end boundary.
