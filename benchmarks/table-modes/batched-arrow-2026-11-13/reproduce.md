# Reproduce the rejected candidate

Reproduction must use Redis Software with active/effective 4x QPF, strict TLS certificate verification and RESP3, as required by AGENTS.md. The harness never provisions a server. Operational settings and credentials remain outside the repository. Use a dedicated empty fixture: each variant owns and resets that fixture. Restore all declared default native Go runtime settings; this candidate does not change GC policy.

The unit/race selection is recorded verbatim in the check command metadata. Construction controls run with Go 1.27.3, GOMAXPROCS=4:

```
go test -run '^$' -bench '^BenchmarkBatchedArrowConstruction$' -benchtime=200ms -count=3 -benchmem .
```

Each native variant uses the existing table-modes `arrow_compare.Benchmark.run_reads` and `run.read_worker`, with the timed modes restricted to `managed` and `unmanaged`. The archived baseline is 24f654b76b98f3d8960af94a3ca0230185d467a1; the candidate is 2caafbc3cef7005523d4f0eca1631c2646128d4e. The source Arrow IPC SHA-256 is a9f58367a9bb3af0e244043640e967ec618b4f96b759cd144be900030f2bf5a2. Pass `--suite reads --read-repeats 6 --dedicated-empty-server` and the actual binary/source/CLI/work paths. Use fresh separate work directories. The controller records driver/CLI/harness hashes and checks reported driver versions before trials. Run baseline first and candidate second; retain every warmup and measured result. The order within each variant alternates the eight cases by repetition, exactly as in the existing harness.

Every worker writes the entire result to Arrow IPC; the controller checks complete names/types/row counts and canonical values against the immutable source. One reader scans the complete table; four readers use eight disjoint existing range tasks. All source raw HASH values are independently checked before and after reads. These workloads have no NULLs; mixed NULL/default/NaN/error semantics are independently covered by the unit/live correctness suite, rather than inferred from native performance trials.

The public wrapper `../batched_arrow_read.py` restricts the timed mode list and delegates to the existing comparison harness and workers. It additionally performs ownership-checked database cleanup outside the timed trials, as the private controller did after each measured variant. It allows only the read suite. For each separate baseline/candidate build, run:

```sh
python3 benchmarks/table-modes/batched_arrow_read.py --source "$SOURCE_ARROW" --driver "$DRIVER_LIBRARY" --cli "$DRIVER_CLI" --version "$DRIVER_VERSION" --work "$FRESH_WORK_DIRECTORY" --suite reads --read-repeats 6 --dedicated-empty-server
```

Supply the existing table-modes private environment contract (see its README), without saving credentials in the repository. Binary build/driver version must match the variant; never infer library provenance from a subsequently switched checkout. The measured private controller additionally checked exact baseline/candidate driver and CLI hashes, effective Software/QPF/TLS/RESP3, container limits, empty database and source/harness identities, then cleaned the dedicated owned database and verified empty/index-free after each variant.
