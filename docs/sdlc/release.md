# Release and recovery runbook

Snapshot: 2026-10-20, `6f42461`. Authority for executable details:
[release.yml](../../.github/workflows/release.yml) and
[build-linux.sh](../../go/build-linux.sh). This is a library/CLI distribution
project; no production service deployment pipeline was found.

## Existing release path

The release workflow starts when a GitHub release is published, or on manual
dispatch for an existing tag. It also runs builds/tests for PRs changing the
release workflow or Linux build script, without uploading assets.

- Linux amd64 and arm64 libraries build against glibc 2.28 on AlmaLinux 8.
  The CLI is built without cgo and checked for static linking.
- Linux jobs run Go and ADBC suites on Redis Software and an OSS three-shard
  cluster. Packaged artifacts are validated on Ubuntu 20.04/22.04, AlmaLinux
  8/9 and Amazon Linux 2023 on both architectures, including CLI HASH
  check/copy/adopt where the tagged source includes that CLI.
- macOS arm64 builds the library/CLI, runs Go without a Redis test server and
  checks that the shared library loads. Integration skips there are expected.
- Tarballs include binaries, library headers where applicable, README and
  SHA-256 checksum files. The upload job waits for Linux, platform and macOS
  jobs; it attaches assets using `gh release upload --clobber`.

Manual dispatch can replace existing release assets. It also has a Go test skip
input for historical tags with incorrect tests. Any use must be explicit in the
release evidence; it is not proof that omitted behavior passed.

## Proposed release checklist

1. Identify the exact source tag/commit, changes, compatibility implications and
   actual human release decision. Named approvers are not established locally.
2. Inspect applicable PR checks and the intended release's build/validation
   results. Keep supported platform differences and skips visible.
3. Confirm version-stamped filenames, library load, CLI smoke checks and matching
   checksum files. Link the workflow run and artifacts in the release record.
4. Publish or rebuild only within the authorization for that release. Document
   asset replacement and skipped tests. The workflow starts after publication;
   it is not itself a pre-publication approval gate.
5. Record known limits, user-facing options and an application-specific recovery
   plan for any storage/metadata behavior change.

No environment approval gate or rollback job is declared in this workflow.
GitHub organization settings may add controls; they were not inspected.

## Recovery limits

For a consumer binary regression, preserve the failing version, query/repro,
logs and asset checksums. Reproduce against a disposable database. Propose a
revert or fix through review and validate the candidate before distribution.

Returning a consumer to a prior known-good driver is a candidate recovery action,
not a guaranteed data rollback. First verify that the older driver understands
the current metadata/row/index layout and that the affected application can
reconnect safely. Binary replacement cannot undo deleted HASHes or a completed
schema/data operation. Use a deployment's own verified backup/restore procedure
when data restoration is needed; none is supplied by this checkout's RAM-only,
nonpersistent local test setup.

Preserve faulty release evidence rather than rewriting tags or deleting assets
as a diagnostic shortcut. Link any incident to [maintenance](maintenance.md)
and the next change's intent.
