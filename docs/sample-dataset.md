# Sample dataset and SQL walkthrough

Complete the [release-based quick start](../README.md#quick-start) first. Run
these commands from the repository root with the quick-start virtual environment
active. Loading the sample replaces the `public.customers` and `public.sales`
tables; use the disposable local Redis database.

## Load the sample dataset

```bash
python go/examples/load_sample.py
```

This creates two tables in the default `public` schema:

- `customers` (50 rows): created with `CREATE TABLE` / `INSERT`. `email` is
  declared `NOINDEX`, so it is stored in the row HASHes but not in the index.
- `sales` (10,000 rows, `--rows N` to change): bulk-ingested from an Arrow
  table with `order_id, customer_id, country, product, quantity,
  unit_price NUMERIC(10,2), discount, ordered_at TIMESTAMP WITH TIME ZONE,
  status, notes`. Every column except `notes` is indexed.

## Run SQL

`examples/sql.py` runs one statement passed as an argument, or starts an
interactive prompt (statements end with `;`) when given none:

```bash
python go/examples/sql.py "SELECT COUNT(*) FROM sales"
```

```bash
python go/examples/sql.py
```

Common queries to try:

```sql
-- Browse rows (insertion order unless ORDER BY is given)
SELECT * FROM sales ORDER BY order_id LIMIT 5;

-- Count rows: GROUPBY 0 REDUCE COUNT inside the index
SELECT COUNT(*) FROM sales;

-- Filter on a TAG and a NUMERIC range, sort and limit inside the index,
-- then fetch just those 5 rows from their HASHes
SELECT order_id, country, product, quantity, unit_price
FROM sales
WHERE country = 'JPN' AND quantity >= 18
ORDER BY unit_price DESC
LIMIT 5;

-- Aggregate per group, computed from SORTABLE fields without opening HASHes
SELECT country, COUNT(*) AS orders, SUM(quantity) AS units, AVG(quantity) AS avg_units
FROM sales
GROUP BY country
ORDER BY units DESC;

-- Aggregate over an expression (computed exactly by the driver)
SELECT product, SUM(quantity * unit_price) AS revenue
FROM sales
WHERE status = 'shipped'
GROUP BY product
ORDER BY revenue DESC;

-- Timestamp range
SELECT order_id, ordered_at, status
FROM sales
WHERE ordered_at >= TIMESTAMP '2025-12-31 00:00:00'
ORDER BY ordered_at
LIMIT 5;

-- NULL handling
SELECT COUNT(*) AS no_discount FROM sales WHERE discount IS NULL;
SELECT order_id, notes FROM sales WHERE notes IS NOT NULL ORDER BY order_id LIMIT 3;

-- Full-row lookup by row id: a direct HMGET, the index is not used
SELECT __rowid, order_id, country, notes FROM sales WHERE __rowid = 42;

-- CASE expressions, also usable in GROUP BY (by alias or position)
SELECT order_id, quantity,
       CASE WHEN quantity >= 15 THEN 'bulk' WHEN quantity >= 5 THEN 'standard' ELSE 'small' END AS size
FROM sales ORDER BY order_id LIMIT 5;
SELECT CASE WHEN discount IS NULL THEN 'full price' ELSE 'discounted' END AS pricing, COUNT(*) AS orders
FROM sales GROUP BY pricing ORDER BY pricing;

-- HAVING filters groups; here the GROUPBY/REDUCE itself still runs in the index
SELECT country, COUNT(*) AS orders, SUM(quantity) AS units
FROM sales GROUP BY country HAVING SUM(quantity) > 17500 ORDER BY units DESC;

-- CREATE TABLE AS SELECT: column names and types come from the query
CREATE TABLE country_revenue AS
SELECT country, COUNT(*) AS orders, SUM(quantity * unit_price) AS revenue
FROM sales WHERE status = 'shipped' GROUP BY country;
SELECT * FROM country_revenue ORDER BY revenue DESC LIMIT 3;

-- Subqueries. An uncorrelated one runs once; its result is pushed into the
-- outer query's index filter (a range here, a union query for IN)
SELECT COUNT(*) AS above_avg FROM sales WHERE unit_price > (SELECT AVG(unit_price) FROM sales);
SELECT name, country FROM customers
WHERE customer_id IN (SELECT customer_id FROM sales WHERE quantity = 20 AND status = 'returned')
ORDER BY name LIMIT 5;

-- Correlated subqueries run per outer row (memoised on the outer values)
SELECT c.name, (SELECT COUNT(*) FROM sales s WHERE s.customer_id = c.customer_id) AS orders
FROM customers c ORDER BY orders DESC, c.name LIMIT 3;

-- EXISTS / IN correlated only by equalities can run once instead, as a hash
-- semi-join (here the matching customer ids also filter sales in the index)
SELECT COUNT(*) FROM sales s
WHERE EXISTS (SELECT 1 FROM customers c WHERE c.customer_id = s.customer_id AND c.country = 'JPN');

-- CTEs and derived tables are computed once and held in memory
WITH per_customer AS (
  SELECT customer_id, SUM(quantity) AS units FROM sales GROUP BY customer_id
)
SELECT customer_id, units FROM per_customer
WHERE units > (SELECT AVG(units) FROM per_customer)
ORDER BY units DESC LIMIT 3;
SELECT bucket, COUNT(*) AS orders
FROM (SELECT CASE WHEN quantity >= 15 THEN 'bulk' ELSE 'regular' END AS bucket FROM sales) AS t
GROUP BY bucket ORDER BY bucket;

-- Joins. Each table's own filters run in its index; inner joins start from
-- the table with the fewest matching rows, then fetch only the matching rows
-- of the other (an index union on the join key)
SELECT c.name, COUNT(*) AS orders, SUM(s.quantity) AS units
FROM sales s JOIN customers c ON s.customer_id = c.customer_id
WHERE c.country = 'JPN' AND s.status = 'shipped'
GROUP BY c.name ORDER BY units DESC LIMIT 3;

-- LEFT JOIN anti-join: customers who never bought 20 gizmos in one order
SELECT c.name FROM customers c
LEFT JOIN sales s ON s.customer_id = c.customer_id AND s.product = 'gizmo' AND s.quantity = 20
WHERE s.order_id IS NULL ORDER BY c.name LIMIT 5;

-- Comma joins with the condition in WHERE are hash joins, not cross products
SELECT c.country, COUNT(DISTINCT c.customer_id) AS customers, SUM(s.quantity * s.unit_price) AS revenue
FROM customers c, sales s WHERE s.customer_id = c.customer_id
GROUP BY c.country ORDER BY revenue DESC LIMIT 3;

-- Views. A single-table view stays lazy: filters on the view are pushed into
-- the base table's index together with the view's own WHERE
CREATE VIEW shipped_sales AS
SELECT order_id, customer_id, country, product, quantity FROM sales WHERE status = 'shipped';
SELECT COUNT(*) FROM shipped_sales WHERE country = 'JPN' AND quantity >= 15;
DROP VIEW shipped_sales;

-- Temporary tables and views belong to this connection: other connections
-- don't see them, and they are dropped when it closes
CREATE TEMP TABLE big_jpn AS
SELECT order_id, customer_id, quantity FROM sales WHERE country = 'JPN' AND quantity >= 18;
SELECT c.name, COUNT(*) AS orders FROM big_jpn b JOIN customers c ON c.customer_id = b.customer_id
GROUP BY c.name ORDER BY orders DESC, c.name LIMIT 3;

-- Set operations: each branch runs in its own index; the driver combines them
SELECT country FROM customers WHERE customer_id <= 10
UNION
SELECT country FROM sales WHERE quantity = 20 AND product = 'gizmo'
ORDER BY 1;
SELECT customer_id FROM customers
EXCEPT
SELECT customer_id FROM sales WHERE product = 'gizmo' AND quantity = 20
ORDER BY 1 LIMIT 5;

-- Window functions run in the driver, after WHERE / GROUP BY / HAVING.
-- Each customer's latest order (the dbt de-duplication idiom), and the same
-- with QUALIFY: the earliest customer per country
SELECT customer_id, order_id, ordered_at FROM (
  SELECT customer_id, order_id, ordered_at,
         ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY ordered_at DESC) AS rn
  FROM sales
) AS latest WHERE rn = 1 ORDER BY customer_id LIMIT 5;
SELECT name, country, signup_date FROM customers
QUALIFY ROW_NUMBER() OVER (PARTITION BY country ORDER BY signup_date) = 1 ORDER BY country;

-- Ranks and running totals over grouped rows; LAG and a moving average
SELECT country, SUM(quantity) AS units, RANK() OVER (ORDER BY SUM(quantity) DESC) AS rnk,
       SUM(SUM(quantity)) OVER (ORDER BY SUM(quantity) DESC) AS running_units
FROM sales GROUP BY country ORDER BY rnk;
SELECT order_id, quantity, LAG(quantity) OVER w AS prev,
       AVG(quantity) OVER (w ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) AS moving_avg
FROM sales WHERE customer_id = 7 WINDOW w AS (ORDER BY order_id) ORDER BY order_id LIMIT 5;

-- Date/time functions (UTC): monthly totals for the first quarter
SELECT TO_CHAR(DATE_TRUNC('month', ordered_at), 'YYYY-MM') AS month, COUNT(*) AS orders, SUM(quantity) AS units
FROM sales WHERE EXTRACT(QUARTER FROM ordered_at) = 1
GROUP BY month ORDER BY month;

-- Timestamp arithmetic: the bound is computed once, then runs as an index range
SELECT COUNT(*) AS last_week FROM sales
WHERE ordered_at >= TIMESTAMP '2025-12-31 00:00:00' - INTERVAL '7 days';
SELECT MAX(ordered_at) - MIN(ordered_at) AS span, AGE(MAX(ordered_at), MIN(ordered_at)) AS age FROM sales;

-- Math, string and conditional functions. The index still answers the WHERE
-- clause; functions over columns run in the driver on the matching rows
SELECT INITCAP(product) AS product, ROUND(AVG(unit_price), 2) AS avg_price,
       ROUND(SUM(quantity * unit_price * (1 - COALESCE(discount, 0))), 2) AS net
FROM sales WHERE status = 'shipped' GROUP BY product ORDER BY net DESC;
SELECT LPAD(CAST(customer_id AS VARCHAR), 4, '0') AS id, SPLIT_PART(email, '@', 2) AS domain,
       SUBSTRING(name FROM 10) AS num, IIF(country IN ('USA', 'CAN', 'MEX'), 'americas', 'other') AS region
FROM customers WHERE customer_id <= 3 ORDER BY customer_id;

-- information_schema: which columns of sales are indexed?
SELECT column_name, data_type, is_nullable, is_indexed
FROM information_schema.columns WHERE table_name = 'sales' ORDER BY ordinal_position;

-- LIKE; a prefix pattern on an indexed column is an index prefix query
SELECT COUNT(*) FROM sales WHERE product LIKE 'gi%';

-- Regular expressions and SIMILAR TO run in the driver, on the
-- rows the rest of the WHERE clause finds in the index
SELECT name, REGEXP_SUBSTR(email, '([0-9]+)@', 1, 1, '', 1) AS num, SUBSTRING(email FROM '@(.*)$') AS domain
FROM customers WHERE email ~ '^customer[0-9]@' ORDER BY customer_id LIMIT 3;
SELECT product, COUNT(*) AS orders FROM sales
WHERE country = 'JPN' AND product SIMILAR TO 'g(adget|izmo)' GROUP BY product ORDER BY product;

-- IN lists, dates
SELECT name, country, signup_date
FROM customers
WHERE country IN ('USA', 'CAN')
ORDER BY name
LIMIT 5;

-- Writes
INSERT INTO customers (customer_id, name, country, signup_date)
VALUES (51, 'Ada', 'GBR', DATE '2026-09-30');
UPDATE sales SET status = 'returned' WHERE order_id = 1;
DELETE FROM customers WHERE customer_id = 51;

-- DDL
CREATE TABLE events (id BIGINT NOT NULL, kind VARCHAR, payload VARCHAR NOINDEX, at TIMESTAMP(3));
COMMENT ON COLUMN events.payload IS 'Raw JSON, as received';
DROP TABLE events;
```

## Inspect the data in Redis

Each row is a plain HASH, and each table has one RediSearch index. A renamed
table keeps its key names unless `adbc.redis.rename_rekey` is set (see
[ALTER TABLE](driver-reference.md#schema-objects-and-ddl)), and a table created again after `DROP TABLE`, or
emptied by `TRUNCATE`, gets new ones (`public:sales~2:`, see [Key prefixes](driver-reference.md#metadata-and-key-naming)); `information_schema.tables` shows each table's `key_prefix` and
`index_name`:

```bash
docker compose -f go/compose.yaml exec redis-software /opt/redislabs/bin/redis-cli -p 12000 HGETALL public:sales:42
```

```bash
docker compose -f go/compose.yaml exec redis-software /opt/redislabs/bin/redis-cli -p 12000 FT.INFO idx:public:sales
```

To see the `FT.AGGREGATE` / `FT.CURSOR` commands the driver sends for each query,
run `MONITOR` in a second terminal while you run SQL:

```bash
docker compose -f go/compose.yaml exec redis-software /opt/redislabs/bin/redis-cli -p 12000 MONITOR
```

Stop and remove the disposable Redis Software database with `make -C go redis-down`.
