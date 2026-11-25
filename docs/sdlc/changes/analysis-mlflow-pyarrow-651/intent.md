# Intent

Date: 2026-11-24. Requestor: Albert Wong ([issue #651](https://github.com/redis/redis-adbc-driver/issues/651)).
Source: `ea16062c`. Status: implemented and locally validated; hosted CI and human review pending.

MLflow 3.18.0 fixed the automatic dataset digest for empty Arrow-backed pandas
string columns ([mlflow#27359](https://github.com/mlflow/mlflow/pull/27359)) and
allows PyArrow 26. The analysis candidate profile already required MLflow 3.18.0
but kept a strict expected failure written for 3.17.2 and a PyArrow `<26` cap.
Under the pinned versions the expected failure can only report XPASS(strict).

Affected consumers: maintainers running the `analysis` candidate profile, and
readers of the MLflow and Ibis integration guides.

Desired behavior: the empty default-digest case is an ordinary passing test, the
profile installs PyArrow 26, and the documentation describes current versions.
`CANDIDATE_PROFILES=analysis` passes on a verified Redis Software 4x QPF fixture.

Exclusions: Beam keeps its PyArrow cap until a release contains
[apache/beam#40927](https://github.com/apache/beam/pull/40927). No driver, SQL or
Arrow conversion behavior changes. Historical validation records stay unchanged.

See [spec](spec.md), [plan](plan.md) and [validation](validation.md).
