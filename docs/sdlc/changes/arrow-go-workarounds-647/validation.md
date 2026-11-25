# Validation

2026-11-24; base `ea16062c`, Go 1.27.4 darwin/arm64, arrow-go v18.9.0. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

## Focused tests

`TestCImportedRunEndNestedValidation` sends each case through a real Arrow C Data export and import (`cdata.ExportArrowArray`, `cdata.ImportCArray`):
- On the base commit, with the REE placeholder still in place: all 17 cases pass (8 valid, 9 invalid).
- With the change: all 17 pass again, with the same exact messages and paths, and the same `ingestColumn` status, message and empty SQLSTATE. The existing `TestCImportedRunEndValidation`, `TestCImportedNullValidation` and `TestArrowLayout*` tests pass unchanged.
- Sensitivity, with the change and a temporary `-modfile` pinning arrow-go v18.8.0, before apache/arrow-go#1421: all 17 cases fail, as does `TestCImportedRunEndValidation`. The errors are `expected at least 1 buffers for run_end_encoded<…>, got 0`, so before #1421 invalid inputs would also have been rejected for the wrong reason.
- NULL handling is still needed on v18.9.0. A `-overlay` copy with the NULL placeholder removed too makes `valid_null_values` and `valid_struct_of_sliced_null_values` fail with `field "values": arrow/array: expected at least 1 buffers for null, got 0`, and `TestCImportedNullValidation` fails as well. So the new test covers the NULL adaptation that this change keeps.
- `CGO_ENABLED=0 go test` still compiles the package tests and skips the cgo-only file.

## Unit pass

`cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: every package `ok`. The main package took 40.8 s. Integration tests skip without `REDIS_URI`, as expected.

## Full Go suite on Redis Software

`COMPOSE_PROJECT_NAME=adbc647 SOFTWARE_API_PORT=29647 SOFTWARE_DB_PORT=26647 make -C go test-software` ran on a dedicated Redis Software fixture: Redis 8.8.0, 3 shards, modules ReJSON, bf, search and timeseries. The `software-setup.sh` gate verified QPF before running: scaling factor 4, 6 Search workers, mnp routing and 32 connections. Every package was `ok`, with the main package taking 1445.8 s.

Totals: 3,420 PASS, 0 FAIL, 878 SKIP (counted from `--- PASS`, `--- FAIL` and `--- SKIP` lines). The skips all depend on configuration:
- 330 need `REDIS_SNAPSHOT_URI` or `REDIS_SNAPSHOT_CLUSTER_URI`.
- 17 are ACL checks: Software rejects `acl|setuser`, and OSS covers them.
- 6 need the owned Software API reader-lease gate.
- 5 need Flex databases.
- The rest are opt-in profiling or diagnostic tests, or subtests under skipped parents.

No Arrow validation test skipped.

## ADBC validation

Run on the same verified fixture: `software-setup.sh --exec make validate`, with adbc-drivers/validation at `e4c18a5c` and CPython 3.14.0. Result: **582 passed, 1 skipped, 3 subtests passed** in 173.3 s.
- The one skip is upstream `statement.py:205`, "Driver does not support transactions" (autocommit only).
- The new `test_layout_inventory_c_bindings[struct_of_run_end]` passed: pyarrow sends REE inside a struct through the shared library's C Bind and BindStream, full and sliced. `list_of_run_end`, `run_end_16/32/64`, `run_end_list` and `test_normalized_layout_inputs` also passed.
- The Python case was run only with the change.

Environment note: under `software-setup.sh --exec`, `SSL_CERT_FILE` points at the fixture's test CA. uv then rejects PyPI's certificate (`invalid peer certificate: UnknownIssuer`), even with `UV_NATIVE_TLS=1`. Running `uv sync` in `go/validation` first, outside the gate, and then the gated `make validate` with `UV_OFFLINE=1` avoided it. Nothing in the repository was changed for this.

## Not run

- OSS Cluster and the linux amd64/arm64 jobs: left to PR CI.
- Postgres differential: no SQL behavior changed.
- ClickBench and performance: the change only skips an adaptation copy for REE-containing types, and no speedup is claimed.
