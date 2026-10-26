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

// Integration tests for text read as an integer (intin.go) by casts,
// INSERT and UPDATE, comparisons, bound parameters and bulk ingest. The
// values and errors of the statements are postgres:16's, but for the
// driver's `column "n": ` before an error writing a column. Postgres has
// no counterpart for bound and ingested strings, which the driver reads as
// it reads text written to the column.

import (
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// textBatch is a batch of one string column.
func textBatch(name string, vals ...string) arrow.RecordBatch {
	sb := array.NewStringBuilder(memory.DefaultAllocator)
	defer sb.Release()
	sb.AppendValues(vals, nil)
	col := sb.NewArray()
	defer col.Release()
	return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.BinaryTypes.String}}, nil),
		[]arrow.Array{col}, int64(len(vals)))
}

func TestSQLTextToInteger(t *testing.T) {
	h := newSQLHarness(t)
	h.dropTables("it_intin", "it_intin_pk")
	h.exec(`CREATE TABLE it_intin (id INTEGER, s SMALLINT, n INTEGER, b BIGINT, u INTEGER NOINDEX, t VARCHAR)`)
	h.exec(`INSERT INTO it_intin VALUES (1, ' 7 ', '0x10', '1_000_000', '-0b11', ' 12 '),
		(2, '+2', '-0o17', '9223372036854775807', '0', '1.5')`)
	h.exec(`CREATE TABLE it_intin_pk (k INTEGER PRIMARY KEY, v VARCHAR)`)
	h.exec(`INSERT INTO it_intin_pk VALUES ('1', 'a'), ('0x2', 'b')`)
	all := []string{"1|7|16|1000000|-3| 12 ", "2|2|-15|9223372036854775807|0|1.5"}
	h.expectRows(`SELECT id, s, n, b, u, t FROM it_intin ORDER BY id`, all...)

	// Text that is an integer, read from the rows and compared with
	// integer columns, indexed or not, and a key.
	h.expectRows(`SELECT id, CAST(t AS INTEGER) FROM it_intin WHERE id = 1`, "1|12")
	h.expectRows(`SELECT id, TRY_CAST(t AS INTEGER) FROM it_intin ORDER BY id`, "1|12", "2|NULL")
	h.expectRows(`SELECT id FROM it_intin WHERE n = ' 16 '`, "1")
	h.expectRows(`SELECT id FROM it_intin WHERE u = '-0b11'`, "1")
	h.expectRows(`SELECT id FROM it_intin WHERE b > '0x10' ORDER BY id`, "1", "2")
	h.expectRows(`SELECT id FROM it_intin WHERE s IN ('7', '0x7') ORDER BY id`, "1")
	h.expectRows(`SELECT v FROM it_intin_pk WHERE k = '0x2'`, "b")

	const data = adbc.StatusInvalidData
	for _, c := range []struct{ sql, state, msg string }{
		// Written to a column.
		{`INSERT INTO it_intin (id, n) VALUES (3, '1.5')`, "22P02", `column "n": invalid input syntax for type integer: "1.5"`},
		{`INSERT INTO it_intin (id, n) VALUES (3, '1e3')`, "22P02", `column "n": invalid input syntax for type integer: "1e3"`},
		{`INSERT INTO it_intin (id, b) VALUES (3, '')`, "22P02", `column "b": invalid input syntax for type bigint: ""`},
		{`INSERT INTO it_intin (id, s) VALUES (3, '40000')`, "22003", `column "s": value "40000" is out of range for type smallint`},
		{`INSERT INTO it_intin (id, b) VALUES (3, '99999999999999999999')`, "22003",
			`column "b": value "99999999999999999999" is out of range for type bigint`},
		{`UPDATE it_intin SET n = '2.5' WHERE id = 1`, "22P02", `column "n": invalid input syntax for type integer: "2.5"`},
		{`UPDATE it_intin SET u = 'x'`, "22P02", `column "u": invalid input syntax for type integer: "x"`},
		{`INSERT INTO it_intin_pk VALUES ('3.0', 'c')`, "22P02", `column "k": invalid input syntax for type integer: "3.0"`},
		// Read from the rows.
		{`SELECT CAST(t AS INTEGER) FROM it_intin`, "22P02", `invalid input syntax for type integer: "1.5"`},
		{`SELECT CAST(t AS SMALLINT) FROM it_intin`, "22P02", `invalid input syntax for type smallint: "1.5"`},
		// Compared with an integer.
		{`SELECT id FROM it_intin WHERE n = '1.5'`, "22P02", `invalid input syntax for type integer: "1.5"`},
		{`SELECT id FROM it_intin WHERE u < '1e3'`, "22P02", `invalid input syntax for type integer: "1e3"`},
		{`SELECT id FROM it_intin WHERE b = 'x'`, "22P02", `invalid input syntax for type bigint: "x"`},
		{`SELECT id FROM it_intin WHERE n BETWEEN '0' AND '1.5'`, "22P02", `invalid input syntax for type integer: "1.5"`},
		{`SELECT id FROM it_intin WHERE n = '99999999999'`, "22003", `value "99999999999" is out of range for type integer`},
		{`SELECT v FROM it_intin_pk WHERE k = '1.5'`, "22P02", `invalid input syntax for type integer: "1.5"`},
		{`SELECT id FROM it_intin WHERE n IS DISTINCT FROM '1.0'`, "22P02", `invalid input syntax for type integer: "1.0"`},
	} {
		checkState(t, c.sql, h.stmtErr(c.sql, nil), data, c.state, c.msg)
	}
	h.expectRows(`SELECT id, s, n, b, u, t FROM it_intin ORDER BY id`, all...)

	// Bound strings.
	rec := textBatch("p", "1.5")
	defer rec.Release()
	checkState(t, "bound '1.5' inserted into INTEGER", h.stmtErr(`INSERT INTO it_intin (id, n) VALUES (3, $1)`, rec),
		data, "22P02", `column "n": invalid input syntax for type integer: "1.5"`)
	checkState(t, "bound '1.5' compared with INTEGER", h.stmtErr(`SELECT id FROM it_intin WHERE n = $1`, rec),
		data, "22P02", `invalid input syntax for type integer: "1.5"`)
	checkState(t, "bound '1.5' cast to BIGINT", h.stmtErr(`SELECT CAST($1 AS BIGINT)`, rec),
		data, "22P02", `invalid input syntax for type bigint: "1.5"`)
	ok := textBatch("p", " 0x10 ")
	defer ok.Release()
	if _, rows, _ := h.queryWith(`SELECT id FROM it_intin WHERE n = $1`, ok); len(rows) != 1 || rows[0] != "1" {
		t.Errorf("bound ' 0x10 ' compared with n: %q, want [1]", rows)
	}

	// Bulk ingest of strings into the columns: a batch with one that
	// doesn't convert writes nothing.
	ingest := func(rec arrow.RecordBatch) error {
		t.Helper()
		st, err := h.conn.NewStatement(h.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close(h.ctx)
		for k, v := range map[string]string{
			adbc.OptionKeyIngestTargetTable: "it_intin",
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
	bad := textBatch("n", "5", "1.5")
	defer bad.Release()
	checkState(t, "ingest of '1.5' into INTEGER", ingest(bad), data, "22P02", `column "n": invalid input syntax for type integer: "1.5"`)
	big := textBatch("s", "32768")
	defer big.Release()
	checkState(t, "ingest of '32768' into SMALLINT", ingest(big), data, "22003", `column "s": value "32768" is out of range for type smallint`)
	h.expectRows(`SELECT id, s, n, b, u, t FROM it_intin ORDER BY id`, all...)
	good := textBatch("n", " 5 ", "0b1_0")
	defer good.Release()
	if err := ingest(good); err != nil {
		t.Fatal(err)
	}
	h.expectRows(`SELECT n FROM it_intin WHERE id IS NULL ORDER BY n`, "2", "5")
}

// redis-adbc copy with -type n=INTEGER reads the HASHes' text as a cast
// does: "1.5" doesn't convert (it was 2), and " 0x10 " is 16.
func TestHashScanTextIntegers(t *testing.T) {
	f := newHashFixture(t, "it:intin:")
	f.hset("it:intin:1", "n", " 0x10 ")
	f.hset("it:intin:2", "n", "1.5")
	f.hset("it:intin:3", "n", "-7")
	opts := HashCopyOptions{Prefix: f.prefix, KeyColumn: "_key", Types: map[string]string{"n": "INTEGER"}}
	s, err := scanHashes(f.h.ctx, f.opts, opts)
	if err != nil {
		t.Fatal(err)
	}
	for s.Next() {
	}
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), `it:intin:2, field "n": "1.5" doesn't convert to INTEGER`) ||
		!strings.Contains(err.Error(), `invalid input syntax for type integer: "1.5"`) {
		t.Errorf("got %v", err)
	}
	s.Release()
	opts.NullOnError = true
	if s, err = scanHashes(f.h.ctx, f.opts, opts); err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	rows := scanAll(t, s)
	if rows["it:intin:1"] != "n=16" || rows["it:intin:2"] != "n=NULL" || rows["it:intin:3"] != "n=-7" {
		t.Errorf("got %v", rows)
	}
}
