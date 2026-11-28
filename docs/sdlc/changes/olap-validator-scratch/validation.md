# Validation

2026-11-27 (UTC), base main `e57172a7`. See [intent](intent.md), [spec](spec.md), [plan](plan.md).

**Result.**
- **The bind mount reproduces both retained records' shapes outside the harness:** a file reopened empty, then a re-read found it full-size as zeros or as its real bytes. The container's `/tmp` never did.
- **On that mount, neither fsync nor reading the open handle prevents it.** It was seen only within about a millisecond of the write, which is when the validator reopens its scratch.
- **With the validator's scratch moved off the bind mount,** the synthetic-mount regression passes, and so do 30 end-to-end coordinator queries on a real bind mount.
- **No Redis server was used:** no driver or Redis code path changed.

## Retained records

Only the JSON status files in `failed-artifacts/tpcds-q72-duckdb-20261126T224859Z-*` and `failed-artifacts/tpcds-q78-duckdb-20261127T005505Z-*` (private profiles, not published) were read; the trusted pickles were not opened.

| | Q72 (TPC-DS SF1) | Q78 (TPC-DS SF 0.1) |
|---|---|---|
| Engine result files | 4 × 13,019 bytes, all recorded `8db1a014…` | 4 × 11,222 bytes, all recorded `0f774cd9…` |
| Result shape | 100 rows × 6 columns | 100 rows × 10 columns |
| Failure | `validator scratch sort-0`: "bytes differ from the writer's record after 0 objects" | same |
| `decode_offset` | absent | absent |
| Recorded | 20,839 bytes `ffc9fc791a45fdd6` | 20,280 bytes `b56b0b6bbf6a388f` |
| Error path's re-read | 20,839 bytes `468bbb87a0c2dcf2`, first zero page 0 | 20,280 bytes `b56b0b6bbf6a388f` |

