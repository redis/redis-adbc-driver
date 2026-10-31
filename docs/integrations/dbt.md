# Redis integration guide for dbt Core

Redis must run separately with Search (the Query Engine). Redis Software with
active verified 4x QPF is the development default. See
[driver installation](../python-integrations.md#install-and-discover-the-driver)
for supported platforms and source builds. This guide uses the
[Redis dbt Core v1 adapter](https://github.com/redis/redis-dbt-adapter/tree/main/v1),
not the experimental custom dbt v2 engine.

## Install and connect

Use a separate Python 3.14 environment. Install the adapter from its public
source repository and install the Python Redis driver in the same environment:

```sh
python3.14 -m venv .venv-dbt
. .venv-dbt/bin/activate
git clone https://github.com/redis/redis-dbt-adapter.git
python -m pip install ./redis-dbt-adapter/v1/adapter 'adbc-driver-redis[dbapi]'
export REDIS_URI=redis://localhost:6379/0
export REDIS_ADBC_DRIVER="$(python -c 'from adbc_driver_redis import _driver_path; print(_driver_path())')"
mkdir -p redis-guide/models redis-guide/seeds redis-guide/tests
cd redis-guide
```

Use `rediss://` for verified TLS. The adapter's profile takes the native library
path through `REDIS_ADBC_DRIVER`; the Python package's
`REDIS_ADBC_LIBRARY` source-build override is a different variable.
Use a database dedicated to these examples.

## Create the starter project

Save `dbt_project.yml`:

```yaml
name: redis_guide
version: "1.0.0"
config-version: 2
profile: redis_guide
model-paths: [models]
seed-paths: [seeds]
test-paths: [tests]
models:
  redis_guide:
    +materialized: table
```

Save `profiles.yml` alongside it:

```yaml
redis_guide:
  target: dev
  outputs:
    dev:
      type: redis_adbc
      driver: "{{ env_var('REDIS_ADBC_DRIVER') }}"
      uri: "{{ env_var('REDIS_URI') }}"
      database: redis
      schema: dbt_guide
      threads: 1
```

Save `seeds/orders.csv`:

```csv
id,amount
1,10
2,20
3,12
```

Save `models/revenue.sql`:

```sql
select count(*) as order_count, sum(amount) as total
from {{ ref('orders') }}
```

Save `tests/revenue_is_42.sql`:

```sql
select * from {{ ref('revenue') }}
where order_count <> 3 or total <> 42
```

## Build, test and inspect

Run from `redis-guide`:

```sh
dbt debug --profiles-dir .
dbt seed --profiles-dir .
dbt build --profiles-dir .
dbt docs generate --profiles-dir .
dbt show --profiles-dir . --inline "select * from {{ ref('revenue') }}" --output json
```

The seed loads three orders. The model persists one row with `order_count=3`
and `total=42`; the singular test fails if those values differ. `dbt build`
also loads the seed, so the separate seed command demonstrates that operation.
View generated catalog/manifest files in `target/`.

These commands create objects in `dbt_guide`. After inspecting the results,
remove only the example schema in the dedicated database:

```python
import os
from adbc_driver_redis import dbapi

with dbapi.connect(os.environ["REDIS_URI"]) as conn, conn.cursor() as cur:
    cur.execute("DROP SCHEMA IF EXISTS dbt_guide CASCADE")
```

## Validation and limits

The [live guide validation](../sdlc/changes/upstream-status-249/validation.md)
records the five commands above passing with dbt Core 1.12.6 and the Redis v1
adapter on Redis Software 8.6.2 with Search and active verified 4x QPF.
The persisted result was independently read through ADBC and checked as `(3,42)`.
This small example does not validate every adapter feature or the larger
[NYC taxi project](https://github.com/redis/redis-dbt-adapter/blob/main/v1/README.md).

Writes autocommit. A failed dbt run can leave partial writes or intermediate
objects; rollback does not restore them. Metadata, SQL coverage and adapter
versions matter independently of ADBC connectivity. Hosted dbt, TLS, clusters,
snapshots, incremental/microbatch models and the experimental v2 engine were
not covered by this guide run.

[All integration guides](README.md)
