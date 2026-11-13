# Experimental owned nullable HMGET evidence

The [November 13 report](2026-11-13/report.md) retains all 36 actual-API parser
observations and 56 complete native driver trials. Native elapsed time regressed
in every case, so the driver API adapter and dependency replacement remain
unapplied. The separate current-master parser gain is not a driver gain.

Audit with `python3 benchmarks/typed-hmget/audit.py`. The [results](2026-11-13/results.json)
include exact upstream bases/features, every patch/source/module/build hash,
negative cases, warmups and corrected/failed check outcomes.

The opt-in [runner](run.py) requires an existing official go-redis Git clone
containing the recorded v9.23.0 and master base commits. It creates fresh private
worktrees and applies the supplied patches there, preserving upstream licenses
and existing checkouts. It never changes the driver's default Go dependency.
These source-equivalent private reproductions do not promise identical binary
hashes: filesystem paths and build environments differ. The native runner builds
both variants before either timed block and writes a private reproduction manifest
with actual library/CLI hashes, Go build information and compound source/module/
overlay hashes. Keep that path-bearing build manifest private.

```sh
python benchmarks/typed-hmget/run.py parser \
  --upstream-repo /private/go-redis-clone --work /private/new-parser-run
```

Native reproduction additionally requires Go 1.27.3/cgo, the existing Python
benchmark dependencies, the exact independently typed Arrow source identified
by SHA-256, and an owned empty Redis Software fixture created with
`make -C go redis-up`. Verify active/effective 4x QPF, six workers, mnp32,
one 4-GiB noeviction RAM shard and matching four-CPU/eight-GiB container limits.
Privately set `REDIS_BENCH_URI` (strict TLS, protocol=3), `SSL_CERT_FILE`,
`REDIS_BENCH_API_URL`, `REDIS_BENCH_API_USER`, `REDIS_BENCH_API_PASSWORD` and
optionally `REDIS_BENCH_DATABASE` (default adbc-test). Never point it at shared
or production data. The unchanged harness rechecks effective settings per case,
requires emptiness, writes an ownership marker and cleans only that owned DB.

```sh
python benchmarks/typed-hmget/run.py native \
  --upstream-repo /private/go-redis-clone --source /private/source.arrow \
  --work /private/new-native-run --dedicated-empty-server
```

The runner applies the adapter in a separate overlay tree and uses a private
modfile pointing at the patched v9.23.0 worktree. The clean driver base alone
does not identify the candidate. Do not install the experimental replacement
into default go.mod or relabel a different server/source as this observation.
Retain all failures and order controls. Remove only reproduction-owned worktrees
and disposable server resources after recording final validation/cleanup.
