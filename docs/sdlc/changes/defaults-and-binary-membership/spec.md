# Spec

Date: 2026-11-20. Requestor: Albert Wong. Author: Codex. Base: `e11653eb`.

## Defaults

CREATE TABLE and ALTER TABLE ADD COLUMN use PostgreSQL's restricted `b_expr`:
symbolic arithmetic/comparison operators and IS [NOT] DISTINCT FROM are allowed;
unparenthesized boolean connectives, most IS predicates, IN, BETWEEN, LIKE,
quantified comparisons, OVERLAPS and AT TIME ZONE are not. Parentheses, function
arguments and CASE retain complete nested expressions. Following NOT NULL,
CHECK, UNIQUE and REFERENCES constraints, and column COLLATE clauses, remain
outside the stored expression.
ALTER COLUMN SET DEFAULT uses unrestricted `a_expr`, as PostgreSQL does.
Syntax errors precede type/signature checks and never publish a definition.
Primary source: [PostgreSQL 18 gram.y](https://github.com/postgres/postgres/blob/REL_18_STABLE/src/backend/parser/gram.y),
`ColConstraintElem`, `alter_column_default`, `b_expr` and `c_expr`.

On ALTER TYPE, retain a direct unknown literal's original input type with a
stored cast, then validate assignment to the new type without evaluating a
computed value. Remove the old assignment typmod: numeric scale/precision and
string length apply at use time, and PostgreSQL removes the previous assignment
before installing the new one. Preserve UUID and array element identity; BPCHAR
input must not introduce CHARACTER's default length of one. Subsequent ALTERs
retain that input expression. Definition-time input errors, deferred computed
errors, frozen local-time literals, SQL/Arrow omission versus explicit NULL/value,
dependencies and private publication continue through the existing paths.
No storage format change is required: the retained expression remains SQL text.

## Native binary-key membership

The standalone Python reproducer uses native RESP3 Redis commands only. It
varies initial scan versus post-index HSET notifications, filtered/unfiltered,
one/overlapping prefixes, and absent/pre-index/post-index TTL assignment (24
cases per server). Keys distinguish actual zero bytes, non-UTF8 bytes and literal
backslash escape text. Full index enumeration, singleton INKEYS and HASH
existence are independent controls. Queries must preserve DUMP and absolute
PEXPIRETIME. Deliberate expiry is a subsequent, separate phase.

The disposable fixture refuses existing `app:*` keys and its reserved index
name; it removes only its enumerated synthetic keys and drops its index without
DD. Never run it on a database with concurrent application writers. Management
verification of Software distribution and effective 4x QPF precedes execution.
The tool reports observations; it does not equate successful execution with
complete native NUL-key indexing. Native warnings/errors stop the run.

Partition regressions cover positive unfiltered NUL inclusion, native filtered
membership, initial scan/notifications, exact bytes, unknown fields and TTLs.
A native zero, warning, composed error or uncertain reply cannot become an
inferred match. No application index is populated or rebuilt by reads.

See [intent](intent.md), [plan](plan.md), and [validation](validation.md).
