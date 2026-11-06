# From SQL to Redis: INSERT and SELECT

Each SQL row becomes a Redis HASH. A Redis Search index finds rows matching
the SQL `WHERE` clause. The driver also maintains table metadata describing
columns, types, and the table's row prefix and index.

The commands below illustrate the stored layout and query execution; they
omit metadata reads, row ID allocation, writer fences and publication bookkeeping.
Managed tables use [always-on statement snapshots](snapshots.md). Run the SQL
through the driver. The Redis snippets explain what happens underneath;
they are not a complete script for creating a driver-managed table manually.

## Insert a row

Create a table and insert a row through the driver:

```sql
CREATE TABLE customers (
    customer_id BIGINT,
    name TEXT,
    country TEXT
);

INSERT INTO customers (customer_id, name, country)
VALUES (101, 'Alice', 'USA');
```

Assuming the `public` schema, a new table name, and an allocated internal
row ID of `1`, an illustrative private row image is:

```redis
HSET public:{meta}:snapshot:rows:<generation>:1 __rowid 1 customer_id 101 name "Alice" country "USA"
```

The key suffix `1` is the driver's internal row ID; `101` is the application's
customer ID. Each column is a HASH field. SQL NULL is normally represented
by an absent field; the hidden `__rowid` field keeps even an all-NULL row alive.

`<generation>` is a private storage identity, not a literal key component. The
driver registers and writes bounded row batches behind server-side fences,
builds a final Search index over the immutable output, then publishes the new
catalog generation atomically. Failed writes do not expose partial rows.
Unchanged rows and indexes remain shared across generations.

Inspect the current physical names through SQL rather than constructing them:

```sql
SELECT key_prefix, index_name
FROM information_schema.tables
WHERE table_schema = 'public' AND table_name = 'customers';
```

The index name has the form `adbc_snapshot_{meta}_<generation>_<seal>`.
Earlier streamed queries retain their original generation after a later write.

## Select matching rows

```sql
SELECT customer_id, name
FROM customers
WHERE country = 'USA';
```

For these ordinary string values, the driver uses a TAG equality query on
the index. An illustrative candidate lookup is:

```redis
FT.SEARCH adbc_snapshot_{meta}_<generation>_<seal> "@country:{USA}" NOCONTENT DIALECT 2
```

The exact Search command depends on the query plan. The driver resolves newer
versions and deletion markers before SQL predicates, aggregates or limits can
use an older candidate. Authoritative HASH reads preserve exact values and NULLs;
safe optimized plans can also use the immutable indexes. The typed result is:

```text
customer_id | name
------------+------
101         | Alice
```

## Look up a row directly

A lookup by the internal row ID can skip the Search index:

```sql
SELECT customer_id, name
FROM customers
WHERE __rowid = 1;
```

The corresponding field lookup is:

```redis
HMGET public:{meta}:snapshot:rows:<visible-generation>:1 customer_id name
```

The visible generation is resolved from the statement's pinned snapshot. A
replacement or deletion may hide an older HASH with the same internal row ID.
A filter on `customer_id = 101` instead uses that column's index; it does
not imply that the Redis key ends in `101`.

See the [architecture guide](driver-reference.md#architecture-hybrid-index-row-layout)
for index definitions, metadata, and key prefixes after truncation or table
recreation. Existing application HASH collections are imported into private
managed copies; explicit refresh observes later application changes. Imported
rows retain their original keys as `__key` provenance. SQL-added rows have NULL
provenance, and writes never change the application's source HASHes.
