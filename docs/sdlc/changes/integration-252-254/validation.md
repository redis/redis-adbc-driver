# Validation: R, Datasets and Ray TLS follow-ups

Date: 2026-10-30. Source base: `91064e6` plus this change.
Requestor: Albert Wong. Status: focused verification complete; upstream reports submitted; dependency fixes/releases pending. [Intent](intent.md) · [Spec](spec.md) · [Plan](plan.md)

## Results

Counts extracted from the current JUnit reports are saved in [results.json](results.json).
Raw logs, endpoint credentials and TLS private material are not committed.

| Check | Passed | Failed | Errors | Skipped | Expected failure |
|---|---:|---:|---:|---:|---:|
| Installed adbi 0.1.3: native R reads and four binding cases | 1 | 0 | 0 | 0 | 4 |
| Unpatched current upstream adbi: binding cases, `--runxfail` | 0 | 4 | 0 | 0 | 0 |
| Patched upstream adbi: reads/binding, `--runxfail` | 5 | 0 | 0 | 0 | 0 |
| Installed Datasets 5.1.0: local and ADBC persistence | 3 | 0 | 0 | 0 | 2 |
| Patched Datasets 5.1.0: same tests, `--runxfail` | 5 | 0 | 0 | 0 | 0 |
| Patched Datasets: proposed upstream regression variants | 3 | 0 | 0 | 0 | 0 |
| Real private-CA Ray datasource profile | 17 | 0 | 0 | 0 | 0 |
| Launcher ownership/cleanup/status accounting unit checks | 6 | 0 | 0 | 0 | 0 |

The raw installed R cases also fail four times with `--runxfail`. The independent
unpatched Datasets default-sharding check yields one pass (nonempty) and one
failure (empty) with `--runxfail`. Those are deliberate reproductions, not passing
compatibility results. Quarto was deselected, not skipped. The ten-profile native
sweep and unrelated Go suites were not rerun.

## Environment and native server

macOS arm64; Python 3.12.16; R 4.6.2; DBI 1.3.0;
adbcdrivermanager 0.25.0; nanoarrow 0.9.0; Python ADBC manager 1.13.0;
PyArrow 26.0.0; Datasets 5.1.0; Ray 2.59.0. Build: `make -C go build` with a
private writable GOCACHE. No driver implementation was changed.

Redis Software image `redislabs/redis:8.2.0-78.18`, Redis 8.6.2, one shard, Search.
`make -C go redis-up` verified active scaling factor 4 and effective routing and
workers before R/Datasets tests. The dedicated Ray project verified the same
configuration before converting its proxy to TLS. The reused fixture helper
then verified active 4x QPF, six Search workers, `mnp`, 32 connections, management
API CA verification, database TLS and plaintext rejection before Ray execution.

## #252: preserve ADBC metadata and fix upstream classification

