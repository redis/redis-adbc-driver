// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestIngestStringArenaOwnership(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprintf("large_%t", large), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			var builder array.StringLikeBuilder
			if large {
				builder = array.NewLargeStringBuilder(mem)
			} else {
				builder = array.NewStringBuilder(mem)
			}
			for i, s := range []string{"outside", "é\x00🙂", "", "unused", "last", "tail"} {
				if i == 3 {
					builder.AppendNull()
				} else {
					builder.Append(s)
				}
			}
			whole := builder.NewArray()
			builder.Release()
			input := array.NewSlice(whole, 1, 5)
			whole.Release()
			rows := makeIngestStringRows(4, 2)
			for _, row := range rows {
				row[1] = intValue(typeInt64, 9007199254740993)
			}
			if err := ingestColumn(input, typeString, utcZone, rows, 0); err != nil {
				t.Fatal(err)
			}
			want := []Value{stringValue("é\x00🙂"), stringValue(""), nullValue(typeString), stringValue("last")}
			for i, row := range rows {
				if !reflect.DeepEqual(row[0], want[i]) || row[1].I != 9007199254740993 {
					t.Fatalf("row%d got%#v", i, row)
				}
			}
			for i := range input.Data().Buffers()[2].Bytes() {
				input.Data().Buffers()[2].Bytes()[i] = 'X'
			}
			input.Release()
			retained := rows[0][0]
			rows = nil
			if retained.S != "é\x00🙂" {
				t.Fatalf("retained string aliases Arrow: %q", retained.S)
			}
		})
	}
}

func makeIngestStringRows(n, columns int) [][]Value {
	rows := make([][]Value, n)
	values := make([]Value, n*columns)
	for i := range rows {
		start := i * columns
		rows[i] = values[start : start+columns : start+columns]
	}
	return rows
}

func TestIngestStringArenaCapAndFallback(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprintf("extra_%d", extra), func(t *testing.T) {
			b := array.NewStringBuilder(memory.DefaultAllocator)
			first := strings.Repeat("a", ingestStringArenaBytes/2)
			second := strings.Repeat("b", ingestStringArenaBytes/2+extra)
			b.Append(first)
			b.Append(second)
			input := b.NewArray()
			b.Release()
			defer input.Release()
			rows := makeIngestStringRows(2, 1)
			rows[0][0] = stringValue("untouched")
			if got := ingestStringArena(input.(*array.String), rows, 0); got != (extra == 0) {
				t.Fatalf("eligible%v", got)
			}
			if extra > 0 && rows[0][0].S != "untouched" {
				t.Fatal("oversized preflight changed output")
			}
			if err := ingestColumn(input, typeString, utcZone, rows, 0); err != nil {
				t.Fatal(err)
			}
			for i := range input.Data().Buffers()[2].Bytes() {
				input.Data().Buffers()[2].Bytes()[i] = 'X'
			}
			if rows[0][0].S != first || rows[1][0].S != second {
				t.Fatal("cap/fallback aliases input")
			}
		})
	}
}

type prefixOnlyIngestStrings struct {
	t      *testing.T
	values []string
	max    int
}

func (s prefixOnlyIngestStrings) IsNull(i int) bool {
	if i >= s.max {
		s.t.Fatal("read past conversion rows")
	}
	return false
}
func (s prefixOnlyIngestStrings) Value(i int) string {
	if i >= s.max {
		s.t.Fatal("read past conversion rows")
	}
	return s.values[i]
}

func TestIngestStringArenaPrefixAndEmpty(t *testing.T) {
	source := prefixOnlyIngestStrings{t, []string{"first", "second", "unread"}, 2}
	rows := makeIngestStringRows(2, 1)
	if !ingestStringArena(source, rows, 0) || rows[0][0].S != "first" || rows[1][0].S != "second" {
		t.Fatal("prefix mismatch")
	}
	if !ingestStringArena(prefixOnlyIngestStrings{t, nil, 0}, nil, 0) {
		t.Fatal("empty prefix rejected")
	}
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.AppendNull()
	b.Append("")
	b.AppendNull()
	b.Append("")
	input := b.NewArray()
	b.Release()
	defer input.Release()
	rows = makeIngestStringRows(4, 1)
	if err := ingestColumn(input, typeString, utcZone, rows, 0); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		if row[0].Null != (i%2 == 0) || row[0].S != "" {
			t.Fatalf("row%d%#v", i, row)
		}
	}
}

