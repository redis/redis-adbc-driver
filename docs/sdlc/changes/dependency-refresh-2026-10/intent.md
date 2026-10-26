# Refresh libraries and Python environments

Date: 2026-10-22. Requester: Albert Wong. Author: Codex.
Source: `e196ca33146d113f5b75cc7f4b25fe815fcf129b`.
Status: implemented for review; maintainer review pending.
Follow-up to merged PR [#439](https://github.com/alberttwong/redis-adbc-driver/pull/439).
See [specification](spec.md) and [plan and evidence](plan.md).

Move declared library requirements to current stable releases, including major
upgrades, to reduce vulnerability exposure. Use Python 3.14 for builds and main
integration tests. The requester approved a separate Superset compatibility extra
because the latest Superset cannot use the latest SQLAlchemy.

Preserve existing SQL, Arrow ownership, TLS, storage and external HASH contracts.
Do not publish a release or change live deployments. Keep historical benchmark
and deployment evidence unchanged. Runtime/server images and coupled DuckDB
engine/scanner source pins are outside this library dependency refresh.

Superset's compatibility stack requires Python 3.12 and older transitive libraries.
Keep Python >=3.12 installability for that environment, while CI/build defaults
and the validation/pgdiff harnesses use Python 3.14. Python 3.10 is end of life
([official schedule](https://peps.python.org/pep-0619/)). The Go runtime remains
at the previously security-patched requirement of 1.26.10.
