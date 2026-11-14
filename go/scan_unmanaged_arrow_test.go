// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestUnmanagedArrowInputAgainstExistingBinding(t *testing.T) {
	alias := typeInt64
	alias.Alias = "example_integer_alias"
	uuid := typeString
	uuid.Alias = "uuid"
	values := []Value{nullValue(typeString), intValue(typeInt64, 0), intValue(typeInt64, -1),
		intValue(typeInt64, 32768), intValue(typeInt64, 2147483648), intValue(typeInt64, 9007199254740993),
		intValue(typeInt64, math.MaxInt64), intValue(typeInt64, math.MinInt64), intValue(alias, math.MaxInt64),
		stringValue("a\x00b雪"), stringValue("123"), stringValue("bad"), intValue(uuid, 3),
		floatValue(typeFloat64, math.NaN()), floatValue(typeFloat64, 1.5), boolValue(true),
		intValue(typeDate, 1), intValue(typeTimestamp, 1234567890123456),
	}
	for _, typ := range []ColType{typeInt16, typeInt32, typeInt64, typeString, typeBool, typeFloat64, typeDate, typeTimestamp, typeTimestampTZ, typeTimeTZ, typeBinary, {Kind: KindString, Fixed: true, Length: 5}, decimalType(12, 3)} {
		t.Run(typ.SQLName(), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			cols := []resultColumn{{Name: "v", Type: typ}}
			a, b := array.NewRecordBuilder(mem, resultSchema(cols)), array.NewRecordBuilder(mem, resultSchema(cols))
			defer a.Release()
			defer b.Release()
			actual, control := bindArrowColumn(a.Field(0), typ), bindArrowColumn(b.Field(0), typ)
			inputFn := bindUnmanagedArrowInput(a.Field(0), typ, actual)
			for _, input := range values {
				before := input
				v, wantErr := control.coerce(input)
				if wantErr == nil {
					wantErr = control.append(v)
				}
				var gotErr error
				if inputFn != nil {
					gotErr = inputFn(&input)
				} else {
					gotErr = appendArrowInputFallback(actual, &input)
				}
				if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
					t.Fatalf("input=%#v errors=%v / %v", input, gotErr, wantErr)
				}
				// DeepEqual treats NaN as unequal; compare every other value exactly.
				if !math.IsNaN(input.F) && !reflect.DeepEqual(input, before) {
					t.Fatal("scratch input mutated")
				}
			}
			x, y := a.NewRecordBatch(), b.NewRecordBatch()
			defer x.Release()
			defer y.Release()
			if !array.RecordApproxEqual(x, y, array.WithNaNsEqual(true), array.WithAbsTolerance(0)) {
				t.Fatalf("different outputs: %v / %v", x, y)
			}
		})
	}
}

func scopedInputProjection(width int, unmanaged bool) (*scanIter, []resultColumn, aggRow) {
	meta := &tableMeta{Schema: defaultSchema, Name: "input", KeyPrefix: "input:"}
	it := &scanIter{meta: meta, need: map[string]bool{}}
	raw := aggRow{rowIDField: "1"}
	var items []planItem
	var cols []resultColumn
	for i := range width {
		name := fmt.Sprintf("c%d", i)
		typ, encoded := typeString, "a\x00b雪"
		if i%2 == 0 {
			typ, encoded = typeInt64, "9007199254740993"
		}
		meta.Columns = append(meta.Columns, columnMeta{Name: name, Type: typ})
		it.need[name], raw[name] = true, encoded
		it.fetch = append(it.fetch, name)
		items = append(items, planItem{expr: &ColumnRef{Name: name}})
		cols = append(cols, resultColumn{Name: name, Type: typ})
	}
	if unmanaged {
		meta.Unmanaged, meta.Adopted, meta.KeyRows, meta.AppIndex = true, true, true, true
		meta.IndexName, meta.UnmanagedPrefixes = "input_idx", []string{"input:"}
		raw[keyField] = "input:key"
	} else {
		it.hashFetch = true
	}
	it.projection = newScanProjection(it, items)
	return it, cols, raw
}

