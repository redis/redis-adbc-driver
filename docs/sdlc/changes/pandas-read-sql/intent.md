# pandas read_sql coverage

Date: 2026-10-29. Source: `95d6583`. Status: implemented and locally verified.
Requestor: repository user ("test pandas `read_sql`").

Add repeatable integration coverage for pandas consumers of the Redis ADBC
connection and SQLAlchemy dialect. No driver behavior change is intended.
See [specification](spec.md), [plan](plan.md), and [validation](validation.md).
PR preparation: applied the tests to destination `redis/redis-adbc-driver` main
at `5c1641f` after discovering that the original checkout had unrelated history.
