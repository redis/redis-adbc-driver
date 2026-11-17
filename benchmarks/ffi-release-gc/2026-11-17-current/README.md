# Current-source ownership check, 2026-11-17

Source `292ee2544fa6d48baec7091eab9e2cc28f385a3d`, after merged #462;
shipping Arrow Go **v18.9.0**. Production forced GC unchanged. This is server-free
C/ctypes/PyArrow stub correctness, with no Redis endpoint or performance claim.

The repaired builder selects the archived module's Go 1.27.4 instead of forcing
Go 1.27.3. Both source/runtime identities are retained separately from the launcher
version in [validation.json](validation.json). The Linux image's Python 3.11 can
extract the local archive after all members/types/paths are validated. No Arrow
replacement, GC omission, assertion skip or checkptr bypass was used.

| Environment and flags | Basic seven groups | Expanded lifetime probe |
|---|---|---|
| Linux/aarch64, Go 1.27.4, checkptr=2 + cgocheck2, Python 3.11.2/PyArrow 26.0.0 | Pass; checked allocator zero | Fails sliced STRUCT exact-value check in concurrent retained/rebind loop |
| Linux/aarch64, same plus race | Runtime initialization fails before checks | Runtime initialization fails before checks |
| macOS/arm64, Go 1.27.4, race + checkptr=2 + cgocheck2, Python 3.14.10/PyArrow 26.0.0 | Pass; checked allocator zero | Same sliced STRUCT failure |

The [Linux basic result](linux-strict-basic.json) and
[macOS race basic result](macos-race-basic.json) retain every original group.
Build/source/library/harness hashes and all failed statuses are retained in
`validation.json`; raw logs, absolute paths and binaries remain private.
The Linux race probes returned 66 when ThreadSanitizer could not allocate memory
before reaching ownership checks. This is not a race pass, a driver race finding,
or proof of the allocation failure's underlying cause. The Linux run used no
Software fixture; the macOS result does not replace that missing Linux gate.

The expanded probe reaches its independent-client loop and fails at stream input,
seed 1: nested `struct.x` is 0 instead of expected 1. It does not complete the
256 cycles or emit a passing lifetime report. Both platforms fail the same exact
assertion. A separate Go 1.27.4/macOS standalone module requiring Arrow v18.9.0
also fails the unchanged [C Data reproduction](../repros/struct_offset_test.go.txt):
expected `{[202 303]}`, got `{[303]}`. This confirms that upgrading the project's
pin from 18.8.0 to 18.9.0 did not clear [#460](https://github.com/redis/redis-adbc-driver/issues/460),
[upstream #1598](https://github.com/apache/arrow-go/issues/1598) /
[PR #1600](https://github.com/apache/arrow-go/pull/1600).

Build both variants separately using the repaired builder and a new private
outside-Git directory for each:

```sh
python benchmarks/ffi-release-gc/build.py --base 292ee2544fa6d48baec7091eab9e2cc28f385a3d --baseline-only --checkptr --cgo-check --work /private/new-strict
python benchmarks/ffi-release-gc/build.py --base 292ee2544fa6d48baec7091eab9e2cc28f385a3d --baseline-only --race --checkptr --cgo-check --work /private/new-race
```

Run `run.py --correctness-only` and the complete `lifetime.py` with each actual
platform library and a parent-process timeout of 120 seconds. The runner never
silently skips the STRUCT case. The standalone reproduction uses a separate Go
module with `go 1.27.4` and `require github.com/apache/arrow-go/v18 v18.9.0`.

These checks advance the merged holder fix's current-dependency qualification;
they do not pass #446's full ownership, sanitizer, retained-memory or full-path
performance gates. Earlier Go 1.27.3/Arrow 18.8.0 evidence remains historical and
is not relabeled. No latency or GC-policy comparison was performed.
