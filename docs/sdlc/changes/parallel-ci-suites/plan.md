# Parallel CI suites plan and verification

Date: 2026-11-12. Base: `53b76248`. Requestor: Albert Wong. Author: Codex.
Status: implemented and checked locally; hosted execution pending.

See [intent](intent.md) and [specification](spec.md).

1. Inspect current workflow, architecture selector, cache actions and actual
   completed CI step timings.
2. Split long suites onto independent verified fixtures, retain original names
   as failure-propagating summary checks, and expose individual Go timings.
3. Add regression coverage for isolation, retained suite flags/architecture
   selection and summary failure propagation. Run targeted checks and lint.

## Evidence

The [completed PR run](https://github.com/redis/redis-adbc-driver/actions/runs/43257797324)
had a 2,373-second amd64 Linux job: Go tests 927 seconds, snapshot race tests
1,028 seconds, Python ADBC validation 113 seconds and Software startup 50 seconds.
The [nightly run](https://github.com/redis/redis-adbc-driver/actions/runs/43190836481)
spent 609–730 seconds building scanners on cache misses. These are historical
baselines; they do not validate the new scheduling layout.

Local checks:

- `actionlint` v1.7.7: passed for `.github/workflows/test.yml`.
- `python3 -m unittest discover -s scripts -p 'test_ci_*.py' -v`: 17 passed.
  This includes execution of the actual summary shell across all 64 combinations
  of success, failure, cancelled and skipped upstream results.
- `python3 -m unittest discover -s scripts -p test_software_setup_exec.py -v`:
  6 passed, using mocked infrastructure to exercise actual verification gates.
- YAML comparison against the base workflow: all 28 named Linux steps retained,
  including commands, options and conditions; only Go verbosity flags changed.
  Coverage and Superset jobs are unchanged. The new regression-test path selects
  full architecture coverage.
- `git diff --check`: passed.

Self-review under REVIEW.md covered correctness and failure propagation,
fixture/data/privacy isolation, and scope/evidence. No server tests were run
locally: this change rearranges unchanged tests and fixture setup. Hosted runtime,
queueing, cost and actual speedup remain unmeasured until CI runs after publication.
