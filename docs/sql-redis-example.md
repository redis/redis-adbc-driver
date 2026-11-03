# From SQL to Redis: INSERT and SELECT

Each SQL row becomes a Redis HASH. A Redis Search index finds rows matching
the SQL `WHERE` clause. The driver also maintains table metadata describing
columns, types, and the table's row prefix and index.

The commands below illustrate the stored layout and query execution; they
omit metadata reads, row ID allocation, and write bookkeeping. Run the SQL
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
row ID of `1`, the row is stored as:

```redis
HSET public:customers:1 __rowid 1 customer_id 101 name "Alice" country "USA"
```

The key suffix `1` is the driver's internal row ID; `101` is the application's
customer ID. Each column is a HASH field. SQL NULL is normally represented
by an absent field; the hidden `__rowid` field keeps even an all-NULL row alive.

Creating the table also creates the Search index `idx:public:customers`.
Redis Search indexes the row HASH automatically when it is written; the
driver does not issue a separate command to insert this row into the index.

## Select matching rows

```sql
SELECT customer_id, name
FROM customers
WHERE country = 'USA';
```

For these ordinary string values, the driver uses a TAG equality query on
the index. A simplified Redis command showing the relevant parts is:

```redis
FT.AGGREGATE idx:public:customers "@country:{USA}" LOAD 2 @customer_id @name WITHCURSOR COUNT 10000 DIALECT 2
```

The index finds matching rows, and `LOAD` returns the requested fields.
The driver reads the cursor results and converts the values into typed SQL
columns:

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
HMGET public:customers:1 customer_id name
```

A filter on `customer_id = 101` instead uses that column's index; it does
not imply that the Redis key ends in `101`.

See the [architecture guide](driver-reference.md#architecture-hybrid-index-row-layout)
for index definitions, metadata, and key prefixes after truncation or table
recreation. Existing [HASH collections adopted in place](driver-reference.md#adopt-hashes-in-place)
retain their application keys and do not support SQL INSERT.