func TestIngestStringArenaValidationBeforeCopy(t *testing.T) {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	b.Append("first")
	b.Append("second")
	input := b.NewArray()
	b.Release()
	defer input.Release()
	binary.LittleEndian.PutUint32(input.Data().Buffers()[1].Bytes()[8:], uint32(1<<30))
	rows := makeIngestStringRows(2, 1)
	rows[0][0] = stringValue("untouched")
	if err := ingestColumn(input, typeString, utcZone, rows, 0); err == nil {
		t.Fatal("invalid offsets accepted")
	}
	if rows[0][0].S != "untouched" {
		t.Fatal("copy happened before complete validation")
	}
}

func BenchmarkIngestStringConversion(b *testing.B) {
	for _, columns := range []int{8, 105} {
		for _, n := range []int{63, 1024} {
			arrays := make([]arrow.Array, columns)
			targets := make([]ColType, columns)
			for c := range columns {
				if c%3 == 0 {
					builder := array.NewStringBuilder(memory.DefaultAllocator)
					for r := range n {
						if r%17 == 0 {
							builder.AppendNull()
						} else {
							builder.Append(fmt.Sprintf("é\x00column%d/row%d", c, r))
						}
					}
					arrays[c] = builder.NewArray()
					builder.Release()
					targets[c] = typeString
				} else {
					builder := array.NewInt64Builder(memory.DefaultAllocator)
					for r := range n {
						if r%17 == 0 {
							builder.AppendNull()
						} else {
							builder.Append(9007199254740993 + int64(r))
						}
					}
					arrays[c] = builder.NewArray()
					builder.Release()
					targets[c] = typeInt64
				}
			}
			for _, arena := range []bool{false, true} {
				label := "clone"
				if arena {
					label = "arena"
				}
				b.Run(fmt.Sprintf("columns%d/rows%d/%s", columns, n, label), func(b *testing.B) {
					run := func(check bool) {
						rows := makeIngestStringRows(n, columns)
						for c, a := range arrays {
							convert := ingestColumnCloneReference
							if arena {
								convert = ingestColumn
							}
							if err := convert(a, targets[c], utcZone, rows, c); err != nil {
								b.Fatal(err)
							}
						}
						if check {
							for r, row := range rows {
								for c, got := range row {
									want, err := valueAt(arrays[c], r)
									if err == nil {
										want, err = coerceIn(want, targets[c], utcZone)
									}
									if err != nil || !reflect.DeepEqual(got, want) {
										b.Fatalf("row%d col%d got%#v want%#v error%v", r, c, got, want, err)
									}
								}
							}
						}
					}
					run(true)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						run(false)
					}
					b.StopTimer()
					run(true)
				})
			}
			for _, a := range arrays {
				a.Release()
			}
		}
	}
}

// Exact baseline06c76c0b conversion control, renamed only for paired allocation timing.
func ingestColumnCloneReference(arr arrow.Array, target ColType, zone tzZone, rows [][]Value, column int) error {
	if err := validateInputArray(arr); err != nil {
		return errorf(adbc.StatusInvalidArgument, "invalid Arrow input: %v", err)
	}
	var read func(int) Value
	exactInteger := false
	switch a := arr.(type) {
	case *array.Int16:
		read = func(i int) Value { return intValue(typeInt16, int64(a.Value(i))) }
		exactInteger = target == typeInt16
	case *array.Int32:
		read = func(i int) Value { return intValue(typeInt32, int64(a.Value(i))) }
		exactInteger = target == typeInt32
	case *array.Int64:
		read = func(i int) Value { return intValue(typeInt64, a.Value(i)) }
		exactInteger = target == typeInt64
	case *array.String:
		read = func(i int) Value { return stringValue(strings.Clone(a.Value(i))) }
	case *array.LargeString:
		read = func(i int) Value { return stringValue(strings.Clone(a.Value(i))) }
	}
	// A validated signed integer array already has the complete target type
	// and range. Bind that fact once per column; aliases, narrowing, other
	// conversions and every non-integer layout retain coerceIn below.
	if exactInteger {
		for i := range rows {
			if arr.IsNull(i) {
				rows[i][column] = nullValue(target)
			} else {
				rows[i][column] = read(i)
			}
		}
		return nil
	}
	for i := range rows {
		var v Value
		var err error
		switch {
		case read == nil:
			v, err = valueAt(arr, i)
		case arr.IsNull(i):
			v = nullValue(target)
		default:
			v = read(i)
		}
		if err != nil {
			return err
		}
		v, err = coerceIn(v, target, zone)
		if err != nil {
			return err
		}
		rows[i][column] = v
	}
	return nil
}
