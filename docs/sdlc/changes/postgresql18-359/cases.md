# PostgreSQL 18 retained case dispositions

Latest follow-up: 2026-11-10, [#98 JSON changes](../postgresql18-json/validation.md).
The original #359 measurement remains historical; the follow-up observes 145/188
retained passes and 43 retained failures, including five still owned by #361.


This inventory accounts for the 188 baseline failures in [#359](https://github.com/redis/redis-adbc-driver/issues/359). Latest follow-up: 145 observed passes and 43 deferred failures. All 188 cases are triaged; the full corpus still reports every deferred failure. A case classification is not a feature conformance result. Exclusions are from this implementation change only; the full corpus and #118 retain failures.

Core-related identifies a related mandatory feature family, not proof that the precise PostgreSQL syntax/error timing is mandatory. Optional and PostgreSQL-specific classifications describe the tested form. Exact subfeature mapping remains explicitly unverified where noted. The classifications use [PostgreSQL 18 Appendix D](https://www.postgresql.org/docs/18/features-sql-standard.html); this is not an ISO normative test audit.

## Root causes and scope

| Root cause | Classification | Evidence and disposition rationale | Owner |
|---|---|---|---|
| `aggregate-types` | Core-related | E091-01 AVG / E091-05 SUM are Core-related; two cases are not complete feature coverage. Use NUMERIC for integer/decimal averages and bigint sums; preserve real SUM type. Exact average output has an Arrow precision/scale contract rather than arbitrary PostgreSQL numeric scale. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `case-planner-folding` | Core-related | F261-02 searched CASE is Core-related; the exact constant-folding/error timing is PostgreSQL planner behavior. Needs planner constant folding with dead-branch pruning and volatility rules; indiscriminate eager evaluation breaks short-circuit semantics. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `compound-string-literal` | optional standard | F271 compound character literals is optional. Concatenate adjacent quoted fragments only across a newline while preserving source locations. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `date-bin-range` | PostgreSQL-specific | date_bin and its microsecond stride/result bounds are PostgreSQL-specific. Check interval-microsecond int64 range and timestamp lower bound using exact big integers. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `date-input-forms` | PostgreSQL-specific | English/compact/MDY and special date input are extensions; basic datetime types relate to Core F051, not certification. English month names, compact/ordinal/Julian inputs and meridiem/zone handling use the existing exact temporal parser. Date arithmetic/storage still has the broader Arrow date32 range; full input/constructor range consistency is not claimed. [Source](https://www.postgresql.org/docs/18/datatype-datetime.html) | [#93](https://github.com/redis/redis-adbc-driver/issues/93) |
| `endpoint-identity` | fixture/oracle | Endpoint identity/build/version is contextual; exact cross-server equality is not a mandatory feature test. Validate endpoint-specific identity/version contracts, without forging the PostgreSQL server identity. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `enum-catalog` | PostgreSQL-specific | CREATE TYPE AS ENUM is a PostgreSQL extension; not S011 distinct-type support. Requires enum catalog, ordering and persistent type identity; do not replace enums with unconstrained strings. [Source](https://www.postgresql.org/docs/18/datatype-enum.html) | [#107](https://github.com/redis/redis-adbc-driver/issues/107) |
| `extended-temporal-comparison` | ADBC representation/comparison | Python year limits are a comparison limitation; Core F051 datetime support is related but not certified. Compare proleptic Gregorian BC/large-year values using raw Arrow integer epochs without dropping timestamp digits. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#93](https://github.com/redis/redis-adbc-driver/issues/93) |
| `extract-result-type` | optional standard | Standard EXTRACT/numeric result is involved; precise optional subfeature mapping remains unverified. Separate numeric EXTRACT from DATE_PART overloads and propagate numeric types through CASE/arithmetic/windows without rounding through float. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `float-binary-send` | PostgreSQL-specific | float8send is a PostgreSQL binary-send function. Encode IEEE float8 bits in network byte order. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `float-nan-division` | PostgreSQL-specific | Exact NaN/zero arithmetic behavior is dialect-specific within Core-related numeric type families. Propagate float NaN before checking a zero divisor; ordinary zero division still errors. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `format-and-functions` | PostgreSQL-specific | Exact function names/overloads are dialect behavior; a standard analogue does not certify this PostgreSQL form. Add scalar helpers with NULL/error semantics; numeric pictures intentionally support only 9/0/D/G/./,/S/FM in C locale, not the complete PostgreSQL format language. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `input-error-record` | PostgreSQL-specific | pg_input_error_info is a PostgreSQL set-returning diagnostic function. Requires a typed four-column diagnostic record preserving message/detail/hint/SQLSTATE and type-resolution failures. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `input-validity` | PostgreSQL-specific | pg_input_is_valid is a PostgreSQL diagnostic helper, not a mandatory Core feature. Use existing exact type coercion for supported input types; unsupported logical numeric special values remain separate. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `interval-extension` | fixture/oracle | Unquoted INTERVAL n DAY is a driver extension rejected by PostgreSQL; not a mandatory missing feature. Keep existing extension support and PostgreSQL 42601 visible; resolve exact-case scope without broad allowlisting. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `joined-table-aliases` | optional standard | F407 CROSS JOIN / F404 range variable for common column names are optional; exact nested alias coverage remains unverified. Needs grouped FROM join trees, column aliases, USING-only alias visibility and qualifier hiding; flattening joins changes name resolution. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `json-dialect` | PostgreSQL-specific | jsonb operators and json_* functions are PostgreSQL-specific forms; SQL/JSON has separate optional features. Needs logical JSON column identity, JSON modification/existence functions and set-returning/whole-row support; plain varchar cannot stand in for JSON semantics. [Source](https://www.postgresql.org/docs/18/functions-json.html) | [#98](https://github.com/redis/redis-adbc-driver/issues/98) |
| `length-result-type` | Core-related | E021-04/05 character and octet length; this case also uses PostgreSQL escape syntax. Return PostgreSQL-compatible int32 length results. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `logical-text-metadata` | Core-related | F021-01 COLUMNS view; logical text identity in CTAS. Exact feature conformance unverified. Logical text/varchar identity is lost before information_schema; requires expression/CTAS catalog propagation. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `nested-bigint-sum` | Core-related | SUM relates to E091-05; window framing adds optional standard behavior. Propagate NUMERIC bigint SUM type through nested/group/window aggregation. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `numeric-special-values` | ADBC representation/comparison | NUMERIC NaN/infinity are PostgreSQL extensions; Arrow scaled integer decimals cannot represent them. Requires a lossless numeric representation and arithmetic/cast/error semantics; silently coercing to float/string is unacceptable. [Source](https://www.postgresql.org/docs/18/datatype-numeric.html) | [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `octet-and-character-functions` | Core-related | E021-05 OCTET_LENGTH is Core-related; CHR/ASCII in the same case are dialect forms. Length uses int32; Unicode CHR/ASCII helpers are tested separately. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `ordered-window-peers` | fixture/oracle | Only equal-rank groups are unordered; window functions involve optional standard features. Comparator must preserve order between distinct sort keys and compare duplicate-preserving multisets within equal-key groups only. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `overlaps` | optional standard | F053 OVERLAPS is optional in the PostgreSQL 18 feature inventory. Requires row-pair temporal interval normalization, equal-endpoint and NULL semantics; generic scalar row comparison is insufficient. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `public-privileges` | Core-related | E081 basic privileges and F031 GRANT/REVOKE are Core-related; full feature coverage unverified. PUBLIC is not a provisioned Redis role. Requires a security model for existing/future identities and explicit grant/revoke interactions. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#363](https://github.com/redis/redis-adbc-driver/issues/363) |
| `recursive-consumer-limit` | optional standard | T131 recursive query is optional; PostgreSQL demand-driven execution is also observable. Eager CTE materialization needs safe demand propagation; global truncation would corrupt sorted/filtered/shared consumers. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `statistical-aggregates` | optional standard | Statistical aggregate family; precise optional standard subfeature mapping remains unverified. Paired-NULL regression/covariance accumulators and bit aggregates, including window frames and nonfinite inputs. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `symmetric-between` | optional standard | T461 symmetric BETWEEN is optional. Parse both boundary orders with three-valued predicates. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `temporal-infinity` | ADBC representation/comparison | PostgreSQL special temporal values have no ordinary Arrow timestamp/date infinity encoding. Requires a lossless logical/Arrow representation. Finite integer extrema cannot be reinterpreted as infinity. [Source](https://www.postgresql.org/docs/18/datatype-datetime.html) | [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `temporal-lower-bound` | PostgreSQL-specific | Exact PostgreSQL temporal minimum/range checks are dialect limits within Core-related datetime types. Check parsed timestamp UTC epoch and date_bin result lower bounds; keep the broader Arrow storage/constructor contract explicit. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#93](https://github.com/redis/redis-adbc-driver/issues/93) |
| `temporal-upper-range` | ADBC representation/comparison | PostgreSQL microsecond epoch range exceeds Arrow int64 microseconds since the Unix epoch. Requires a lossless Arrow/logical epoch representation; a coarser timestamp would lose microseconds. [Source](https://www.postgresql.org/docs/18/datatype-datetime.html) | [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `typed-integer-literals` | PostgreSQL-specific | int2/int4 typed literal spellings are PostgreSQL-specific; arithmetic/comparison relate to Core E011, not full conformance. Parse typed literals through checked existing coercion, preserving arithmetic overflow and exact integer comparisons. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `unicode-literal-errors` | PostgreSQL-specific | Exact invalid-escape SQLSTATE behavior is PostgreSQL-specific; Unicode literals relate to optional F393 and errors to Core E171. Distinguish malformed Unicode escapes, invalid scalar values and embedded NUL without weakening valid UTF-8 parsing. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `unique-index` | Core-related | UNIQUE enforcement relates to E141-01; CREATE UNIQUE INDEX syntax is PostgreSQL-specific. Uniqueness is a relational-integrity/storage publication decision; belongs with #114 and #360, not a parser-only change. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#360](https://github.com/redis/redis-adbc-driver/issues/360) |
| `uuid-storage-comparison` | ADBC representation/comparison | UUID is PostgreSQL-specific here; Arrow UTF-8 is the driver storage representation. Compare canonical UUID values using the existing UTF-8 storage mapping without hiding changed values. [Source](https://www.postgresql.org/docs/18/datatype-uuid.html) | [#118](https://github.com/redis/redis-adbc-driver/issues/118) |
| `whole-row-reference` | PostgreSQL-specific | Bare relation whole-row values and PostgreSQL composite output are dialect forms, related to optional row constructors. Requires bind-time whole-row expansion with logical field names/NULL semantics and independent composite Arrow comparison. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `width-bucket` | optional standard | WIDTH_BUCKET is a standard numeric function; precise optional subfeature mapping remains unverified. Exact finite numeric buckets and float infinite-operand handling; invalid/overflow counts remain errors. [Source](https://www.postgresql.org/docs/18/features-sql-standard.html) | [#110](https://github.com/redis/redis-adbc-driver/issues/110) |

## Stable cases

| File and case ID | Root cause | Latest observed result |
|---|---|---|
| `dbt/utils.json:event-0207-0` | `logical-text-metadata` | fail — deferred to [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `issue-240.json:repro-1` | `length-result-type` | pass |
| `issue-246.json:row-1` | `endpoint-identity` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `issue-246.json:row-2` | `endpoint-identity` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `issue-246.json:row-6` | `endpoint-identity` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `issue-246.json:row-9` | `endpoint-identity` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `issue-246.json:row-10` | `endpoint-identity` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `issue-249.json:row-1` | `date-input-forms` | pass |
| `issue-249.json:row-2` | `date-input-forms` | pass |
| `issue-249.json:row-3` | `date-input-forms` | pass |
| `issue-249.json:row-4` | `date-input-forms` | pass |
| `issue-249.json:row-5` | `date-input-forms` | pass |
| `issue-249.json:row-6` | `temporal-infinity` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `issue-249.json:row-7` | `date-input-forms` | pass |
| `issue-249.json:row-8` | `date-input-forms` | pass |
| `issue-249.json:row-9` | `temporal-infinity` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `issue-254.json:row-1` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-3` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-4` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-5` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-6` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-7` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-8` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-9` | `json-dialect` | pass — observed in [JSON follow-up](../postgresql18-json/validation.md) |
| `issue-254.json:row-10` | `json-dialect` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `issue-258.json:row-2` | `uuid-storage-comparison` | pass |
| `issue-260.json:row-3` | `unique-index` | fail — deferred to [#360](https://github.com/redis/redis-adbc-driver/issues/360) |
| `issue-263.json:row-1` | `enum-catalog` | fail — deferred to [#107](https://github.com/redis/redis-adbc-driver/issues/107) |
| `issue-263.json:row-2` | `enum-catalog` | fail — deferred to [#107](https://github.com/redis/redis-adbc-driver/issues/107) |
| `issue-264.json:row-1` | `public-privileges` | fail — deferred to [#363](https://github.com/redis/redis-adbc-driver/issues/363) |
| `issue-264.json:row-2` | `public-privileges` | fail — deferred to [#363](https://github.com/redis/redis-adbc-driver/issues/363) |
| `issue-266.json:row-1` | `width-bucket` | pass |
| `issue-266.json:row-2` | `statistical-aggregates` | pass |
| `issue-266.json:row-3` | `statistical-aggregates` | pass |
| `issue-266.json:row-4` | `statistical-aggregates` | pass |
| `issue-266.json:row-5` | `statistical-aggregates` | pass |
| `issue-266.json:row-6` | `format-and-functions` | pass |
| `issue-266.json:row-7` | `format-and-functions` | pass |
| `issue-266.json:row-8` | `format-and-functions` | pass |
| `issue-266.json:row-9` | `format-and-functions` | pass |
| `issue-266.json:row-10` | `format-and-functions` | pass |
| `issue-266.json:row-11` | `format-and-functions` | pass |
| `issue-266.json:row-12` | `format-and-functions` | pass |
| `issue-266.json:row-13` | `format-and-functions` | pass |
| `issue-266.json:row-14` | `format-and-functions` | pass |
| `issue-266.json:row-15` | `format-and-functions` | pass |
| `issue-266.json:row-16` | `format-and-functions` | pass |
| `issue-266.json:row-17` | `format-and-functions` | pass |
| `issue-266.json:row-18` | `format-and-functions` | pass |
| `issue-266.json:row-19` | `overlaps` | fail — deferred to [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `issue-266.json:row-20` | `symmetric-between` | pass |
| `issue-266.json:row-21` | `octet-and-character-functions` | pass |
| `issue-266.json:row-22` | `format-and-functions` | pass |
| `issue-266.json:row-23` | `format-and-functions` | pass |
| `issue-266.json:row-24` | `format-and-functions` | pass |
| `issue-266.json:row-25` | `format-and-functions` | pass |
| `issue-266.json:row-26` | `format-and-functions` | pass |
| `issue-268.json:repro-4` | `interval-extension` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `postgres/aggregates.json:statement-0005` | `aggregate-types` | pass |
| `postgres/aggregates.json:statement-0014` | `aggregate-types` | pass |
| `postgres/case.json:statement-0020` | `case-planner-folding` | fail — deferred to [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `postgres/date.json:statement-0023` | `date-input-forms` | pass |
| `postgres/date.json:statement-0026` | `date-input-forms` | pass |
| `postgres/date.json:statement-0027` | `date-input-forms` | pass |
| `postgres/date.json:statement-0028` | `date-input-forms` | pass |
| `postgres/date.json:statement-0029` | `date-input-forms` | pass |
| `postgres/date.json:statement-0030` | `date-input-forms` | pass |
| `postgres/date.json:statement-0031` | `date-input-forms` | pass |
| `postgres/date.json:statement-0032` | `date-input-forms` | pass |
| `postgres/date.json:statement-0033` | `date-input-forms` | pass |
| `postgres/date.json:statement-0034` | `date-input-forms` | pass |
| `postgres/date.json:statement-0035` | `date-input-forms` | pass |
| `postgres/date.json:statement-0036` | `date-input-forms` | pass |
| `postgres/date.json:statement-0037` | `date-input-forms` | pass |
| `postgres/date.json:statement-0038` | `date-input-forms` | pass |
| `postgres/date.json:statement-0039` | `date-input-forms` | pass |
| `postgres/date.json:statement-0040` | `date-input-forms` | pass |
| `postgres/date.json:statement-0044` | `date-input-forms` | pass |
| `postgres/date.json:statement-0045` | `date-input-forms` | pass |
| `postgres/date.json:statement-0046` | `date-input-forms` | pass |
| `postgres/float8.json:statement-0010` | `float-binary-send` | pass |
| `postgres/float8.json:statement-0019` | `input-validity` | pass |
| `postgres/float8.json:statement-0020` | `input-validity` | pass |
| `postgres/float8.json:statement-0021` | `input-validity` | pass |
| `postgres/float8.json:statement-0022` | `input-error-record` | fail — deferred to [#110](https://github.com/redis/redis-adbc-driver/issues/110) |
| `postgres/float8.json:statement-0035` | `float-nan-division` | pass |
| `postgres/float8.json:statement-0036` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/int4.json:statement-0009` | `input-validity` | pass |
| `postgres/int4.json:statement-0010` | `input-validity` | pass |
| `postgres/int4.json:statement-0011` | `input-validity` | pass |
| `postgres/int4.json:statement-0013` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0014` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0015` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0016` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0017` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0018` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0019` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0020` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0021` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0022` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0023` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0024` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0025` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0026` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0027` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0028` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0029` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0030` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0031` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0032` | `typed-integer-literals` | pass |
| `postgres/int4.json:statement-0033` | `typed-integer-literals` | pass |
| `postgres/int8.json:statement-0008` | `input-validity` | pass |
| `postgres/int8.json:statement-0009` | `input-validity` | pass |
| `postgres/int8.json:statement-0010` | `input-validity` | pass |
| `postgres/interval.json:statement-0023` | `input-validity` | pass |
| `postgres/interval.json:statement-0024` | `input-validity` | pass |
| `postgres/interval.json:statement-0025` | `input-validity` | pass |
| `postgres/interval.json:statement-0069` | `format-and-functions` | pass |
| `postgres/join.json:statement-0034` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0035` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0042` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0043` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0044` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0045` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0046` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0047` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0048` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0049` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/join.json:statement-0050` | `joined-table-aliases` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/numeric.json:statement-0526` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0527` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0528` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0530` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0531` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0532` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0534` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0535` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0536` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0537` | `format-and-functions` | pass |
| `postgres/numeric.json:statement-0543` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0547` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0551` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0553` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0554` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/numeric.json:statement-0555` | `numeric-special-values` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/select.json:statement-0019` | `whole-row-reference` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/select.json:statement-0020` | `whole-row-reference` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/select.json:statement-0021` | `whole-row-reference` | pass — observed in the [#361 follow-up](../postgresql18-361/validation.md) |
| `postgres/strings.json:statement-0000` | `compound-string-literal` | pass |
| `postgres/strings.json:statement-0001` | `compound-string-literal` | pass |
| `postgres/strings.json:statement-0017` | `unicode-literal-errors` | pass |
| `postgres/strings.json:statement-0020` | `unicode-literal-errors` | pass |
| `postgres/strings.json:statement-0021` | `unicode-literal-errors` | pass |
| `postgres/strings.json:statement-0027` | `unicode-literal-errors` | pass |
| `postgres/timestamp.json:statement-0026` | `temporal-infinity` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/timestamp.json:statement-0062` | `input-validity` | pass |
| `postgres/timestamp.json:statement-0063` | `input-validity` | pass |
| `postgres/timestamp.json:statement-0101` | `extended-temporal-comparison` | pass |
| `postgres/timestamp.json:statement-0102` | `temporal-lower-bound` | pass |
| `postgres/timestamp.json:statement-0103` | `temporal-upper-range` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/timestamp.json:statement-0125` | `date-bin-range` | pass |
| `postgres/timestamp.json:statement-0126` | `date-bin-range` | pass |
| `postgres/timestamp.json:statement-0133` | `temporal-upper-range` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/timestamp.json:statement-0136` | `temporal-upper-range` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/timestamptz.json:statement-0028` | `temporal-infinity` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/timestamptz.json:statement-0062` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0064` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0067` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0068` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0069` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0070` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0072` | `input-validity` | pass |
| `postgres/timestamptz.json:statement-0073` | `input-validity` | pass |
| `postgres/timestamptz.json:statement-0110` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0111` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0112` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0113` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0114` | `date-input-forms` | pass |
| `postgres/timestamptz.json:statement-0116` | `extended-temporal-comparison` | pass |
| `postgres/timestamptz.json:statement-0117` | `extended-temporal-comparison` | pass |
| `postgres/timestamptz.json:statement-0118` | `extended-temporal-comparison` | pass |
| `postgres/timestamptz.json:statement-0119` | `temporal-lower-bound` | pass |
| `postgres/timestamptz.json:statement-0120` | `temporal-upper-range` | fail — deferred to [#362](https://github.com/redis/redis-adbc-driver/issues/362) |
| `postgres/window.json:statement-0006` | `ordered-window-peers` | fail — deferred to [#364](https://github.com/redis/redis-adbc-driver/issues/364) |
| `postgres/window.json:statement-0036` | `nested-bigint-sum` | pass |
| `postgres/window.json:statement-0041` | `extract-result-type` | fail — deferred to [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `postgres/with.json:statement-0010` | `recursive-consumer-limit` | fail — deferred to [#361](https://github.com/redis/redis-adbc-driver/issues/361) |
| `postgres/with.json:statement-0011` | `recursive-consumer-limit` | fail — deferred to [#361](https://github.com/redis/redis-adbc-driver/issues/361) |

See [validation](validation.md) and [machine-readable inventory](../../../../go/pgdiff/issue359-cases.json). The strict completion check requires observed passes; `--triage` checks documented coverage and explicitly preserves failures.
