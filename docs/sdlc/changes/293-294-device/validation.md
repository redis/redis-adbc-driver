# Validation evidence

Date: 2026-11-04. Base source: `efb9cbfb1c280f7ba0c11667d5a3a2e6309b167d`
plus this PR's changes. Author: Codex; this is implementation evidence, not a
maintainer approval. See [plan](plan.md) and [spec](spec.md).

Environment: macOS ARM64; Go 1.27.3; CPython 3.15; PyArrow 26.0.1;
adbc-driver-manager 1.13.0. No CUDA GPU is available locally.

Passed locally:

- `make -C go build`: portable shared-library build with lazy/unavailable CUDA.
- `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`: all packages passed.
  Redis-dependent tests skipped; this command is unit evidence only.
- `clang -Wall -Wextra -Werror -fsanitize=address,undefined -g
  scripts/test_device_staging.c -o /tmp/device-staging-test` and execution:
  passed. Every nonzero staging/schema allocation was failed in turn; output
  cleanup, borrowed callbacks and zero remaining allocations were asserted.
  CPU stubs deliberately abort on device buffer operations, not fake a GPU.
- Native and public adapter suite through `go/software-setup.sh --exec` after
  `make -C go redis-up`: verified authenticated TLS/RESP3 Redis Software and
  active 4x QPF, using the repository gate rather than assuming proxy policy.
  Fully fetched result values independently checked against Arrow fixtures.
  **29 tests: 19 passed, 10 CUDA-only skips, zero failures.**
- CPU tests cover all four variable layouts, empty strings/payloads, NULLs,
  sliced offsets and bitmap shifts, metadata, retained output, malformed
  offsets, size rejection before offset access, dictionaries/nesting rejection,
  capsule cleanup after unused/consumed exports, unsupported versions/extensions,
  cancellation before producer export, unavailable CUDA, and public manager Bind
  with producer release before execution.
- `CGO_ENABLED=0 go build -o /tmp/redis-device-nocgo ./cmd/redis-adbc`:
  passed. Importing CPU clients/device module in a fresh Python process loaded
  neither PyArrow nor CUDA Python packages.
- Syntax compilation and `git diff --check`: passed.

## Real CUDA validation

Executed on 2026-11-04 at source
`bcf631187ef1fff6346469277f64e55a1a6160cc` (implementation and tests;
subsequent changes only record evidence). Temporary AWS EC2 g5.xlarge,
Linux amd64, NVIDIA A10G (23,028 MiB), NVIDIA driver 595.112.07, CUDA runtime
12.9 (12090), Go 1.27.3, CPython 3.12.3, CuPy 14.2.0,
pylibcudf 26.10.2, PyArrow 26.0.1 and adbc-driver-manager 1.13.0.

Both `test_*device*.py` suites ran with `GPU_TEST=1` and the shared library,
through `go/software-setup.sh --exec`. The fixture verified authenticated
TLS/RESP3 Redis Software and active 4x QPF before executing tests. Results:
**29 tests: 28 passed, zero failures, one macOS-only unsupported-platform skip.**
That skipped case passed separately on the local Mac without CUDA. Allocation
fault injection also passed under AddressSanitizer/UndefinedBehaviorSanitizer
on the Linux host. CUDA cases were executed on hardware, not simulated by stubs.

Passed cases include:

- Independent CuPy producer buffers on non-default streams with events, UTF-8,
  binary and large offsets, empty values/arrays, NULLs and sliced offsets.
- Independent CuPy consumers verify normalized offset widths and payload bytes.
- Actual pylibcudf consumption of the adapter's device capsules and subsequent
  production back through the public adapter, with output retained after the
  external producer is deleted. Both columns are independently validated.
- Public manager CUDA Bind with producer release before ExecuteUpdate; fully
  fetched results checked against the original nullable Arrow fixture.
- Cancellation of both native BindDevice and Python adapter polling while a
  real CuPy producer event is pending; borrowed descriptors remain owned by the
  producer, whose work is explicitly completed before cleanup.
- Retained device output after early stream release, exact large integers,
  NULL/order preservation, CPU/CUDA SQL equality and overflow rejection.

The first hardware run exposed an example error: raw pylibcudf.Table exports
unnamed schema fields, so row dictionaries hid duplicate names and ingestion
rejected empty names. Added explicit `column_names` to adapter Bind, validated
names before manager binding, documented the producer contract, and compared
raw exported columns by position. The corrected full hardware run passed.

The temporary GPU host is terminated and its security group, instance profile
and IAM role removed after evidence collection (cleanup confirmation recorded
with the final PR). No operational identifiers, endpoints, credentials or raw
diagnostics are published. This establishes functional interoperability for the
listed subset on one GPU model; it makes no zero-copy or GPU speedup claim.
