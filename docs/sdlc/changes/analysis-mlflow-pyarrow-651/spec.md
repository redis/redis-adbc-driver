# Specification

Date: 2026-11-24. Requestor: Albert Wong ([issue #651](https://github.com/redis/redis-adbc-driver/issues/651)).
Source: `ea16062c`. Status: implemented and locally validated.

- `analysis-tests` requires `pyarrow>=26.0.1` and `pandas>=3.1.1,<4`, the floors
  the other extras use. pandas 3 is the upstream bug's condition: its default
  string dtype is Arrow-backed, while pandas 2.3 would not exercise the fix. uv
  resolves the extra on Python 3.14 with Ibis 12.0.0, DuckDB 2.0.1, MLflow 3.18.0,
  pandas 3.1.1 and PyArrow 26.0.1.
- `test_mlflow_default_empty_digest` has no xfail. It asserts that the empty
  native result's string column is Arrow-backed pandas `StringDtype` (the
  upstream bug's precondition), that the default digest is a non-empty string,
  and that the profile is zero rows and zero elements.
- `test_mlflow_dataset_tracking` keeps the explicit-digest option for its empty
  case. Its exact Arrow comparison with `check_metadata=True` now expects the
  canonical `ARROW:FLIGHT:SQL:TYPE_NAME` field metadata that query results carry
  since [#454](https://github.com/redis/redis-adbc-driver/pull/454), as the Ray
  test already does since #455. The metadata is asserted, not ignored.
- `run-candidate-tests.sh` sources `go/build/test.env` after `make -C go redis-up`,
  so tests receive the TLS fixture's authenticated `rediss://` URI and its CA as
  `SSL_CERT_FILE`. The earlier `redis://127.0.0.1:$SOFTWARE_DB_PORT/0` export
  predates the TLS-only Software fixture and is rejected by `python/tests/conftest.py`.
- The MLflow guide uses the default digest and requires MLflow 3.18.0; it and the
  Ibis guide install PyArrow 26. The Python README states the Beam-only cap.

Risks: the guide and test rely on MLflow's default digest for empty data, which
older MLflow releases cannot compute; the extra and guide floors exclude them.

See [intent](intent.md), [plan](plan.md) and [validation](validation.md).
