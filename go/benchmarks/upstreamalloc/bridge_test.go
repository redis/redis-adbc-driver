//go:build cgo

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package upstreamalloc

import (
	"fmt"
	"math"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

const columns, totalRows = 105, 10000

func fieldType(col int) arrow.DataType {
	return []arrow.DataType{arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32,
		arrow.PrimitiveTypes.Int64, arrow.BinaryTypes.String, arrow.FixedWidthTypes.Date32,
		&arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, arrow.FixedWidthTypes.Boolean}[col%7]
}

func integerValue(row, col int) int64 {
	if col%7 == 0 {
		if row == 0 {
			return math.MinInt16
		}
		if row == 1 {
			return math.MaxInt16
		}
		return int64(int16(row*17 + col))
	}
	if col%7 == 1 {
		if row == 0 {
			return math.MinInt32
		}
		if row == 1 {
			return math.MaxInt32
		}
		return int64(row*101 - col)
	}
	if row%97 == 0 {
		return math.MaxInt64 - int64(row)
	}
	return 9007199254740993 + int64(row*columns+col)
}

func stringValue(row, col int) string {
	if row%13 == 0 {
		return ""
	}
	return fmt.Sprintf("r%d:c%d:\x00\r\n☃", row, col)
}

func makeRecords(mem memory.Allocator, rows, batch int) []arrow.RecordBatch {
	fields := make([]arrow.Field, columns)
	for col := range fields {
		fields[col] = arrow.Field{Name: fmt.Sprintf("c%d", col), Type: fieldType(col), Nullable: true}
	}
	b := array.NewRecordBuilder(mem, arrow.NewSchema(fields, nil))
	defer b.Release()
	for row := range rows {
		for col := range columns {
			builder := b.Field(col)
			if (row+col)%17 == 0 {
				builder.AppendNull()
				continue
			}
			switch a := builder.(type) {
			case *array.Int16Builder:
				a.Append(int16(integerValue(row, col)))
			case *array.Int32Builder:
				a.Append(int32(integerValue(row, col)))
			case *array.Int64Builder:
				a.Append(integerValue(row, col))
			case *array.StringBuilder:
				a.Append(stringValue(row, col))
			case *array.Date32Builder:
				a.Append(arrow.Date32(18000 + row%3650))
			case *array.TimestampBuilder:
				a.Append(arrow.Timestamp(1700000000000000 + int64(row)*1000003 + int64(col)))
			case *array.BooleanBuilder:
				a.Append(row%2 == 0)
			}
		}
	}
	whole := b.NewRecordBatch()
	defer whole.Release()
	var out []arrow.RecordBatch
	for start := 0; start < rows; start += batch {
		out = append(out, whole.NewSlice(int64(start), int64(min(start+batch, rows))))
	}
	return out
}

func validateRecord(rec arrow.RecordBatch, start int) error {
	if rec.NumCols() != columns {
		return fmt.Errorf("columns: %d", rec.NumCols())
	}
	for col := range columns {
		a := rec.Column(col)
		if !arrow.TypeEqual(a.DataType(), fieldType(col)) {
			return fmt.Errorf("type column %d: %s", col, a.DataType())
		}
		for local := 0; local < a.Len(); local++ {
			row := start + local
			if a.IsNull(local) != ((row+col)%17 == 0) {
				return fmt.Errorf("null %d/%d", row, col)
			}
			if a.IsNull(local) {
				continue
			}
			ok := false
			switch v := a.(type) {
			case *array.Int16:
				ok = int64(v.Value(local)) == integerValue(row, col)
			case *array.Int32:
				ok = int64(v.Value(local)) == integerValue(row, col)
			case *array.Int64:
				ok = v.Value(local) == integerValue(row, col)
			case *array.String:
				ok = v.Value(local) == stringValue(row, col)
			case *array.Date32:
				ok = v.Value(local) == arrow.Date32(18000+row%3650)
			case *array.Timestamp:
				ok = v.Value(local) == arrow.Timestamp(1700000000000000+int64(row)*1000003+int64(col))
			case *array.Boolean:
				ok = v.Value(local) == (row%2 == 0)
			}
			if !ok {
				return fmt.Errorf("value %d/%d", row, col)
			}
		}
	}
	return nil
}

func releaseRecords(records []arrow.RecordBatch) {
	for _, rec := range records {
		rec.Release()
	}
}

type countedReader struct {
	array.RecordReader
	retains, releases atomic.Int32
}

func (r *countedReader) Retain()  { r.retains.Add(1); r.RecordReader.Retain() }
func (r *countedReader) Release() { r.releases.Add(1); r.RecordReader.Release() }

// Minimal reference accounting for v18.8.0's ExportRecordReader documentation
// mismatch. No finalizer/GC is needed: the caller drops the constructor ref, and
// releasing the imported consumer drops the exporter's independently held ref.
func TestArrowStreamProducerReferences(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	b := array.NewInt64Builder(mem)
	b.AppendValues([]int64{9007199254740993, math.MaxInt64}, nil)
	a := b.NewArray()
	b.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.PrimitiveTypes.Int64}}, nil)
	rec := array.NewRecordBatch(schema, []arrow.Array{a}, 2)
	a.Release()
	producer, err := array.NewRecordReader(schema, []arrow.RecordBatch{rec})
	rec.Release()
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedReader{RecordReader: producer}
	consumer, err := roundTripReader(counted)
	if err != nil {
		t.Fatal(err)
	}
	if counted.retains.Load() != 1 || counted.releases.Load() != 1 {
		t.Error("export must retain and caller must release original reference")
	}
	if !consumer.Next() {
		t.Fatal(consumer.Err())
	}
	held := consumer.RecordBatch()
	held.Retain()
	consumer.Release()
	if counted.releases.Load() != 2 {
		t.Error("consumer must release exporter's reference exactly once")
	}
	values := held.Column(0).(*array.Int64)
	if held.NumRows() != 2 || values.Value(0) != 9007199254740993 || values.Value(1) != math.MaxInt64 {
		t.Error("retained values changed after reader release")
	}
	held.Release()
}

