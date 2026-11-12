# Specification: Redis ADBC provider

Date: 2026-11-12. Base: `b4783b64`. Status: implemented locally.
See [intent](intent.md), [plan](plan.md), and [validation](validation.md).

- The existing adbc-driver-redis distribution exposes an apache_airflow_provider
  entry point returning schema-valid metadata. Metadata loads without Airflow
  or PyArrow; query support requires the existing airflow extra.
- Register the unique redis_adbc connection type with display name Redis ADBC.
  Preserve generic adbc and native redis provider registrations.
- Airflow management and task-runtime connections discover RedisAdbcHook.
  Preserve adbc_conn_id and explicit imports; default to redis_adbc_default.
- Connection extras keep db_kwargs.uri and conn_kwargs. Default the dialect to
  redis. UI fields direct configuration to extras instead of unused host fields.
- Standard SQLExecuteQueryOperator resolves the hook from connection type and
  executes with autocommit=True. Its default False remains an explicit error
  before connecting. No transaction emulation or operator monkeypatching.
- Verify installed-wheel registration, discovery, bound queries, DDL/inserts,
  NULLs, legacy explicit-hook use, rejection paths and existing TaskFlow smoke.
  Live tests require Redis Software and verified effective 4x QPF.

Risks: services need the airflow extra; older installed wheels lack this entry
point. Existing generic adbc connections require migration for automatic Redis
selection. No new Airflow 2.x, scheduler, remote-executor or UI claim.
