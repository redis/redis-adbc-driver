// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

// Integration tests for the errors' SQLSTATEs and ADBC statuses
// (sqlstate.go). Each SQLSTATE is the one Postgres 16 reports for the same
// statement.

import (
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// stmtErr runs a statement, reading its result to the end, and returns
// its error.
func (h *sqlHarness) stmtErr(sql string, bind arrow.RecordBatch) error {
	h.t.Helper()
	st, err := h.conn.NewStatement(h.ctx)
	if err != nil {
		h.t.Fatal(err)
	}
	defer st.Close(h.ctx)
	if err := st.SetSqlQuery(h.ctx, sql); err != nil {
		h.t.Fatal(err)
	}
	if bind != nil {
		if err := st.Bind(h.ctx, bind); err != nil {
			h.t.Fatal(err)
		}
	}
	rdr, _, err := st.ExecuteQuery(h.ctx)
	if err != nil {
		return err
	}
	defer rdr.Release()
	for rdr.Next() {
	}
	return rdr.Err()
}

// checkState checks that err is an adbc.Error with status code, SQLSTATE
// state and a message containing msg.
func checkState(t *testing.T, what string, err error, code adbc.Status, state, msg string) {
	t.Helper()
	var ae adbc.Error
	switch {
	case err == nil:
		t.Errorf("%s: no error, want %v (%s)", what, code, state)
	case !errors.As(err, &ae):
		t.Errorf("%s: %T %v, want an adbc.Error", what, err, err)
	case ae.Code != code || string(ae.SqlState[:]) != state || !strings.Contains(ae.Msg, msg):
		t.Errorf("%s:\n got: %v (%q)\nwant: %v (%s) containing %q", what, ae.Code, ae.SqlState[:], code, state, msg)
	}
}

func TestSQLErrorStates(t *testing.T) {
	h := newSQLHarness(t)
	h.dropViews("it_err_atv")
	h.dropTables("it_err", "it_err2", "it_err3", "it_err_pk", "it_err_at", "it_err_num")
	h.exec(`CREATE TABLE it_err (id INTEGER NOT NULL, n INTEGER, v VARCHAR(3), d DATE, b BOOLEAN, c INTEGER CHECK (c > 0))`)
	h.exec(`INSERT INTO it_err (id, n) VALUES (1, 10)`)
	h.exec(`CREATE TABLE it_err2 (x INTEGER)`)
	h.exec(`INSERT INTO it_err2 VALUES (-1)`)
	h.exec(`CREATE TABLE it_err_pk (k INTEGER PRIMARY KEY)`)
	h.exec(`CREATE TABLE it_err_num (m NUMERIC(5, 2), r REAL, f DOUBLE PRECISION, t TIME, ts TIMESTAMP WITH TIME ZONE)`)
	h.exec(`CREATE TABLE it_err_at (a INTEGER, b BOOLEAN DEFAULT true)`)
	h.exec(`CREATE VIEW it_err_atv AS SELECT a FROM it_err_at`)

	const (
		data   = adbc.StatusInvalidData
		arg    = adbc.StatusInvalidArgument
		integ  = adbc.StatusIntegrity
		absent = adbc.StatusNotFound
		exists = adbc.StatusAlreadyExists
		notImp = adbc.StatusNotImplemented
	)
	for _, c := range []struct {
		sql   string
		code  adbc.Status
		state string
		msg   string // a part of the message
	}{
		// Text that doesn't convert: invalid_text_representation.
		{`SELECT CAST('abc' AS INTEGER)`, data, "22P02", `"abc"`},
		{`SELECT CAST('1.5' AS INTEGER)`, data, "22P02", `invalid input syntax for type integer: "1.5"`},
		{`SELECT id FROM it_err WHERE n = '1e3'`, data, "22P02", `invalid input syntax for type integer: "1e3"`},
		{`SELECT 'abc'::boolean`, data, "22P02", `invalid input syntax for type boolean: "abc"`},
		{`SELECT TRUE = 'maybe'`, data, "22P02", `invalid input syntax for type boolean: "maybe"`},
		{`SELECT CAST('abc' AS DOUBLE PRECISION)`, data, "22P02", `invalid input syntax for type double precision: "abc"`},
		{`SELECT CAST('1_000' AS DOUBLE PRECISION)`, data, "22P02", `invalid input syntax for type double precision: "1_000"`},
		{`SELECT CAST('abc' AS REAL)`, data, "22P02", `invalid input syntax for type real: "abc"`},
		{`SELECT CAST('abc' AS NUMERIC(10, 2))`, data, "22P02", `invalid input syntax for type numeric: "abc"`},
		{`SELECT 'abc'::numeric`, data, "22P02", `invalid input syntax for type numeric: "abc"`},
		{`SELECT 1.5 = 'x'`, data, "22P02", `invalid input syntax for type numeric: "x"`},
		{`SELECT CAST('{' AS JSON)`, data, "22P02", "invalid input syntax for type json"},
		// numeric_value_out_of_range.
		{`SELECT CAST(3000000000 AS INTEGER)`, data, "22003", "integer out of range"},
		{`SELECT CAST('3000000000' AS INTEGER)`, data, "22003", `value "3000000000" is out of range for type integer`},
		{`SELECT CAST(1e300 AS BIGINT)`, data, "22003", "bigint out of range"},
		{`SELECT CAST(123456 AS NUMERIC(5, 2))`, data, "22003", "numeric field overflow"},
		{`SELECT CAST('123456' AS NUMERIC(5, 2))`, data, "22003",
			"numeric field overflow; a field with precision 5, scale 2 must round to an absolute value less than 10^3"},
		{`SELECT CAST('1e9999999999' AS NUMERIC)`, data, "22003", "value overflows numeric format"},
		{`SELECT CAST('1e400' AS DOUBLE PRECISION)`, data, "22003", `"1e400" is out of range for type double precision`},
		{`SELECT CAST('1e39' AS REAL)`, data, "22003", `"1e39" is out of range for type real`},
		{`SELECT CAST(CAST(1e39 AS DOUBLE PRECISION) AS REAL)`, data, "22003", "value out of range: overflow"},
		{`SELECT CAST(CAST('NaN' AS DOUBLE PRECISION) AS NUMERIC(5, 2))`, data, "22003", "NaN"},
		{`SELECT 9223372036854775807 + 1`, data, "22003", "integer overflow"},
		{`SELECT ACOS(2)`, data, "22003", "input is out of range"},
		{`SELECT PERCENTILE_CONT(2) WITHIN GROUP (ORDER BY n) FROM it_err`, data, "22003", "percentile value 2 is not between 0 and 1"},
		// division_by_zero.
		{`SELECT 1 / 0`, data, "22012", "division by zero"},
		{`SELECT 1.5 / 0`, data, "22012", "division by zero"},
		{`SELECT MOD(1, 0)`, data, "22012", "division by zero"},
		{`SELECT INTERVAL '1 day' / 0`, data, "22012", "division by zero"},
		// Dates and times: invalid_datetime_format, and
		// datetime_field_overflow for fields out of range.
		{`SELECT CAST('abc' AS DATE)`, data, "22007", `invalid input syntax for type date: "abc"`},
		{`SELECT DATE 'abc'`, data, "22007", `invalid input syntax for type date: "abc"`},
		{`SELECT DATE '2024-01-01' = 'abc'`, data, "22007", `invalid input syntax for type date: "abc"`},
		{`SELECT CAST('abc' AS TIMESTAMP)`, data, "22007", `invalid input syntax for type timestamp: "abc"`},
		{`SELECT CAST('abc' AS TIMESTAMP WITH TIME ZONE)`, data, "22007", `invalid input syntax for type timestamp with time zone: "abc"`},
		{`SELECT CAST('abc' AS TIME)`, data, "22007", `invalid input syntax for type time: "abc"`},
		{`SELECT CAST('abc' AS INTERVAL)`, data, "22007", `"abc"`},
		{`SELECT CAST('1:75:00' AS INTERVAL)`, data, "22015", `interval field value out of range: "1:75:00"`},
		{`SELECT CAST('2562047:47:16.854776' AS INTERVAL)`, data, "22008", "interval out of range"},
		{`SELECT CAST('2024-02-30' AS DATE)`, data, "22008", `date/time field value out of range: "2024-02-30"`},
		{`SELECT CAST('2024-02-30 10:00' AS TIMESTAMP)`, data, "22008", `date/time field value out of range: "2024-02-30 10:00"`},
		{`SELECT CAST('25:00' AS TIME)`, data, "22008", `date/time field value out of range: "25:00"`},
		{`SELECT CAST('24:00:01' AS TIME)`, data, "22008", `date/time field value out of range: "24:00:01"`},
		{`SELECT CAST('5881580-07-12' AS DATE)`, data, "22008", `date out of range: "5881580-07-12"`},
		{`SELECT CAST('294277-01-01' AS TIMESTAMP)`, data, "22008", `timestamp out of range: "294277-01-01"`},
		{`SELECT CAST('2024-01-05 10:00+16' AS TIMESTAMP WITH TIME ZONE)`, data, "22009", `time zone displacement out of range: "2024-01-05 10:00+16"`},
		{`SELECT CAST('2024-01-05 10:00 Mars/Base' AS TIMESTAMP WITH TIME ZONE)`, data, "22023", `time zone "mars/base" not recognized`},
		{`SELECT DATE '2024-01-01' + 2147483647`, data, "22008", "date out of range"},
		{`SELECT INTERVAL '2147483647 days' * 2`, data, "22008", "interval out of range"},
		{`SELECT INTERVAL '1 mon' / 1e-300`, data, "22008", "interval out of range"},
		{`SELECT SUM(x) FROM (SELECT INTERVAL '2147483647 days' AS x UNION ALL SELECT INTERVAL '1 day') t`, data, "22008", "interval out of range"},
		{`SELECT AVG(x) FROM (SELECT INTERVAL '2147483647 days' AS x UNION ALL SELECT INTERVAL '1 day') t`, data, "22008", "interval out of range"},
		{`SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY x) FROM (SELECT INTERVAL '-2147483648 mons' AS x UNION ALL SELECT INTERVAL '2147483647 mons') t`, data, "22008", "interval out of range"},
		{`SELECT MAKE_DATE(2024, 2, 30)`, data, "22008", "date field value out of range: 2024-02-30"},
		{`SELECT TO_DATE('2024-02-30', 'YYYY-MM-DD')`, data, "22008", `date/time field value out of range: "2024-02-30"`},
		{`SELECT TO_DATE('xx', 'MM')`, data, "22007", `invalid value "xx" for "MM"`},
		{`SELECT TO_TIMESTAMP('2024-01-01 +25', 'YYYY-MM-DD TZH')`, data, "22009", "time zone displacement out of range"},
		{`SELECT CAST('2024-01-05 10:00:00+16' AS TIMESTAMP)`, data, "22009", `time zone displacement out of range: "2024-01-05 10:00:00+16"`},
		{`SELECT TIMESTAMP '2024-01-05 10:00:00-16'`, data, "22009", `time zone displacement out of range: "2024-01-05 10:00:00-16"`},
		{`SELECT TO_CHAR(INTERVAL '1 day', 'Day')`, data, "22007", "invalid format specification for an interval value"},
		{`SELECT DATE_BIN(INTERVAL '-1 day', TIMESTAMP '2024-01-01', TIMESTAMP '2024-01-01')`, data, "22008", "stride must be greater than zero"},
		{`SELECT DATE_BIN(INTERVAL '1 month', TIMESTAMP '2024-01-01', TIMESTAMP '2024-01-01')`, notImp, "0A000", "months or years"},
		{`SELECT EXTRACT(day FROM TIME '10:00')`, notImp, "0A000", `field "day" is not valid for TIME values`},
		{`SELECT EXTRACT(foo FROM DATE '2024-01-01')`, data, "22023", `unknown date/time field "foo"`},
		{`SELECT NOW() AT TIME ZONE 'Nowhere/Zone'`, data, "22023", `time zone "Nowhere/Zone" not recognized`},
		{`SELECT CAST('abc' AS TIME WITH TIME ZONE)`, data, "22007", `invalid input syntax for type time with time zone: "abc"`},
		{`SELECT CAST('25:00+02' AS TIMETZ)`, data, "22008", `date/time field value out of range: "25:00+02"`},
		{`SELECT CAST('10:00+16' AS TIMETZ)`, data, "22009", `time zone displacement out of range: "10:00+16"`},
		{`SELECT EXTRACT(year FROM TIMETZ '10:00+02')`, notImp, "0A000", `unit "year" not supported for type time with time zone`},
		{`SELECT EXTRACT(foo FROM TIMETZ '10:00+02')`, data, "22023", `unit "foo" not recognized for type time with time zone`},
		// Functions' arguments.
		{`SELECT LN(0)`, data, "2201E", "cannot take logarithm of zero"},
		{`SELECT SQRT(-1)`, data, "2201F", "cannot take square root of a negative number"},
		{`SELECT 'a' ~ '('`, data, "2201B", "invalid regular expression"},
		{`SELECT SUBSTRING('abc', 1, -1)`, data, "22011", "negative substring length not allowed"},
		{`SELECT SPLIT_PART('a,b', ',', 0)`, data, "22023", "field position must not be zero"},
		{`SELECT 'a' LIKE 'b' ESCAPE 'xy'`, data, "22025", "ESCAPE"},
		{`SELECT * FROM GENERATE_SERIES(1, 10, 0)`, data, "22023", "step size cannot equal zero"},
		{`SELECT NTILE(0) OVER ()`, data, "22014", "argument of NTILE must be greater than zero"},
		{`SELECT NTH_VALUE(1, 0) OVER ()`, data, "22016", "argument of NTH_VALUE must be greater than zero"},
		{`SELECT JSON_BUILD_OBJECT(NULL, 1)`, data, "22004", "null value not allowed for object key"},
		{`SELECT JSON_ARRAY_LENGTH('{}')`, data, "22023", "cannot get array length of a non-array"},
		{`SELECT JSON_OBJECT('{a}')`, data, "2202E", "array must have even number of elements"},
		{`SELECT CAST('a' AS VARCHAR(0))`, data, "22023", "length for type varchar must be at least 1"},
		{`SELECT 'x' + 1`, data, "22P02", `"x"`},
		{`SELECT CAST(1 AS SMALLINT) + '40000'`, data, "22003", "40000"},
		// A type that never converts: cannot_coerce, also in TRY_CAST.
		{`SELECT CAST(DATE '2024-01-01' AS BOOLEAN)`, arg, "42846", "BOOLEAN"},
		{`SELECT TRY_CAST(DATE '2024-01-01' AS BOOLEAN)`, arg, "42846", "BOOLEAN"},

		// Writes: the column's name, then the value's error.
		{`INSERT INTO it_err (id, n) VALUES (2, 'x')`, data, "22P02", `column "n": `},
		{`INSERT INTO it_err (id, n) SELECT 2, 'x'`, data, "22P02", `column "n": `},
		{`UPDATE it_err SET n = 'x'`, data, "22P02", `column "n": `},
		{`UPDATE it_err SET n = 1 / 0`, data, "22012", "division by zero"},
		{`INSERT INTO it_err (id, d) VALUES (2, '2024-02-30')`, data, "22008", `column "d": date/time field value out of range: "2024-02-30"`},
		{`INSERT INTO it_err (id, b) VALUES (2, 'maybe')`, data, "22P02", `column "b": invalid input syntax for type boolean: "maybe"`},
		{`INSERT INTO it_err_num (m) VALUES ('x')`, data, "22P02", `column "m": invalid input syntax for type numeric: "x"`},
		{`INSERT INTO it_err_num (m) VALUES (1234.5)`, data, "22003", `column "m": numeric field overflow`},
		{`INSERT INTO it_err_num (r) VALUES ('1e39')`, data, "22003", `column "r": "1e39" is out of range for type real`},
		// 1e300 is a DOUBLE PRECISION here, and a NUMERIC in Postgres
		// (`"1000…" is out of range for type real`).
		{`INSERT INTO it_err_num (r) VALUES (1e300)`, data, "22003", `column "r": value out of range: overflow`},
		{`INSERT INTO it_err_num (f) VALUES ('x')`, data, "22P02", `column "f": invalid input syntax for type double precision: "x"`},
		{`INSERT INTO it_err_num (t) VALUES ('24:00:01')`, data, "22008", `column "t": date/time field value out of range: "24:00:01"`},
		{`INSERT INTO it_err_num (ts) VALUES ('x')`, data, "22007", `column "ts": invalid input syntax for type timestamp with time zone: "x"`},
		{`UPDATE it_err SET d = 'x'`, data, "22007", `column "d": invalid input syntax for type date: "x"`},
		{`MERGE INTO it_err t USING (SELECT 2 AS id) s ON t.id = s.id WHEN NOT MATCHED THEN INSERT (id, n) VALUES (s.id, 'x')`, data, "22P02", `column "n": `},
		{`INSERT INTO it_err (id, v) VALUES (2, 'abcd')`, data, "22001", "value too long for type character varying(3)"},
		{`UPDATE it_err SET v = 'abcd'`, data, "22001", "value too long for type character varying(3)"},
		// A type that never converts to the column's: datatype_mismatch.
		{`INSERT INTO it_err (id, b) VALUES (2, DATE '2024-01-01')`, arg, "42804", `column "b": `},
		// Integrity violations.
		{`INSERT INTO it_err (id) VALUES (NULL)`, integ, "23502", "violates not-null constraint"},
		{`UPDATE it_err SET id = NULL`, integ, "23502", "violates not-null constraint"},
		{`INSERT INTO it_err (id, c) VALUES (2, -1)`, integ, "23514", "violates check constraint"},
		{`ALTER TABLE it_err2 ADD CONSTRAINT x_pos CHECK (x > 0)`, integ, "23514", `check constraint "x_pos" of relation "it_err2" is violated by some row`},
		{`ALTER TABLE it_err ALTER COLUMN v SET NOT NULL`, integ, "23502", `column "v" of relation "it_err" contains null values`},

		// Errors in the statement (class 42), as adbc-driver-postgresql
		// classifies them.
		{`SELECT 1 +`, arg, "42601", "syntax error"},
		{`SELECT JSON_VALUE('{}', '$.')`, arg, "42601", "syntax error at end of jsonpath input"},
		{`SELECT * FROM it_err_nosuch`, absent, "42P01", "does not exist"},
		{`SELECT nosuch FROM it_err`, absent, "42703", `column "nosuch" does not exist`},
		{`INSERT INTO it_err (nosuch) VALUES (1)`, absent, "42703", `column "nosuch" does not exist`},
		{`SELECT nosuchfunc(1)`, absent, "42883", "function nosuchfunc(integer) does not exist"},
		// A quoted literal next to an operator with no form for the other
		// operand's type, or more than one (see unknown.go).
		{`SELECT TRUE + '1'`, absent, "42883", "operator does not exist: boolean + unknown"},
		{`SELECT DATE '2024-01-01' + '1'`, arg, "42725", "operator is not unique: date + unknown"},
		{`CREATE TABLE it_err (x INTEGER)`, exists, "42P07", "already exists"},
		{`INSERT INTO it_err (id, id) VALUES (1, 2)`, exists, "42701", `column "id" specified more than once`},
		{`ALTER TABLE it_err ADD COLUMN n INTEGER`, exists, "42701", `column "n" of relation "it_err" already exists`},
		{`ALTER TABLE it_err ALTER COLUMN nosuch SET DEFAULT 1`, absent, "42703", `column "nosuch" of relation "it_err" does not exist`},
		{`ALTER TABLE it_err DROP COLUMN nosuch`, absent, "42703", `column "nosuch" of relation "it_err" does not exist`},
		{`CREATE TABLE it_err3 (a INTEGER PRIMARY KEY, b INTEGER PRIMARY KEY)`, arg, "42P16", `multiple primary keys for table "it_err3" are not allowed`},
		{`ALTER TABLE it_err_pk ALTER COLUMN k DROP NOT NULL`, arg, "42P16", `column "k" is in a primary key`},
		{`CREATE TABLE it_err3 (a INTEGER, PRIMARY KEY (nosuch))`, absent, "42703", `column "nosuch" named in key does not exist`},
		{`CREATE TABLE it_err3 (a INTEGER, PRIMARY KEY (a, a))`, exists, "42701", `column "a" appears twice in primary key constraint`},
		{`CREATE TABLE it_err3 (a INTEGER CONSTRAINT c CHECK (a > 0), CONSTRAINT c PRIMARY KEY (a))`, exists, "42710", `constraint "c" for relation "it_err3" already exists`},
		{`CREATE TABLE it_err3 (a INTEGER, CONSTRAINT c CHECK (a > 0), CONSTRAINT c CHECK (a > 1))`, exists, "42710", `check constraint "c" already exists`},
		// Column names that repeat: in a table or view, duplicate_column; read
		// from a query, ambiguous_column.
		{`CREATE TABLE it_err3 (a INTEGER, a INTEGER)`, exists, "42701", `column "a" specified more than once`},
		{`CREATE TABLE it_err3 AS SELECT count(*), count(n) FROM it_err`, exists, "42701", `column "count" specified more than once`},
		{`CREATE VIEW it_err3 AS SELECT 1, 2`, exists, "42701", `column "?column?" specified more than once`},
		{`SELECT id FROM (SELECT id, id FROM it_err) s`, arg, "42702", `column reference "id" is ambiguous`},
		{`SELECT x FROM it_err2, (SELECT 1 AS x) s`, arg, "42702", `column reference "x" is ambiguous`},
		{`SELECT * FROM (SELECT 1, 2) s ORDER BY "?column?"`, arg, "42702", `ORDER BY "?column?" is ambiguous`},
		{`SELECT 1 AS a, 2 AS a UNION SELECT 3, 4 ORDER BY a`, arg, "42702", `ORDER BY "a" is ambiguous`},
		{`SELECT * FROM (SELECT 1 AS a, 2 AS a) s JOIN (SELECT 1 AS a) r USING (a)`, arg, "42702", `common column name "a" appears more than once in left table`},

		// ALTER COLUMN … TYPE: the conversion, the rows' constraints, and
		// what Postgres refuses.
		{`ALTER TABLE it_err ALTER COLUMN d TYPE BOOLEAN`, arg, "42804", `column "d" cannot be cast automatically to type boolean`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BOOLEAN USING d`, arg, "42804", `result of USING clause for column "n" cannot be cast automatically to type boolean`},
		{`ALTER TABLE it_err_at ALTER COLUMN b TYPE DATE USING NULL`, arg, "42804", `default for column "b" cannot be cast automatically to type date`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE INTEGER USING 'x'`, data, "22P02", `"x"`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE SMALLINT USING 100000`, data, "22003", "out of range"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE VARCHAR(1)`, data, "22001", "value too long for type character varying(1)"},
		{`ALTER TABLE it_err ALTER COLUMN id TYPE BIGINT USING NULL`, integ, "23502", `column "id" of relation "it_err" contains null values`},
		{`ALTER TABLE it_err ALTER COLUMN c TYPE INTEGER USING -1`, integ, "23514", `of relation "it_err" is violated by some row`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT, ALTER COLUMN n TYPE TEXT`, notImp, "0A000", `cannot alter type of column "n" twice`},
		{`ALTER TABLE it_err_at ALTER COLUMN a TYPE BIGINT`, notImp, "0A000", "cannot alter type of a column used by a view or rule"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING (SELECT 1)`, notImp, "0A000", "cannot use subquery in transform expression"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING ROW_NUMBER() OVER ()`, arg, "42P20", "window functions are not allowed in transform expressions"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING COUNT(*)`, arg, "42803", "aggregate functions are not allowed in transform expressions"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING GROUPING(n)`, arg, "42803", "grouping operations are not allowed in transform expressions"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING generate_series(1, 2)`, notImp, "0A000", "set-returning functions are not allowed in transform expressions"},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING nope`, absent, "42703", `column "nope" does not exist`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE RECORD`, arg, "42P16", `column "n" has pseudo-type record`},
		{`ALTER TABLE it_err ALTER COLUMN n TYPE VARCHAR(0)`, data, "22023", "length for type varchar must be at least 1"},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), c.code, c.state, c.msg)
	}
	// Nothing was written.
	h.expectRows(`SELECT id, n, v, d, b, c FROM it_err`, "1|10|NULL|NULL|NULL|NULL")

	// TRY_CAST still gives NULL for a value that doesn't convert.
	h.expectRows(`SELECT TRY_CAST('abc' AS INTEGER), TRY_CAST('2024-02-30' AS DATE), TRY_CAST(3000000000 AS INTEGER)`, "NULL|NULL|NULL")

	// A bound parameter that doesn't convert.
	mem := memory.DefaultAllocator
	sb := array.NewStringBuilder(mem)
	defer sb.Release()
	sb.Append("x")
	sCol := sb.NewArray()
	defer sCol.Release()
	strRec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "p", Type: arrow.BinaryTypes.String}}, nil), []arrow.Array{sCol}, 1)
	defer strRec.Release()
	checkState(t, "bound 'x' cast to INTEGER", h.stmtErr(`SELECT CAST($1 AS INTEGER)`, strRec), data, "22P02", `"x"`)
	checkState(t, "bound 'x' inserted into INTEGER", h.stmtErr(`INSERT INTO it_err (id, n) VALUES (2, $1)`, strRec), data, "22P02", `column "n": `)
	// A transform expression has no parameters, as in Postgres's ALTER TABLE.
	checkState(t, "bound parameter in USING", h.stmtErr(`ALTER TABLE it_err ALTER COLUMN n TYPE BIGINT USING $1`, strRec), absent, "42P02", "there is no parameter $1")
	ub := array.NewUint64Builder(mem)
	defer ub.Release()
	ub.Append(1 << 63)
	uCol := ub.NewArray()
	defer uCol.Release()
	uRec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Uint64}}, nil), []arrow.Array{uCol}, 1)
	defer uRec.Release()
	checkState(t, "bound uint64 2^63", h.stmtErr(`SELECT CAST($1 AS BIGINT)`, uRec), data, "22003", "bigint out of range")

	// Bulk ingest.
	ingest := func(rec arrow.RecordBatch) error {
		t.Helper()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		for k, v := range map[string]string{
			adbc.OptionKeyIngestTargetTable: "it_err",
			adbc.OptionKeyIngestMode:        adbc.OptionValueIngestModeAppend,
		} {
			if err := st.SetOption(h.ctx, k, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.Bind(h.ctx, rec); err != nil {
			t.Fatal(err)
		}
		_, err = st.ExecuteUpdate(h.ctx)
		return err
	}
	checkState(t, "ingest of uint64 2^63", ingest(uRec), data, "22003", `column "id": integer out of range`)
	nb := array.NewInt64Builder(mem)
	defer nb.Release()
	nb.AppendNull()
	nCol := nb.NewArray()
	defer nCol.Release()
	nullRec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}, nil), []arrow.Array{nCol}, 1)
	defer nullRec.Release()
	checkState(t, "ingest of a NULL id", ingest(nullRec), integ, "23502", "violates not-null constraint")
	textRec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.BinaryTypes.String}}, nil), []arrow.Array{sCol}, 1)
	defer textRec.Release()
	checkState(t, "ingest of 'x' into an INTEGER", ingest(textRec), data, "22P02", `column "n": `)
	h.expectRows(`SELECT id, n, v, d, b, c FROM it_err`, "1|10|NULL|NULL|NULL|NULL")
}
