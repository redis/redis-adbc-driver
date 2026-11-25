Target: redis/redis-adbc-driver — new issue
Title: Analysis profile: remove stale MLflow 3.17.2 strict xfail and lift the PyArrow <26 cap (fixed in MLflow 3.18.0)
Labels: bug, partner integration
Status: small enough to fix directly instead of filing. Not run: the test outcome under MLflow 3.18 is inferred from the code.
---
## Upstream changes

- https://github.com/mlflow/mlflow/issues/27358 (automatic digest fails for empty Arrow-backed pandas string columns) is closed. Fix PR https://github.com/mlflow/mlflow/pull/27359 is included in MLflow 3.18.0 (2026-11-04).
- MLflow 3.18.0 declares `pyarrow<27,>=4.0.0`, so PyArrow 26 is allowed.
- Beam's PyArrow 26 support (apache/beam#40919, PR apache/beam#40927) is merged for 2.79.0 but unreleased (latest is 2.78.0). Its cap stays for now.

## Stale repo state

- `python/pyproject.toml:33`: `analysis-tests` already requires `mlflow>=3.18.0,<4` but keeps `pyarrow>=18,<26`.
- `python/tests/test_analysis_candidates.py:90-104`: the strict xfail only matches `mlflow.__version__ == "3.17.2"` with `pandas.__version__ == "3.1.0"`. With the pinned MLflow, the call either succeeds (XPASS(strict), a failure) or re-raises a different error, so it can no longer xfail as intended.
- These still describe the 3.17.2 failure and/or the PyArrow cap:
  - `python/tests/test_analysis_candidates.py:56-57`
  - `python/README.md:114`, `:122-130`
  - `docs/integrations/mlflow.md:11`, `:29`, `:56`
  - `docs/integrations/ibis.md:9`, `:11`

## Does CI run this?

No. Consequently, the stale xfail only bites manual candidate runs.
- The integration job installs only `./python[test]` (`.github/workflows/test.yml:504`) and runs `pytest -q python/tests/` (`:522`).
- `mlflow` is not in the `test` extra (`python/pyproject.toml:24`), so `pytest.importorskip("mlflow")` (`python/tests/test_analysis_candidates.py:95`) skips the test.
- `analysis-tests` is installed only by `python/scripts/run-all-native-candidates.sh:47`, through the `analysis` profile (`python/scripts/integration_registry.py:85`).
- No workflow references that runner or extra.

## Proposed change

1. Make `test_mlflow_default_empty_digest` an ordinary assertion that the default digest works on an empty result. Keep the explicit-digest workflow test.
2. Remove `<26` from `analysis-tests` if uv resolves Ibis plus MLflow 3.18 with PyArrow 26.0.1. Ibis 12 declares `pyarrow>=10.0.1`; this is not yet verified against 26.
3. Update the text above. Keep the Beam PyArrow statement until a Beam release containing apache/beam#40927 is out.

## Acceptance

`CANDIDATE_PROFILES=analysis` passes on a verified fixture with PyArrow 26, and the MLflow empty default-digest case passes instead of xfailing.
