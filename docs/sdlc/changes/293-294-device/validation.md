# Validation evidence and remaining gates

Date: 2026-11-04. Base source: `efb9cbfb1c280f7ba0c11667d5a3a2e6309b167d`
plus this PR's changes. Author: Codex; this is implementation evidence, not a
maintainer approval. See [plan](plan.md) and [spec](spec.md).

Environment: macOS ARM64; Go 1.27.2; CPython 3.15; PyArrow 26.0.1;
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
  **28 tests: 18 passed, 10 CUDA-only skips, zero failures.**
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

Real-device checks are explicit hardware-only tests in
`benchmarks/gpu-query/test_native_device.py` and `test_device_adapter.py`.
They include CuPy variable-width producers and consumers, non-default streams
and events, pylibcudf capsule interchange, public-manager CUDA binding,
pending-event cancellation and output retained beyond producer/stream release.
**These new hardware cases have not been executed.** Prior #215 GPU evidence
cannot validate this extension. The prior cloud configuration is no longer
reusable (its instance profile is absent); no replacement resources were created.
Operational identifiers, endpoints, credentials and raw diagnostics are excluded.

Merge/issue closure gate: run both suites with `GPU_TEST=1`, a newly built
library and a verified Software fixture on actual CUDA hardware. Record actual
source revision, GPU/driver/CUDA, Arrow/manager/Python/CuPy/pylibcudf versions,
all failures/skips, then perform maintainer review. Current CPU-only CI does not
satisfy this gate. Issues #293 and #294 must remain open until that evidence exists.
