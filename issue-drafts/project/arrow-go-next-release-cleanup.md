Target: redis/redis-adbc-driver — new issue
Title: Remove Arrow Go workarounds that upstream has fixed (now, and after the next arrow-go release)
Labels: enhancement
---
### Problem

The driver carries several workarounds for Arrow Go v18.9.0 behavior (`go/go.mod:8`). Upstream has since fixed most of them. One fix is already in v18.9.0, and the others are merged on arrow-go main but not yet released. Keeping the workarounds costs extra copies and validation code, and they can hide upstream regressions once the fixes ship.

| Workaround | Upstream fix | In v18.9.0? |
|---|---|---|
| Run-end-encoded placeholder buffer before `array.ValidateFull` (`go/arrow_validate.go:13`, the `RUN_END_ENCODED` conditions at lines 28 and 45) | https://github.com/apache/arrow-go/pull/1421 | **Yes.** Checked locally on v18.9.0: a top-level REE array imported through C Data with 0 buffers passes `ValidateFull`. REE nested inside other types was not tested. |
| NULL placeholder buffer for C Data imports (`go/arrow_validate.go:13`) and IPC-decoded arrays (`go/partition_retained_ipc.go:416`) | https://github.com/apache/arrow-go/pull/1550 (merged 2026-11-13) | No. Checked locally: v18.9.0 still fails with `expected at least 1 buffers for null, got 0`. |
| NULL Map entries/keys value check (`go/pkg/input_preflight.c:63`; the schema nullability-flag checks at lines 37–40 are separate and stay) | https://github.com/apache/arrow-go/pull/1570 (for https://github.com/apache/arrow-go/issues/1557) | No |
| Sliced STRUCT C Data round trip (#460) | https://github.com/apache/arrow-go/pull/1600 (for https://github.com/apache/arrow-go/issues/1598) | No |
| Sparse union child length and type-code preflight (`go/pkg/input_preflight.c:9`) | Not fixed upstream. Export side only: https://github.com/apache/arrow-go/pull/1641. An import-side report is drafted separately. | — |

### Desired behavior

1. **Now:** add a nested-REE validation test (for example, REE inside a struct). If it passes on v18.9.0 without the placeholder, drop the `RUN_END_ENCODED` conditions from `needsValidationPlaceholder` and `validationData`. Keep the NULL handling.
2. **After the next arrow-go release** that contains apache/arrow-go#1550, apache/arrow-go#1570 and apache/arrow-go#1600:
   - bump `github.com/apache/arrow-go/v18`;
   - remove the NULL placeholder code in both files and the Map-key preflight;
   - re-qualify #460 and close it if it passes.
3. Keep the sparse-union preflight until the importer returns errors for those inputs.

### How to verify

- Keep or add focused tests for each case:
  - C Data import of NULL and REE arrays, alone and nested in a struct;
  - IPC-decoded NULL arrays in retained partitions;
  - Map entries and keys containing NULLs;
  - sliced STRUCT inputs.

  Each must still reach `ValidateFull` with the same accept/reject outcome, and the same SQLSTATE, after the workaround is removed.
- Unit pass: `cd go && env -u REDIS_URI go test -count=1 -p 1 ./...`.
- Full Software suite: `make -C go test-software`.
- ADBC validation: `REDIS_URI=... make -C go validate`, reporting skips.
