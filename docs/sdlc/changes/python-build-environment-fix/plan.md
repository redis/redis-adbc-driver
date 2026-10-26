# Plan and evidence

See [intent](intent.md) and [specification](spec.md).

1. Inspect failing jobs from merged PR #440 and reproduce both failures locally.
2. Align the hidden validation Python pin and resolve release backend requirements
   from the same source manifest used by the wheel build.
3. Verify interpreter selection, environment synchronization, test collection,
   native wheel construction, and workflow syntax. Open a follow-up PR.

## CI diagnosis

- [Test run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39810635908):
  all eight failed jobs fail at Python ADBC validation. The inspected amd64
  OSS-single log reports Python 3.13.17 incompatible with project >=3.14.
- [Release run](https://github.com/alberttwong/redis-adbc-driver/actions/runs/39810636028):
  macOS fails with wheel >=0.48.0 required, 0.47.0 found. Linux arm64's
  Redis Software step fails with the same stale Python 3.13 validation pin.
- The inspected test log already passes Python integration tests (25 passed,
  2 skipped) and isolated Superset tests (3 passed) before the validation failure.

## Local verification (macOS arm64)

- Before pin fix: `uv run --no-sync python --version` in `go/validation` exits 2
  with Python 3.13.16 incompatible with project >=3.14.
- Before backend fix: use `uv run --no-project --with build
  --with 'setuptools>=84.2.1' --with 'wheel==0.47.0' --with packaging` to run
  `scripts/build_python_wheel.py` against the native library. It exits 1 with
  the same unmet wheel >=0.48.0 requirement.
- After pin fix: the same interpreter-selection command exits 0 with Python
  3.14.8. `uv sync` succeeds; `uv run --no-sync pytest --collect-only -q tests/`
  collects 491 tests, exit 0. Collection does not run Redis tests.
- After backend fix: extract manifest requirements and run the new
  `uv run --no-project --with build --with packaging --with-requirements <file>`
  command using the same temporary uv cache. It builds the native macOS 12 arm64
  wheel successfully with declared setuptools/wheel minimums enforced.
- `actionlint -color=false` and `git diff --check`: exit 0.

Full Redis tests and Linux wheel builds were not repeated locally. The fix does
not relax any requirements, remove checks or enlarge skip lists. Self-review
confirms both release paths use the source manifest; maintainer review and
hosted checks are pending. No release was published or deployment changed.
