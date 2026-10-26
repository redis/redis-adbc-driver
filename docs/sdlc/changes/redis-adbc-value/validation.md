# README value validation

Date: 2026-10-26. Status: checks passed; human review pending.
See [intent](intent.md), [specification](spec.md), and [plan](plan.md).

- Checked the section against docs/driver-reference.md, including HASH adoption,
  and docs/python-integrations.md plus docs/data-engineering-adbc-support.md.
- Verified the ADBC API background against the Apache Arrow documentation:
  https://arrow.apache.org/adbc/current/index.html.
- Verified the Flight SQL/JDBC explanation against the Apache Arrow JDBC driver
  documentation: https://arrow.apache.org/java/current/flight_sql_jdbc_driver.html.
  The README presents a separate-service integration path and claims no completed
  Redis Flight SQL/DBeaver end-to-end test.
- Read the canonical redis/redis-apache-arrow-flight README through the signed-in
  GitHub session. It identifies Redis ADBC as its SQL backend and documents JDBC
  clients. Verified the client-examples anchor and docs/client-compatibility.md
  link from that page; this change does not rerun the companion server's tests.
- Confirmed the contents entry matches the new heading and all linked local
  files and heading fragments exist.
- git diff --check passed; the README diff adds the section and contents entry.
- No runtime code or settings changed. No Redis tests or ClickBench runs were
  performed, and no new performance or compatibility result is claimed.
- Agent self-review checked factual limits and documentation consistency. Human
  review has not occurred; no approval or merge is implied.

Review: [PR for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fexplain-redis-adbc-value).
