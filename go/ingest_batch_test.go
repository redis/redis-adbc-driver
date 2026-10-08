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

import (
	"fmt"
	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"math"
	"reflect"
	"testing"
	"time"
)

func ingestTestArrays() []arrow.Array {
	alloc := memory.DefaultAllocator
	ib := array.NewInt64Builder(alloc)
	ib.AppendValues([]int64{0, 32767, 32768, math.MaxInt64}, []bool{true, true, true, false})
	sb := array.NewStringBuilder(alloc)
	sb.AppendValues([]string{"42", "", "éx  ", "unused"}, []bool{true, true, true, false})
	i16 := array.NewInt16Builder(alloc)
	i16.AppendValues([]int16{-32768, 32767, 0, 0}, []bool{true, true, true, false})
	i32 := array.NewInt32Builder(alloc)
	i32.AppendValues([]int32{-2147483648, 2147483647, 0, 0}, []bool{true, true, true, false})
	large := array.NewLargeStringBuilder(alloc)
	large.AppendValues([]string{"42", "", "éx  ", ""}, []bool{true, true, true, false})
	ub := array.NewUint64Builder(alloc)
	ub.AppendValues([]uint64{0, 1, math.MaxUint64, 0}, nil)
	db := array.NewDate32Builder(alloc)
	db.AppendValues([]arrow.Date32{0, 15900, -1, 0}, []bool{true, true, true, false})
	builders := []array.Builder{ib, sb, i16, i32, large, ub, db}
	out := make([]arrow.Array, len(builders))
	for i, b := range builders {
		out[i] = b.NewArray()
		b.Release()
	}
	return out
}

func TestIngestColumnCoercion(t *testing.T) {
	arrays := ingestTestArrays()
	defer func() {
		for _, a := range arrays {
			a.Release()
		}
	}()
	targets := []ColType{typeInt16, typeInt32, typeInt64, typeString, typeBinary, typeDate, {Kind: KindString, Length: 3, Fixed: true}}
	for _, a := range arrays {
		for _, target := range targets {
			t.Run(a.DataType().String()+"/"+target.SQLName(), func(t *testing.T) {
				rows := make([][]Value, a.Len())
				for i := range rows {
					rows[i] = make([]Value, 1)
				}
				gotErr := ingestColumn(a, target, utcZone, rows, 0)
				var wantErr error
				for i := 0; i < a.Len(); i++ {
					v, err := valueAt(a, i)
					if err == nil {
						v, err = coerceIn(v, target, utcZone)
					}
					if err != nil {
						wantErr = err
						break
					}
					if !reflect.DeepEqual(rows[i][0], v) {
						t.Fatalf("row %d: got %#v want %#v", i, rows[i][0], v)
					}
				}
				if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) {
					t.Fatalf("got %v want %v", gotErr, wantErr)
				}
			})
		}
	}
}

func TestIngestColumnOwnsStringBytes(t *testing.T) {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.Append("original")
	a := b.NewArray()
	b.Release()
	defer a.Release()
	rows := [][]Value{make([]Value, 1)}
	if err := ingestColumn(a, typeString, utcZone, rows, 0); err != nil {
		t.Fatal(err)
	}
	copy(a.Data().Buffers()[2].Bytes(), "changed!")
	if rows[0][0].S != "original" {
		t.Fatal("ingested string aliases Arrow storage")
	}
}

func BenchmarkIngestColumn(b *testing.B) {
	builder := array.NewInt64Builder(memory.DefaultAllocator)
	for i := 0; i < 10000; i++ {
		builder.Append(int64(i))
	}
	a := builder.NewArray()
	builder.Release()
	defer a.Release()
	rows := make([][]Value, a.Len())
	values := make([]Value, a.Len())
	for i := range rows {
		rows[i] = values[i : i+1 : i+1]
	}
	for _, optimized := range []bool{false, true} {
		name := "per-cell"
		if optimized {
			name = "per-column"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {
				if optimized {
					if err := ingestColumn(a, typeInt64, utcZone, rows, 0); err != nil {
						b.Fatal(err)
					}
				} else {
					for i := 0; i < a.Len(); i++ {
						v, err := valueAt(a, i)
						if err == nil {
							v, err = coerceIn(v, typeInt64, utcZone)
						}
						if err != nil {
							b.Fatal(err)
						}
						rows[i][0] = v
					}
				}
			}
		})
	}
}

// Separate connections must allocate disjoint IDs even while their HASH
// pipelines overlap. The default/constraint path is exercised by the existing
// ingestion integration suite.
func TestParallelBulkIngest(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_parallel_ingest"
	schema := fmt.Sprintf("parallel_ingest_%d", time.Now().UnixNano())
	h.exec("CREATE SCHEMA " + schema)
	h.exec("SET search_path TO " + schema)
	h.t.Cleanup(func() {
		h.exec("DROP TABLE IF EXISTS " + schema + "." + table)
		h.exec("SET search_path TO public")
		h.exec("DROP SCHEMA " + schema)
	})
	h.exec("CREATE TABLE " + table + " (id BIGINT NOT NULL, note VARCHAR)")
	workers := make([]*sqlHarness, 4)
	for i := range workers {
		workers[i] = newSQLHarness(t)
		workers[i].exec("SET search_path TO " + schema)
	}
	errors := make(chan error, len(workers))
	for _, worker := range workers {
		go func(h *sqlHarness) {
			err := fmt.Errorf("worker exited before ingestion returned")
			defer func() { errors <- err }()
			err = h.ingestNotes(table, adbc.OptionValueIngestModeAppend, 2001)
		}(worker)
	}
	for range workers {
		if err := <-errors; err != nil {
			t.Error(err)
		}
	}
	rows, _ := h.query("SELECT COUNT(*), COUNT(DISTINCT __rowid), MIN(__rowid), MAX(__rowid) FROM " + table)
	if !reflect.DeepEqual(rows, []string{"8004|8004|1|8004"}) {
		t.Fatalf("concurrent ingest lost or duplicated rows: %v", rows)
	}
}