`468bbb87a0c2dcf2` is the SHA-256 prefix of 20,839 zero bytes (`hashlib.sha256(bytes(20839))`). With no decode offset and 0 objects, the decoder consumed 0 bytes (see the [spec](spec.md#diagnosis)).

## Bind-mount stress

[`evidence/scratch_stress.py`](evidence/scratch_stress.py), in `adbc-olap-release-client:20261123` (`446def603bd9`, Python 3.14.10). Host: Docker Desktop 29.9.3 on macOS 26.7.3, Apple silicon, VM with 16 CPUs, 31.5 GiB, kernel 7.0.14-linuxkit. Two Redis Software fixtures and another client container from other sessions were running.
- The bind mount is `host on /mnt/bind type virtiofs` (a folder under `go/build/` in this worktree).
- The container's `/tmp` is on its `overlay` root.

Each iteration repeats one `validate_files` call's scratch pattern:
- make a fresh directory with two subdirectories;
- write a 20,839-byte and a 20,280-byte file (the Q72/Q78 scratch sizes);
- close both, then reopen and read both;
- re-read any differing file at once.

Run 1: 240 s, two concurrent processes, 2 CPUs, unsynced writes.

| Location | Iterations | Reads | Empty on reopen | Other first-read differences |
|---|---:|---:|---:|---:|
| Bind mount | 66,061 | 132,122 | 37 | 0 |
| Container `/tmp` | 465,595 | 931,190 | 0 | 0 |

Run 1 kept details for the first 20 of its 37 empty reads. Every one of those immediate re-reads was full-size:
- 14 found the recorded bytes (the Q78 shape);
- 5 found all zeros (the Q72 shape);
- 1 found other bytes, neither the record nor all zeros.

That is the sequence the records imply: empty, then full-size zeros, then the real bytes.

Run 2: 300 s, three concurrent processes on the bind mount, 3 CPUs. Each process wrote both files, with `flush()`, then read them:

| Mode | Iterations | Reads | Empty | All zeros | Short |
|---|---:|---:|---:|---:|---:|
| Close, reopen (as the scratch runs did) | 42,477 | 84,954 | 250 | 1 | 0 |
| `fsync` before close, reopen (as engine result files) | 36,035 | 72,070 | 73 | 0 | 0 |
| No close: `seek(0)` and read the same `w+b` handle | 59,694 | 119,388 | 14 | 0 | 7 |

- **fsync is not a read-after-write barrier on this mount, and neither is reading the open handle.** Neither of those candidate fixes would have prevented the failure.
- With three processes on the mount, the close/reopen rate rose from 0.03% to 0.3% of reads.
- In the details kept, a reopen re-read after an empty read found:
  - after close/reopen: 23 the recorded bytes, 35 all zeros, 2 other bytes;
  - after fsync: 22, 36 and 2.
  - All 21 kept same-handle failures re-read correctly.
- **A retry is not a fix either.** In run 2, 35 and 36 of the 60 kept immediate re-reads found zeros (5 of 20 in run 1), so one retry would still fail often. Accepting bytes the first read did not see would also weaken the check.

Run 3: 240 s, four concurrent processes on the bind mount, 4 CPUs. Each process fsynced both files, waited a fixed delay, then reopened them:

| Delay before reopening | Iterations | Reads | Empty |
|---:|---:|---:|---:|
| 0 ms | 36,488 | 72,976 | 17 |
| 1 ms | 28,537 | 57,074 | 0 |
| 10 ms | 13,240 | 26,480 | 0 |
| 100 ms | 2,200 | 4,400 | 0 |

The window is about a millisecond:
- **Validator scratch:** each sort run is reopened within it. The result side's `sort-0` is reopened right after it is written.
- **Engine result files:** only a fresh validator process reopens them, after the engine has exited, so after process start-up and imports. That fits the local result files: their only artifact problems since [#641](https://github.com/redis/redis-adbc-driver/issues/641)'s fix are these two scratch records, with no fsynced reference or result file.

## Tests

`python3 -m unittest discover -s benchmarks/olap-release -p 'test_*.py'`:

- **Failing first:** on unmodified main, with only the test changes, 97 tests ran with 3 failures and 2 errors:
  - `test_validator_scratch_never_uses_the_shared_artifact_directory` failed with the records' own message: `validator scratch sort-0 did not read back as written: bytes differ from the writer's record after 0 objects; recorded 17290 bytes …; read back 0 bytes sha256 e3b0c44298fc1c14`.
  - `test_coordinator_gives_the_validator_local_scratch_outside_the_profile` failed because the validator got no `--scratch`.
  - `test_artifact_problem_separates_the_decoders_bytes_from_a_later_reread` (zeros and recorded subtests) errored with no `bytes_consumed`.
  - `test_zero_filled_result_artifact_is_located_and_retained` failed on the new re-read wording.
- **After the change:** 97 tests, OK, 1 skipped, both on macOS (Python 3.15.2) and in the release client image on Linux (Python 3.14.10, the CI version, source mounted read-only). The skip is `WorkerMemoryCgroupTests.test_worker_oom_is_contained_and_recorded`, which needs `OLAP_HARNESS_CGROUP_TESTS=1` in a disposable privileged container. CI's `worker-memory-cgroup` job runs it; it was not run locally.
- The synthetic Q72 fixture builds its zeros explicitly (`bytes(len(data))`) and asserts that the file contains a 4 KiB zero run.

## Coordinator on a real bind mount

[`evidence/coordinator_bind_mount.py`](evidence/coordinator_bind_mount.py) ran the changed coordinator in the release client image with the harness's synthetic engines. Each query had a fresh private profile on a virtiofs bind mount; no database was used.

- 30 queries × 3 engines: every record had `validation_scratch_filesystem: "separate"` and `ok` for all three engines.
- There were no artifact problems.
- No `query-*` or `olap-validate-*` directory was left behind.

Thirty queries make about 720 scratch reads. At run 1's empty-read rate (37 in 132,122) the old layout would expect about 0.2 failures there, so this run shows that the new layout works, not that the old one failed.

## Not established

- **Why virtiofs on Docker Desktop shows an unsynced file empty after close:** the defect is outside this repository and was not traced further.
- **Engine result files** still cross processes on the bind mount. Run 3 found no empty reads once 1 ms had passed, but that bounds the rate rather than proving it zero, and heavier host load might widen the window. They are still digest-verified, so a recurrence would be `validation_failed` with `role` `reference` or `result` and `bytes_consumed`, never a pass or a `mismatch`. If one appears, move the per-query artifact directory off the mount as well and copy only failed files into `failed-artifacts/`.
- **Live harness runs:** no TPC-DS rerun on a Flex fixture was made with this change. A recurrence would now show `bytes_consumed` and `validation_scratch_filesystem` in the record.
- **Review:** human review is pending.
