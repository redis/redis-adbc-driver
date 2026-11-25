# Plan

Date: 2026-11-24. Source: `ea16062c`. Status: implemented and locally validated.

1. Confirm upstream state: MLflow 3.18.0 on PyPI requires `pyarrow<27,>=4.0.0`;
   v3.18.0 contains mlflow#27359's merge commit. Beam 2.78.0 still requires
   `pyarrow<26`, and its tag does not contain apache/beam#40927.
2. Reproduce main's analysis profile on a verified fixture and capture failures.
3. Lift the PyArrow cap, align the pandas floor, replace the strict xfail and
   update the stale metadata expectation.
4. Add a runner contract assertion for the fixture's TLS environment, observe it
   fail, then source `go/build/test.env` in the runner.
5. Update `python/README.md`, `docs/integrations/mlflow.md` and `docs/integrations/ibis.md`.
6. Run the analysis profile through `run-all-native-candidates.sh`, both guide
   checks, the runner contracts and the strict documentation build.

Files: `python/pyproject.toml`, `python/tests/test_analysis_candidates.py`,
`python/scripts/run-candidate-tests.sh`, `scripts/test_integration_registry.py`,
the three documents above and this record.

See [intent](intent.md), [spec](spec.md) and [validation](validation.md).
