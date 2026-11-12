# Intent: register the Redis ADBC Airflow provider

Date: 2026-11-12. Base: `b4783b64`. Requestor: repository user.
Status: implemented and validated locally; pull request requested; not released.

The user requested a discoverable Airflow provider instead of an explicitly
imported hook. Keep the existing Python distribution and optional Airflow extra;
no independent repository or release lifecycle is needed for this change.

Success means standard SQL operators automatically choose RedisAdbcHook for a
Redis ADBC connection. Preserve native library discovery, autocommit-only
behavior and existing explicit imports. Exclude scheduler deployment, native
storage changes, an Apache-maintained provider claim, and publication.

See [spec](spec.md), [plan](plan.md), and [validation](validation.md).
