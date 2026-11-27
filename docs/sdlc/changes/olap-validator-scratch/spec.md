# Specification

2026-11-27; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

## Diagnosis

In `benchmarks/olap-release/isolation.py` the validator (`validation_worker` → `validate_files` → `sorted_rows`) reads each verified source, writes each sorted 4,096-row chunk to `sort-<n>` with `dump_objects(..., sync=False)`, and closes it. It merges runs if there are several, then reopens the final run by path and decodes it with `verified_objects`. Before this change those runs lived in temporary directories under the per-query artifact directory, which is in the private profile, so on the bind mount. Q72 and Q78 each had one run per side.

Both records have `objects_decoded: 0` and no `decode_offset`. In `verified_objects` that combination has one path: the first `pickle.load` hit `EOFError` with nothing consumed (`stream.size == offset == 0`), so the loop ended cleanly. The post-loop check then compared 0 consumed bytes with the writer's record and failed. In other words, **the decoder's reopen found an empty file.**

`_artifact_error` then reopened the file again (`_inspect_artifact`), and the record's "read back" size, digest and zero-page fields come from that second read:
- **Q72:** the second read found 20,839 bytes, all zero: `468bbb87a0c2dcf2` is the SHA-256 prefix of 20,839 zero bytes.
- **Q78:** the second read found exactly the recorded bytes.

**So the matching hashes in Q78 do not contradict the failure.** They describe a later read than the one that failed, and the old record and message labeled both as "read back". Nothing else writes in that private directory.

That is a read-after-write visibility lag on the Docker Desktop bind mount (`virtiofs`). An unsynced file this process had just written and closed reopened at size 0. Moments later it appeared full-size with zero data, and then with its real bytes. A stress loop in the same client image reproduced all three states on the bind mount and none on the container's own `/tmp` ([validation](validation.md#bind-mount-stress)).

Why move the scratch rather than apply the other candidate fixes:
- **fsync:** on this mount, fsynced files also reopened empty.
- **Reading through the still-open handle:** returned empty or short reads too.
- **Retry:** an immediate re-read often still found zeros (5 of 20 to 36 of 60 kept cases per run).

Only storage off the bind mount had no failures. The scratch never leaves the validator process, so nothing requires it to be on the mount.

## Contract

Validator scratch:
- `run_query` creates one mode-0700 `olap-validate-*` directory per query in the default temporary directory (`tempfile`: `TMPDIR`, else `/tmp`), next to the private `query-*` artifact directory, and removes it when the query ends, even after a validator timeout or kill.
- The validator gets it through a new internal `--scratch` argument; `run.py` refuses `--phase validate` without it. Engine workers do not get it.
- `validation_worker` passes it to `validate_files`, which still makes one temporary directory per side inside it. Result files are still read from, and retained in, the private profile.
- Each query record gains `validation_scratch_filesystem`: `"separate"` when the scratch directory's `st_dev` differs from the artifact directory's, otherwise `"same"`. No path is recorded.

Artifact problems:
- `_artifact_error` receives the decoder's own `_DigestReader` and records `bytes_consumed` and `sha256_consumed` (16-hex prefix): exactly the bytes the decoder consumed, up to the failure.
- `bytes_read_back`, `sha256_read_back`, `zero_bytes_at_offset` and `first_zero_page_offset` keep their names. They are documented as coming from a separate re-read after the failure.
- The message adds "the decoder consumed N bytes sha256 …" and words the second read as "a later re-read found N bytes sha256 …". When that re-read equals the record, it says "matching the record".
- `ARTIFACT_PROBLEM_FIELDS` adds the two new fields to the public copy.

Unchanged:
- No retry or second decode. A file whose decoded bytes differ from the record is still `validation_failed` with retained inputs, never `mismatch` and never a pass.
- Engine result files are still fsynced and exchanged through the private profile.
- Sort/merge semantics, chunk size, fan-in and tolerance are unchanged, and scratch runs are still unsynced and verified.

## Risks

- A `/tmp` that is a small tmpfs now holds the scratch. It counts against memory, including a worker cgroup's limit. The README says to set `TMPDIR` to local disk in that case. The release client container's `/tmp` is the container's overlay filesystem.
- A supervisor that kills the coordinator's whole process group leaves an `olap-validate-*` directory behind, as it already left `query-*` in the profile. The release container runs with `--rm`.
- A `TMPDIR` pointing back into the bind mount restores the old exposure. The record then says `"same"` only if it is the same device; a separate bind mount of the same host folder shows `"separate"`.

## Acceptance

- New tests fail on unmodified main:
  - a synthetic bind mount whose unsynced writes reopen empty makes `validation_worker` fail with the Q72/Q78 message;
  - the coordinator passes no local scratch;
  - artifact problems lack the decoder's consumed bytes.
- With the change, all harness tests pass, including the existing #641 regressions, with only the re-read wording updated.
