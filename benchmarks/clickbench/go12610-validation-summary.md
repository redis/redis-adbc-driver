# Go 1.26.10 rebuild validation — 2026-10-22

Status: **rebuild validation complete; full-scale repetition requirement waived
by the user for this rebuild**. Public release publication remains separate.

The toolchain upgrade in [#438](https://github.com/alberttwong/redis-adbc-driver/pull/438)
required rebuilding existing binaries. The rebuilt artifacts below embed Go
1.26.10 and come from driver source `e99cf65079f103316401a2030eacc355c26e2caa`.

## Validation basis

- macOS ARM64, Linux ARM64, and Linux AMD64 library/CLI builds were rebuilt, and
  their embedded Go versions were inspected.
- The Linux ARM64 builder image was rebuilt using a checksum-verified Go 1.26.10
  toolchain replacement. The resulting library requires GLIBC_2.17, within the
  existing GLIBC_2.28 ceiling.
- A separate 1,000-row native fixture completed all 18 statement/partition
  measurements and all nine independent comparisons.
- Snapshot restoration passed exact node-identity, per-node row-count, slot
  coverage, and indexing-readiness checks before full-scale timing.
- Five portable restore-validator tests passed; the helper was merged in
  [#450](https://github.com/alberttwong/redis-adbc-driver/pull/450).

These checks establish the recorded build provenance and fixture behavior.
They do not assert that every vulnerability in the dependency graph was audited.

## Full-scale test stopped by request

The user requested skipping the remaining long-running test and removing
full-scale acceptance as a condition **for this specific situation**. This
waiver applies to this Go 1.26.10 rebuild; it does not weaken the general API
benchmark acceptance rule or establish a completed performance comparison.

Before the stop, four of 18 planned executions and two of nine independently
validated pairs completed. All four outputs consumed 99,997,497 rows. Both pairs
passed exact source-value and cross-API multiset checks.

| Narrow full-scan trial | Statement seconds | Partition seconds | Validation |
|---|---:|---:|---|
| 1 | 1653.248860 | 779.450674 | Exact pair passed |
| 2 | 1670.808670 | 778.047520 | Exact pair passed |

The third statement execution was cancelled by user request; its partial Arrow
output was retained. Remaining measurements were not run. This is
`stopped_by_user`, not a timeout, correctness failure, or completed full-scale
benchmark. The completed trials recorded no query failures or OOMs. An earlier
pre-restore build startup failure was resolved by setting explicit Go cache paths.

Each timing includes connection/session setup, query execution or partition
planning, complete Arrow consumption, IPC writes, and connection close. Partition
timings also include startup/teardown of 16 worker processes. Output hashing and
independent correctness validation occur outside those timers. These are
supplemental scan workloads, not a complete 43-query ClickBench score or a
hardware-equivalent comparison with single-instance baselines.

The broader full-scale API work tracked by
[#421](https://github.com/alberttwong/redis-adbc-driver/issues/421) remains distinct.
This record does not close that issue.

## Preserved build identities

| Platform | Artifact | Bytes | SHA-256 |
|---|---|---:|---|
| macOS ARM64 | library | 44,768,530 | `02a1894758023c23e306afe7bdfd5a151e671a77f45722763632970cf992cc40` |
| macOS ARM64 | CLI | 45,414,114 | `c4395840c0b4d90e26616fd9a8a18c9fc4e8fabe854a4b5e64c1f4525bdd288e` |
| Linux ARM64 | library | 48,417,360 | `2227a9e62724cc80c2681e7de8001821f14704792564c441c9c39cc27677bed8` |
| Linux ARM64 | CLI | 43,712,674 | `336e7c02c9d23d1dc08707fbd40a1984270ac2be76258b5e2cea2978957557b2` |
| Linux AMD64 | library | 54,876,160 | `3972a5bc82f022e64defc26044d3a2eb9af870920cce11b15238e509e07079e2` |
| Linux AMD64 | CLI | 50,114,722 | `d92145cc2f9a9285f81ca2770a5c3116ec69c40765b700e368464b5b0e1810f6` |

## Evidence and resource closure

Completed and partial outputs, the native fixture, source archives, binaries,
settings, failures, restore checks, and the user waiver were preserved privately.
The stopped-run manifest contains 287 files totaling 7,835,656,889 bytes. Every
uploaded object's actual content was read back and hashed; an independent check
confirmed each saved object version, byte count, and SHA-256 metadata.

No benchmark scans remained active before cleanup. All 49 owned instances and
49 root volumes were independently verified deleted, along with the owned
security group, key, cleanup function, and schedule. Backups, evidence, reused
IAM resources, and log history were retained. The progress monitor was removed.

Operational scripts, infrastructure identifiers, and private artifact locations
remain outside this public summary at the user's request. This record summarizes
operator-verified private evidence; the raw run is not publicly reproducible from
this document alone. No public release or package publication is claimed.

See the linked [intent](../../docs/sdlc/changes/go12610-validation-closeout/intent.md),
[specification](../../docs/sdlc/changes/go12610-validation-closeout/spec.md), and
[plan](../../docs/sdlc/changes/go12610-validation-closeout/plan.md).
