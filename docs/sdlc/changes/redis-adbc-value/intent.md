# Explain the purpose and value of Redis ADBC

Date: 2026-10-26. Requestor: Albert Wong. Status: proposed for review.
Source baseline: `00795dbde4bc81605a74b772d7b6904339d33727`.

The README introduces the implementation and then moves into detailed feature
matrices. Add a clear explanation of why the driver exists and the practical
value of connecting Redis HASH data to SQL/Arrow analytics workflows.

Describe in-place operational analysis, reuse of analytics integrations, and
batch data exchange. Preserve the existing SQL, storage, conversion, transaction,
and integration limits. Driver behavior and performance claims are outside this
change.

Also explain how the driver can back a separate Flight SQL service, enabling a
JDBC access path through the Flight SQL JDBC driver for tools such as DBeaver.
Keep the separate-server and validation requirements explicit.

See [specification](spec.md), [plan](plan.md), and [validation](validation.md).
Review: [PR for this branch](https://github.com/redis/redis-adbc-driver/pulls?q=is%3Apr+head%3Acodex%2Fexplain-redis-adbc-value).
