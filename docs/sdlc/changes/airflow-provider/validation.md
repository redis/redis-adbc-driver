# Validation: Redis ADBC provider

Date: 2026-11-12. Base: `b4783b64`, with this working-tree change.
See [intent](intent.md), [spec](spec.md), and [plan](plan.md).

## Environment and prerequisites

macOS arm64; Python 3.14.10; Airflow 3.3.2; Apache Arrow provider 0.1.1;
common-sql 2.1.1; ADBC driver manager 1.13.0. Native library rebuilt using
`make -C go build`, preserving the version linker flag.

Redis Software 8.8.0, three RAM shards, Search, TLS and authentication. A fresh
isolated Compose fixture was provisioned with `make -C go redis-up` and tests
were launched with `software-setup.sh --exec`. Its management gate verified
active QPF scaling factor 4, six Search workers, mnp routing, 32 connections,
noeviction and active primary shards before testing. Operational identifiers,
credentials and local logs are intentionally excluded from this record.

The first live run against the existing default fixture failed before SQL:
its stored managed-table format was incompatible with the current native
library. That fixture was left intact. Docker's automatic network pools were
exhausted; a private temporary Compose override supplied an unused subnet for
the isolated fixture. These setup failures are not successful test evidence.

## Checks and results

- Before implementation, the new provider-discovery regression failed because
  no apache_airflow_provider entry point existed for adbc-driver-redis.
- Built a wheel with `uv build --wheel python`, installed that wheel into the
  isolated Airflow environment, and used the rebuilt native library explicitly.
- `python -m pytest -q python/tests/test_airflow_hook.py python/tests/test_airflow.py`
  with RUN_AIRFLOW_TESTS=1 through the verified Software wrapper:
  **12 passed, zero skips**. Covers installed entry-point/schema discovery,
  management and SDK connection dispatch, default connection ID, generic ADBC
  isolation, standard SQL operator bound-query execution and pre-connect
  transaction rejection, both new and legacy explicit-hook read/write paths,
  NULLs, empty results, option propagation and existing TaskFlow/Arrow smoke.
  Two deprecation warnings came from management-side ProvidersManager.hooks;
  task-runtime discovery was independently exercised through airflow.sdk.
- `airflow providers list --output json`: the installed wheel appears as
  adbc-driver-redis, version 0.1.0. CLI deprecation logs precede its JSON output;
  the provider array was checked separately from those log lines.
- Installed the base wheel into another environment with neither Airflow nor
  PyArrow. Loaded its entry point and verified metadata successfully.
- Simulated an absent optional Arrow provider after Airflow initialization:
  hook import raises AirflowOptionalProviderFeatureException with the airflow
  extra installation instruction; metadata remains loadable.
- `python python/scripts/check_integration_guide.py airflow --require-read-write`
  through the verified Software wrapper: **passed**. Executes the guide's hook
  read/write example and automatic SQL operator query; its two existing
  integration tests also passed. The TaskFlow DAG runs twice per smoke test.
- `git diff --check`: passed. Changed-document local file links checked.

## Review and limits

Reviewed provider metadata, dependency isolation, connection dispatch, option
propagation, autocommit enforcement and legacy explicit-hook use against
REVIEW.md. No actionable findings remain from this local review. This does not
represent human maintainer approval or a release.

No scheduler deployment, remote executor, browser UI, Airflow 2.x, Linux or live
CI validation was performed. The source-built wheel uses an explicit library
path; release-platform native bundling was not changed. Existing published
wheels do not gain this functionality until a release containing the change.
The temporary Software fixture was removed after verification, and the
original local test environment file was restored.
