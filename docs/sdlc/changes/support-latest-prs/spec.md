# Support refresh specification

Date: 2026-11-02. Requestor: Albert Wong. Base: `aa33d73` (merged #282).

Reflect supported SQLAlchemy/DBI acceptance, patched R dependencies, the prior Fivetran SDK tester result and installation/release automation. Correct implemented Fivetran and Superset paths to qualifying ADBC rows under the existing inventory criteria: 41/50 tools, 37 ADBC, 25 overlapping Flight SQL paths. Retain local-only deployment and browser UI limits. Historical suite counts remain historical, and skipped cases are not passes. No runtime changes, new tests or benchmark reruns.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