func TestUnmanagedArrowInputCompleteRecords(t *testing.T) {
	for _, positional := range []bool{false, true} {
		for _, bytes := range []int64{0, 1024} {
			t.Run(fmt.Sprintf("hash=%t/bytes=%d", positional, bytes), func(t *testing.T) {
				it, cols, raw := scopedInputProjection(105, !positional)
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 65, batchBytes: bytes, skip: 3, left: 67,
					arrow: &scanArrow{scratch: make([]Value, len(cols)), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
				defer r.arrow.release()
				r.batchSizer = newBatchSizer(cols)
				values := make([]any, len(it.fetch)+1)
				values[0] = "1"
				for i, f := range it.fetch {
					values[i+1] = raw[f]
				}
				for range 75 {
					var err error
					if positional {
						err = r.appendArrowHash(values)
					} else {
						err = r.appendArrow(raw)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if r.arrow.rows > 0 {
					r.arrow.ready = append(r.arrow.ready, r.arrow.builder.NewRecordBatch())
					r.arrow.rows = 0
				}
				got := 0
				for _, rec := range r.arrow.ready {
					got += int(rec.NumRows())
					for i := range int(rec.NumCols()) {
						if i%2 == 0 {
							a := rec.Column(i).(*array.Int64)
							for j := range a.Len() {
								if a.Value(j) != 9007199254740993 {
									t.Fatal("integer loss")
								}
							}
						} else {
							a := rec.Column(i).(*array.String)
							for j := range a.Len() {
								if a.Value(j) != "a\x00b雪" {
									t.Fatal("string loss")
								}
							}
						}
					}
				}
				wantInput := !positional && bytes == 0
				if (r.arrow.input != nil) != wantInput {
					t.Fatalf("specialization eligibility: got=%t want=%t", r.arrow.input != nil, wantInput)
				}
				if got != 67 {
					t.Fatalf("rows=%d want67", got)
				}
				retained := r.arrow.ready[0]
				retained.Retain()
				defer retained.Release()
				// Required metadata decode still runs after LIMIT, before the early return.
				raw["c104"] = "bad"
				values[105] = "bad"
				var err error
				if positional {
					err = r.appendArrowHash(values)
				} else {
					err = r.appendArrow(raw)
				}
				if err == nil {
					t.Fatal("ignored required metadata error after LIMIT")
				}
				r.arrow.release()
				if retained.Column(1).(*array.String).Value(0) != "a\x00b雪" {
					t.Fatal("retained record changed")
				}
			})
		}
	}
}

// Complete construction includes fresh scratch/bindings/builders for each
// operation. The control binds only the original typed functions inside the
// clock, disabling fusion without changing unmanaged decode/fit/coercion. The
// candidate also pays its separate input bindings. Raw maps are prepared out
// of the timer; this diagnostic is not an end-to-end driver speedup claim.
func BenchmarkUnmanagedArrowInputConstruction(b *testing.B) {
	for _, width := range []int{8, 105} {
		for _, rows := range []int{63, 1024} {
			for _, fused := range []bool{false, true} {
				b.Run(fmt.Sprintf("columns=%d/rows=%d/fused=%t", width, rows, fused), func(b *testing.B) {
					it, cols, raw := scopedInputProjection(width, true)
					reader := func(mem memory.Allocator) *streamReader {
						r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: rows, left: -1,
							arrow: &scanArrow{scratch: make([]Value, width), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
						// The original typed binding setup stays inside the clock.
						// The candidate also pays its additional unmanaged bindings.
						if !fused && width >= 64 {
							r.arrow.typed = make([]typedArrowColumn, width)
							for j, c := range cols {
								r.arrow.typed[j] = bindArrowColumn(r.arrow.builder.Field(j), c.Type)
							}
						}
						return r
					}
					validate := func() {
						mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
						defer mem.AssertSize(b, 0)
						r := reader(mem)
						defer r.arrow.release()
						var expected [][]Value
						for range rows {
							row, err := it.projection.decode(raw)
							if err != nil {
								b.Fatal(err)
							}
							expected = append(expected, row)
							if err := r.appendArrow(raw); err != nil {
								b.Fatal(err)
							}
						}
						want, err := buildRecord(mem, cols, expected)
						if err != nil {
							b.Fatal(err)
						}
						defer want.Release()
						if len(r.arrow.ready) != 1 || !array.RecordEqual(r.arrow.ready[0], want) {
							b.Fatal("construction reference mismatch")
						}
					}
					validate()
					b.ReportAllocs()
					b.ResetTimer()
					for b.Loop() {
						r := reader(memory.DefaultAllocator)
						for range rows {
							if err := r.appendArrow(raw); err != nil {
								b.Fatal(err)
							}
						}
						if len(r.arrow.ready) != 1 || int(r.arrow.ready[0].NumRows()) != rows {
							b.Fatal("incomplete record")
						}
						r.arrow.release()
					}
					validate()
				})
			}
		}
	}
}

func TestUnmanagedArrowInputEligibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		width     int
		unmanaged bool
		bytes     int64
		missing   int
		want      bool
	}{
		{"unmanaged", 64, true, 0, 0, true},
		{"managed", 64, false, 0, 0, false},
		{"narrow", 63, true, 0, 0, false},
		{"byte budget", 64, true, 1 << 20, 0, false},
		{"no iterator", 64, true, 0, 1, false},
		{"no metadata", 64, true, 0, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, cols, _ := scopedInputProjection(tc.width, tc.unmanaged)
			if it.projection == nil {
				t.Fatal("invalid projection fixture")
			}
			if tc.missing == 1 {
				it = nil
			}
			if tc.missing == 2 {
				it.meta = nil
			}
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, batchBytes: tc.bytes, left: -1,
				arrow: &scanArrow{scratch: make([]Value, tc.width), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
			defer r.arrow.release()
			r.batchSizer = newBatchSizer(cols)
			for i, c := range cols {
				if c.Type.Kind == KindString {
					r.arrow.scratch[i] = stringValue("a\x00雪")
				} else {
					r.arrow.scratch[i] = intValue(typeInt64, 9007199254740993)
				}
			}
			if err := r.appendDecodedArrow(); err != nil {
				t.Fatal(err)
			}
			if (r.arrow.input != nil) != tc.want {
				t.Fatalf("bound=%t want=%t", r.arrow.input != nil, tc.want)
			}
			if tc.want {
				for i, fn := range r.arrow.input {
					if fn == nil {
						t.Fatalf("missing supported binding %d", i)
					}
				}
			}
			r.arrow.release()
			if r.arrow.input != nil {
				t.Fatal("released input bindings retained")
			}
		})
	}
}

func TestUnmanagedArrowInputPartialErrorOrder(t *testing.T) {
	it, cols, raw := scopedInputProjection(64, true)
	cols[63].Type, raw["c63"] = typeInt16, "12"
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, left: -1,
		arrow: &scanArrow{scratch: make([]Value, 64), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	if err := r.appendArrow(raw); err != nil {
		t.Fatal(err)
	}
	if len(r.arrow.input) != 64 || r.arrow.input[63] == nil {
		t.Fatal("unmanaged specialization absent")
	}
	retained := r.arrow.ready[0]
	retained.Retain()
	defer retained.Release()
	raw["c63"] = "32768"
	if err := r.appendArrow(raw); err == nil {
		t.Fatal("ignored late output overflow")
	}
	if r.arrow.rows != 0 || len(r.arrow.ready) != 1 {
		t.Fatal("partial row published")
	}
	// Complete input decode still wins over the already-invalid later output.
	raw["c62"] = "bad"
	if err := r.appendArrow(raw); err == nil || !strings.Contains(err.Error(), "c62") {
		t.Fatalf("wrong error order: %v", err)
	}
	r.arrow.release()
	if retained.Column(1).(*array.String).Value(0) != "a\x00b雪" {
		t.Fatal("retained string changed")
	}
	if retained.Column(63).(*array.Int16).Value(0) != 12 {
		t.Fatal("retained integer changed")
	}
}
