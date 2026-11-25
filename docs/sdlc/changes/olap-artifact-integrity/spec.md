# Specification

2026-11-24; see [intent](intent.md), [plan](plan.md), [validation](validation.md).

Artifacts (`benchmarks/olap-release/isolation.py`):
- `dump_rows` / `dump_objects` hash the exact bytes passed to the file and fsync engine result files. They return `{"bytes", "sha256"}`. The engine worker records that per repeat under `artifacts` in its private status file. It is not copied into the public record.
- `verified_objects` decodes through a reader that hashes exactly the consumed bytes, with no read-ahead. A decode error reports its byte offset and how many objects decoded. After the last object, the size and SHA-256 must equal the record. Either failure raises `ArtifactError` (a `ValueError`) with a bounded `detail`. The detail also includes the zero-byte run at the failure offset, the first all-zero 4 KiB page, and recorded versus read-back sizes and 16-hex digest prefixes.
- The validator's sort and merge scratch runs are written and read the same way, without fsync.
- `sorted_rows` yields the `(columns, count)` header only after the whole source has been read and verified. `validate_files` compares shapes after both inputs are verified.
- `validation_worker` requires a recorded digest for the reference and for every repeat. An `ArtifactError` is `validation_failed`; only value or shape differences are `mismatch`.

Coordinator:
- For a validator failure, the record gains `<engine>_artifact_problem`, holding only the listed fields with strings redacted through `safe_error`.
- After any validation failure (mismatch, artifact failure or validator timeout), the engine's pickles, status and validation status move to `failed-artifacts/<suite>-q<query>-<engine>-<UTC>-<random>/` in the private profile. That folder is created mode 0700, and the reference files are copied into it. The record names the folder relative to the profile in `<engine>_retained_artifacts`. A retention `OSError` is recorded in `<engine>_retention_error` and does not change the status.
- Successful validations keep today's cleanup. Later engines still validate against the original reference.

Acceptance:
- Zero bytes appended to a result, a zeroed reference, and a rewritten but decodable result each fail before the change, either as a bare pickle error or as a `mismatch`. After it, each is `validation_failed` with its file, role and location, and the inputs are retained.
- A missing digest never passes. A damaged scratch merge run is detected.
- Recorded digests equal the file bytes, and fsync is called.
- Existing harness tests, including multiset/tolerance validation and the secret and path checks on records, pass unchanged apart from the header-first `sorted_rows` contract.
- Validating 1M rows is not measurably slower.