func validateHandoff(records []arrow.RecordBatch, mode string) error {
	if mode == "c_stream_schema_once" {
		r, err := roundTripStream(records)
		if err != nil {
			return err
		}
		defer r.Release()
		rows := 0
		for r.Next() {
			if err := validateRecord(r.RecordBatch(), rows); err != nil {
				return err
			}
			rows += int(r.RecordBatch().NumRows())
		}
		if err := r.Err(); err != nil {
			return err
		}
		if rows != totalRows {
			return fmt.Errorf("incomplete stream: %d", rows)
		}
		return nil
	}
	d := newDescriptors()
	defer d.close()
	start := 0
	for _, rec := range records {
		if mode == "go_retain" {
			if err := validateRecord(rec, start); err != nil {
				return err
			}
			start += int(rec.NumRows())
			continue
		}
		owned := d
		if mode == "c_data_schema_each_fresh" {
			owned = newDescriptors()
		}
		out, err := owned.roundTrip(rec, mode != "c_data_schema_known_reuse")
		if err == nil {
			err = validateRecord(out, start)
			out.Release()
		}
		if mode == "c_data_schema_each_fresh" {
			owned.close()
		}
		if err != nil {
			return err
		}
		start += int(rec.NumRows())
	}
	return nil
}

func TestArrowHandoffOwnership(t *testing.T) {
	for _, schemaEach := range []bool{true, false} {
		t.Run(fmt.Sprintf("c_data_schema_each_%t", schemaEach), func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			records := makeRecords(mem, 2000, 1000)
			d := newDescriptors()
			defer d.close()
			first, err := d.roundTrip(records[0], schemaEach)
			if err != nil {
				t.Fatal(err)
			}
			second, err := d.roundTrip(records[1], schemaEach)
			if err != nil {
				t.Fatal(err)
			}
			releaseRecords(records) // imported records must own their buffers now
			if err := validateRecord(first, 0); err != nil {
				t.Error(err)
			}
			if err := validateRecord(second, 1000); err != nil {
				t.Error(err)
			}
			first.Release()
			second.Release()
		})
	}
	for _, early := range []bool{false, true} {
		for worker := range 4 {
			t.Run(fmt.Sprintf("stream_early_%t_worker_%d", early, worker), func(t *testing.T) {
				t.Parallel()
				mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
				defer mem.AssertSize(t, 0)
				records := makeRecords(mem, 3000, 1000)
				r, err := roundTripStream(records)
				if err != nil {
					t.Fatal(err)
				}
				releaseRecords(records)
				if !r.Next() {
					t.Fatal(r.Err())
				}
				held := r.RecordBatch()
				held.Retain()
				if !early {
					count := 1000
					for r.Next() {
						if err := validateRecord(r.RecordBatch(), count); err != nil {
							t.Error(err)
						}
						count += int(r.RecordBatch().NumRows())
					}
					if count != 3000 || r.Err() != nil {
						t.Fatal("incomplete stream", count, r.Err())
					}
				}
				r.Release() // held batch must survive advance AND producer/reader release
				if err := validateRecord(held, 0); err != nil {
					t.Error(err)
				}
				held.Release()
			})
		}
	}
}

var sink int64

func BenchmarkArrowHandoff(b *testing.B) {
	for _, batch := range []int{1000, 10000} {
		records := makeRecords(memory.DefaultAllocator, totalRows, batch)
		for _, mode := range []string{"go_retain", "c_data_schema_each_fresh", "c_data_schema_each_reuse", "c_data_schema_known_reuse", "c_stream_schema_once"} {
			b.Run(fmt.Sprintf("batch_%d/%s", batch, mode), func(b *testing.B) {
				// Complete semantic validation precedes timing. Timed work consumes
				// every record but excludes fixture construction/row conversion.
				if err := validateHandoff(records, mode); err != nil {
					b.Fatal(err)
				}
				d := newDescriptors()
				defer d.close()
				b.ReportAllocs()
				b.ReportMetric(totalRows, "rows/op")
				b.ReportMetric(float64(len(records)), "batches/op")
				b.ResetTimer()
				var sum int64
				for range b.N {
					if mode == "c_stream_schema_once" {
						r, err := roundTripStream(records)
						if err != nil {
							b.Fatal(err)
						}
						var rows int64
						for r.Next() {
							rows += r.RecordBatch().NumRows()
						}
						if err := r.Err(); err != nil {
							b.Fatal(err)
						}
						r.Release()
						if rows != totalRows {
							b.Fatal(rows)
						}
						sum += rows
						continue
					}
					for _, rec := range records {
						if mode == "go_retain" {
							rec.Retain()
							sum += rec.NumRows()
							rec.Release()
							continue
						}
						owned := d
						if mode == "c_data_schema_each_fresh" {
							owned = newDescriptors()
						}
						out, err := owned.roundTrip(rec, mode != "c_data_schema_known_reuse")
						if err != nil {
							b.Fatal(err)
						}
						sum += out.NumRows()
						out.Release()
						if mode == "c_data_schema_each_fresh" {
							owned.close()
						}
					}
				}
				b.StopTimer()
				if err := validateHandoff(records, mode); err != nil {
					b.Fatal(err)
				}
				sink = sum
			})
		}
		releaseRecords(records)
	}
}
