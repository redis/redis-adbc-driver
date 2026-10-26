# Latest dependency refresh plan and evidence

See [intent](intent.md), [specification](spec.md) and
[merged prerequisite PR #439](https://github.com/alberttwong/redis-adbc-driver/pull/439).

## Implementation

1. Queried Go module metadata, upstream release tags and PyPI stable releases.
2. Refreshed Go requirements/checksums, then ran `go get -u -t ./...` and
   `go mod tidy`. Migrated regexp2 from v1 to v2.9.1 and adapted its renamed
   capture fields; existing regex tests preserve semantics.
3. Raised all main Python floors to current releases and refreshed pgdiff's
   lockfile and harness requirements. Updated client tests to ADBC 1.12.0.
4. Added the approved separate Superset extra. Its dependencies conflict with
   current SQLAlchemy, Arrow and pandas, so CI installs it on Python 3.12;
   main environments and harnesses remain on Python 3.14. Package metadata
   allows >=3.12 specifically to retain Superset installability.
5. Updated current documentation and workflow selections. Historical evidence,
   runtime/server images and coupled DuckDB source pins were preserved.

## Final verification (local macOS arm64, 2026-10-22)

| Check | Result |
|---|---|
| `env -u REDIS_URI GOTOOLCHAIN=go1.26.10 go test -count=1 -p 1 -json ./...` in `go/` | Exit 0; all three packages pass; 471 passing / 557 skipped events, including subtests |
| `GOTOOLCHAIN=go1.26.10 make -B -C go build adbc-cli` with macOS 12 compiler/linker flags | Shared library and CLI build, exit 0 |
| `GOTOOLCHAIN=go1.26.10 go mod verify` | All modules verified |
| `GOTOOLCHAIN=go1.26.10 govulncheck ./...`, scanner v1.9.0 | Exit 0; no known vulnerabilities found |
| Install `python[test]` on Python 3.14.8 | Resolves current requirements, including PyArrow 26.0.0, SQLAlchemy 2.1.6, pandas 3.1.0, Polars 2.0.0, Dagster 1.13.26 and Dask 2026.8.0 |
| Python tests using the final installed native wheel | 11 passed, 16 skipped; exit 0 |
| `uv run --locked --project go/pgdiff ... python -m unittest discover -s go/pgdiff -p 'test_*.py'` on Python 3.14.8 | 25 tests pass |
| CI matrix helper / autotuning gate helper | 5 / 4 tests pass |
| Native wheel builder on Python 3.14.8 with setuptools 84.2.1 and wheel 0.48.0 | Builds macOS 12 arm64 wheel; installs and loads `AdbcDriverInit`; metadata requires >=3.12 |
| Isolated `python[superset]` install on Python 3.12.15 | Installs Superset 6.1.0, SQLAlchemy 1.4.54, PyArrow 18.1.0, Flask-Caching 2.3.1, cachetools 7.2.4; app initializes and resolves both Redis engine specs |
| Superset test subset without Redis | 1 passed, 2 skipped; full Redis-backed chart test not run |
| `actionlint -color=false` / `git diff --check` | Exit 0 |

Go final logs: `/tmp/redis-adbc-latest-go-tests-final.jsonl`,
`/tmp/redis-adbc-latest-build-final.log`, `/tmp/redis-adbc-latest-vuln-final.log`.
Python final logs: `/tmp/redis-adbc-latest-wheel-tests.log`,
`/tmp/redis-adbc-latest-pgdiff.log`,
`/tmp/redis-adbc-latest-superset-import-final.log`.
Temporary logs are local validation evidence, not durable CI artifacts.

No live Redis or cross-platform validation was run. Python skips include
Redis-dependent cases, Ray, the DuckDB scanner and the separate Superset test.
Initial regexp2 v2 compilation exposed renamed fields; final successful checks
include the adaptation. Superset's required greenlet 3.1.1 was confirmed to fail
compilation on Python 3.14. Flask-Caching 2.5.1 resolves but breaks Superset app
startup (`ignore_delete_many_errors`); 2.3.1 starts successfully.

## Security limits of Superset compatibility

The [PyPI metadata audit](python-advisories.json) checks installed versions,
excluding withdrawn advisories. Main Python 3.14: 75 packages checked, no findings.
Superset Python 3.12: 153 packages checked, four packages with findings:

| Package | Installed | Superset constraint | Advisory fixed versions |
|---|---|---|---|
| cryptography | 46.0.7 | >=42.0.4,<47 | 48.0.1 / 49.0.0 / 50.0.0 |
| msgpack | 1.0.8 | >=1.0.0,<1.1 | 1.2.1 |
| PyArrow | 18.1.0 | >=16.1.0,<19 | 23.0.1 |
| Pillow | 12.3.0 | >=11.0.0,<13 | 13.0.0 |

These requirements originate in latest Superset 6.1.0; its bounds exclude the
fixed releases. They cannot be remediated by normal dependency resolution while
keeping this Superset release. No suppressions or forced incompatible installs
were added. The requester approved preserving the isolated compatibility extra;
that decision is not an approval of vulnerability exploitability or remediation.
This is a published-advisory metadata check, not proof of exposure in this driver
or a claim that every allowed environment is CVE-free.

## Review and release

Self-review follows `REVIEW.md`: field renames preserve rune-based offsets and
existing deadlines; compatibility installs are separated; obsolete ADBC 1.8 tests
are removed consistently with the new floor. Maintainer approval and hosted CI
are pending. No release is published or deployment modified. Released binaries
need rebuilding to include updated Go libraries.