The [ADBC parameter-schema contract](https://arrow.apache.org/adbc/20/cpp/api/group__adbc-statement.html)
requires empty names for unnamed parameters. Tests assert that contract and
parameter count before binding. They check integer filtering, multiple integer
parameters, NULL text and mixed NULL/integer parameters, then bind/fetch each
result again. adbi 0.1.3 rejects all four with the precise tracked error. Its
`dbBindArrow` delegates to the same validation and is not a workaround.

The current upstream source, pinned to
`ac5d3294597c453c8cb1c278b9536cd9afb3eae2` (DESCRIPTION version 0.1.4), also
fails all four. An isolated installation of that same source with
[the proposed patch](upstream/adbi-empty-parameter-names.patch) passes the four
cases and the native read test. The patch adds `nzchar(names(schema))` to the
named-schema predicate; driver metadata and parameter values are unchanged.
The additional upstream testthat regression is prepared but was not run as part
of the upstream package's full test suite.

The installed package's strict expected failures remain. A passing supported
package must remove them; an unrelated error or unexpected success fails CI.
Submitted [adbi #127](https://github.com/r-dbi/adbi/issues/127), including the
proposed fix, regression test and local validation. The local
[report record](upstream/adbi-report.md) links the submission. No released fix
is claimed.

## #253: default-sharding regression and retained empty mitigation

An independent typed Arrow table and fetched ADBC table both reproduce the exact
Datasets 5.1.0 empty reload failure. The expected failure checks the package
version, the exact IndexError message and an empty saved `_data_files` list.
Other exceptions/versions are not accepted; successful default persistence is a
strict XPASS until reviewed. Nonempty default saves and explicit single-shard
empty saves retain feature and row checks.

[The proposed patch](upstream/datasets-empty-shards.patch) keeps at least one
automatically calculated shard. It is based on upstream
`62e0b421bb996e8536d28f4fea662a4045bab82d`; that source still contains the
zero-row shard cap. Applied to a private copy of installed Datasets 5.1.0, it
passes both local cases and all three ADBC cases with `--runxfail`. The proposed
upstream test function was also executed independently: default options,
`num_proc=2` and `max_shard_size='1KB'` all preserve zero rows, features, exact
schema and one recorded shard. The full upstream test suite was not run.

Upstream issue searches for empty save_to_disk and zero shards found no equivalent
in those results on this date. Submitted [Datasets #8912](https://github.com/huggingface/datasets/issues/8912)
with the independent repro, proposed fix and tested regression variants;
[local record](upstream/datasets-report.md). The explicit-shard mitigation remains until a supported
released fix passes; no fixed package version is claimed.

## #254: real worker TLS and fixture lifecycle

```sh
PYTHON=/path/to/core-venv/bin/python \
  CANDIDATE_JUNIT_XML=/path/to/private/results/ray-tls.xml \
  python/scripts/run-candidate-tests.sh ray-tls
```

The command was run unchanged against a fresh owned Software fixture. It passed
all 17 datasource cases, including complete rows/schema in a worker whose PID
differs from the parent, certificate verification rejection from that worker
without the private CA, streaming, replay, retries, cancellation and empty data.
Three existing Ray DataContext deprecation warnings remain visible.

The initial run passed 16 cases and exposed the new positive test's use of only
one of the planner's default partitions. Setting `partition_count=1` makes the
positive assertion require the complete source. The final run passed all cases.
Both runs removed their container, network and private fixture directory.

The six deterministic launcher tests exercise success, failing pytest, failed
setup, pre-existing project refusal, failed cleanup and separate report counts:

```sh
python3 -m unittest discover -s scripts -p test_candidate_tls_runner.py -v
bash -n python/scripts/run-candidate-tests.sh python/scripts/run-all-native-candidates.sh python/scripts/run-ray-tls-candidates.sh
```

Shell/Python syntax and `git diff --check` passed. The fixture uses only the
Redis connection's CA option; global/package trust is unchanged. Plaintext Ray
coverage remains separate and retains its reported TLS skip.

## Review and remaining work

Correctness: real reads were fully fetched and compared with complete source
rows/schema; precise dependency failures remain visible. Lifecycle/security:
pre-existing Compose projects are refused, owned resources are removed on error,
cleanup failure returns a failure, and raw fixture material stays private.
Intent/evidence: no driver renaming, interpolation, upstream endorsement, release
or full-sweep claim. This agent review is not maintainer approval.

#254's implementation and local coverage are complete. #252/#253 have upstream
reports and still need maintainer resolution and supported release validation. The
two proposed fixes have only been validated in isolated local dependencies.


## Upstream submission evidence

On October 30, 2026, CLI `createIssue` requests were rejected by the configured
token for both upstream repositories; neither CLI attempt created an issue.
The user authorized browser submission and explicitly allowed unlabeled reports
because external reporters cannot assign labels in the upstream forms, overriding
the local AGENTS.md label requirement for these two submissions only.

Browser submission succeeded as alberttwong. Both published pages visibly showed
the issue title, open state, complete report and proposed fixes:

- [r-dbi/adbi #127](https://github.com/r-dbi/adbi/issues/127)
- [huggingface/datasets #8912](https://github.com/huggingface/datasets/issues/8912)

The reports link their Redis integration trackers. This records submission, not
upstream acceptance, merged code or released package validation.


## Upstream fix pull requests

On October 30, 2026, submitted ready-for-review fix PRs linked with `Fixes` to
the corresponding upstream issues:

| Project | Issue | Fix PR | Commit | Additional validation |
|---|---|---|---|---|
| adbi | [#127](https://github.com/r-dbi/adbi/issues/127) | [#128](https://github.com/r-dbi/adbi/pull/128) | `327e4ae02050923bf6c4fef29e09e50709f5e298` | Upstream parameter suite: 78 assertions passed, no failures/warnings/skips. |
| Datasets | [#8912](https://github.com/huggingface/datasets/issues/8912) | [#8913](https://github.com/huggingface/datasets/pull/8913) | `82808bf924b677e8b565a27472bf6f5658e37a98` | Focused persistence tests: 7 passed, 2 optional Polars skips; existing serialization/saved-format tests: 5 passed; Ruff lint/format checks passed. |

Both PRs target upstream main and contain only the fix and regression tests.
The adbi patch was tested with testthat, adbcsqlite, DBItest and withr in an
isolated R library. Datasets was installed editable from current upstream source
in a separate Python 3.12 environment. Its multiprocessing checks required an
unsandboxed local socket; the sandboxed run failed with PermissionError, and the
unchanged tests passed outside that sandbox. No assertions/skips were weakened.
The full upstream suites were not run.

Git pushes succeeded. CLI PR creation and the GitHub connector were rejected by
their configured credentials, so the signed-in browser submitted both PRs.
Their published pages and GitHub closing-issue references were verified. Datasets
CI requires maintainer approval for fork workflows; the PR is open and ready for
review. This is submission evidence, not a merge or released dependency fix.
