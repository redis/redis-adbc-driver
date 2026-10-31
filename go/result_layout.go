// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// OptionStringResultLayouts selects physical output layouts by exact result
// column name. SQL storage and expression semantics remain canonical.
const OptionStringResultLayouts = "adbc.redis.result_layouts"
const maxResultDictionaryEntries = 65536

type resultLayouts map[string]string

func parseResultLayouts(value string) (resultLayouts, error) {
	if len(value) > 65536 {
		return nil, errorf(adbc.StatusInvalidArgument, "result_layouts exceeds 64 KiB")
	}
	var layouts resultLayouts
	if err := json.Unmarshal([]byte(value), &layouts); err != nil || layouts == nil {
		return nil, errorf(adbc.StatusInvalidArgument, "result_layouts must be a JSON object of column names to layout strings")
	}
	for _, layout := range layouts {
		if _, err := physicalResultType(nil, layout); err != nil {
			return nil, invalidArg(err)
		}
	}
	return layouts, nil
}

// A nil source validates syntax only; actual compatibility is checked against
// ExecuteSchema/the query reader before exposing an output schema.
func physicalResultType(src arrow.DataType, layout string) (arrow.DataType, error) {
	var target arrow.DataType
	compatible := true
	switch layout {
	case "string", "large_string", "string_view":
		compatible = src == nil || src.ID() == arrow.STRING
		switch layout {
		case "string":
			target = arrow.BinaryTypes.String
		case "large_string":
			target = arrow.BinaryTypes.LargeString
		default:
			target = arrow.BinaryTypes.StringView
		}
	case "binary", "large_binary", "binary_view":
		compatible = src == nil || src.ID() == arrow.BINARY
		switch layout {
		case "binary":
			target = arrow.BinaryTypes.Binary
		case "large_binary":
			target = arrow.BinaryTypes.LargeBinary
		default:
			target = arrow.BinaryTypes.BinaryView
		}
	case "dictionary":
		compatible = src == nil || src.ID() == arrow.STRING || src.ID() == arrow.BINARY
		target = &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: src}
	case "decimal32", "decimal64", "decimal128", "decimal256":
		bits, _ := strconv.Atoi(strings.TrimPrefix(layout, "decimal"))
		precision, scale := int32(1), int32(0)
		if src != nil {
			d, ok := src.(arrow.DecimalType)
			compatible = ok
			if ok {
				precision, scale = d.GetPrecision(), d.GetScale()
			}
		}
		maxPrecision := map[int]int32{32: 9, 64: 18, 128: 38, 256: 76}[bits]
		if precision > maxPrecision {
			return nil, fmt.Errorf("%s cannot hold precision %d", layout, precision)
		}
		switch bits {
		case 32:
			target = &arrow.Decimal32Type{Precision: precision, Scale: scale}
		case 64:
			target = &arrow.Decimal64Type{Precision: precision, Scale: scale}
		case 128:
			target = &arrow.Decimal128Type{Precision: precision, Scale: scale}
		default:
			target = &arrow.Decimal256Type{Precision: precision, Scale: scale}
		}
	case "date64":
		compatible = src == nil || src.ID() == arrow.DATE32
		target = arrow.FixedWidthTypes.Date64
	case "month_interval", "day_time_interval":
		compatible = src == nil || src.ID() == arrow.INTERVAL_MONTH_DAY_NANO
		if layout == "month_interval" {
			target = arrow.FixedWidthTypes.MonthInterval
		} else {
			target = arrow.FixedWidthTypes.DayTimeInterval
		}
	default:
		switch {
		case strings.HasPrefix(layout, "fixed_size_binary:"):
			n, err := strconv.ParseInt(strings.TrimPrefix(layout, "fixed_size_binary:"), 10, 32)
			if err != nil || n < 1 || n > 16<<20 {
				return nil, fmt.Errorf("fixed_size_binary width must be 1 through 16777216 bytes")
			}
			compatible = src == nil || src.ID() == arrow.BINARY
			target = &arrow.FixedSizeBinaryType{ByteWidth: int(n)}
		case strings.HasPrefix(layout, "timestamp:"):
			zone := strings.TrimPrefix(layout, "timestamp:")
			if _, err := time.LoadLocation(zone); err != nil || zone == "" || zone == "Local" {
				return nil, fmt.Errorf("invalid explicit timestamp zone %q", zone)
			}
			unit := arrow.Microsecond
			if src != nil {
				t, ok := src.(*arrow.TimestampType)
				compatible = ok && t.TimeZone != ""
				if ok {
					unit = t.Unit
				}
			}
			target = &arrow.TimestampType{Unit: unit, TimeZone: zone}
		case strings.HasPrefix(layout, "duration:"):
			unit, ok := map[string]arrow.TimeUnit{"s": arrow.Second, "ms": arrow.Millisecond, "us": arrow.Microsecond, "ns": arrow.Nanosecond}[strings.TrimPrefix(layout, "duration:")]
			if !ok {
				return nil, fmt.Errorf("invalid duration unit in %q", layout)
			}
			compatible = src == nil || src.ID() == arrow.INTERVAL_MONTH_DAY_NANO
			target = &arrow.DurationType{Unit: unit}
		default:
			return nil, fmt.Errorf("unknown result layout %q", layout)
		}
	}
	if !compatible {
		return nil, fmt.Errorf("result layout %q is incompatible with %s", layout, src)
	}
	return target, nil
}

func (l resultLayouts) schema(src *arrow.Schema) (*arrow.Schema, error) {
	if len(l) == 0 {
		return src, nil
	}
	fields := src.Fields()
	found := map[string]bool{}
	for i, f := range fields {
		layout, ok := l[f.Name]
		if !ok {
			continue
		}
		if found[f.Name] {
			return nil, errorf(adbc.StatusInvalidArgument, "result_layouts column %q is ambiguous", f.Name)
		}
		found[f.Name] = true
		dt, err := physicalResultType(f.Type, layout)
		if err != nil {
			return nil, invalidArg(fmt.Errorf("column %q: %w", f.Name, err))
		}
		fields[i].Type = dt
	}
	for name := range l {
		if !found[name] {
			return nil, errorf(adbc.StatusInvalidArgument, "result_layouts column %q does not exist", name)
		}
	}
	md := src.Metadata()
	return arrow.NewSchema(fields, &md), nil
}

// Each transformed batch owns fresh buffers. Unchanged columns retain the
// source arrays, so advancing or releasing either reader cannot invalidate a
// batch retained by the consumer. Dictionaries are independent per batch.
type layoutReader struct {
	refs    atomic.Int64
	source  array.RecordReader
	schema  *arrow.Schema
	mem     memory.Allocator
	ctx     context.Context
	current arrow.RecordBatch
	pending arrow.RecordBatch
	offset  int64
	maxRows int64
	err     error
}

func (l resultLayouts) wrap(ctx context.Context, mem memory.Allocator, src array.RecordReader) (array.RecordReader, error) {
	if len(l) == 0 {
		return src, nil
	}
	schema, err := l.schema(src.Schema())
	if err != nil {
		src.Release()
		return nil, err
	}
	r := &layoutReader{source: src, schema: schema, mem: mem, ctx: ctx, maxRows: math.MaxInt64}
	var fixedBytes int64
	for _, f := range schema.Fields() {
		if dt, ok := f.Type.(*arrow.FixedSizeBinaryType); ok {
			fixedBytes += int64(dt.ByteWidth)
		}
	}
	if fixedBytes > 0 {
		r.maxRows = max(int64(1), (16<<20)/fixedBytes)
	}
	r.refs.Store(1)
	return r, nil
}
func (r *layoutReader) Retain() { r.refs.Add(1) }
func (r *layoutReader) Release() {
	if r.refs.Add(-1) == 0 {
		if r.current != nil {
			r.current.Release()
			r.current = nil
		}
		if r.pending != nil {
			r.pending.Release()
			r.pending = nil
		}
		r.source.Release()
	}
}
func (r *layoutReader) Schema() *arrow.Schema          { return r.schema }
func (r *layoutReader) RecordBatch() arrow.RecordBatch { return r.current }
func (r *layoutReader) Record() arrow.Record           { return r.current }
func (r *layoutReader) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.source.Err()
}
func (r *layoutReader) Next() bool {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.err != nil {
		return false
	}
	if err := cancelled(r.ctx); err != nil {
		r.err = err
		return false
	}
	if err := r.source.Err(); err != nil {
		r.err = err
		return false
	}
	if r.pending != nil && r.offset >= r.pending.NumRows() {
		r.pending.Release()
		r.pending = nil
	}
	if r.pending == nil {
		if !r.source.Next() {
			return false
		}
		r.pending = r.source.RecordBatch()
		r.pending.Retain()
		r.offset = 0
	}
	src := r.pending
	end := r.offset + min(src.NumRows()-r.offset, r.maxRows)
	columns := make([]arrow.Array, src.NumCols())
	defer func() {
		for _, a := range columns {
			if a != nil {
				a.Release()
			}
		}
	}()
	for j := range columns {
		dt := r.schema.Field(j).Type
		if arrow.TypeEqual(dt, src.Column(j).DataType()) {
			columns[j] = array.NewSlice(src.Column(j), r.offset, end)
			continue
		}
		b := array.NewBuilder(r.mem, dt)
		for i := int(r.offset); i < int(end); i++ {
			if i%1024 == 0 {
				if err := r.source.Err(); err != nil {
					r.err = err
					break
				}
				if err := cancelled(r.ctx); err != nil {
					r.err = err
					break
				}
			}
			v, err := valueAt(src.Column(j), i)
			if err == nil {
				err = appendPhysicalResult(b, dt, v)
			}
			if err != nil {
				r.err = invalidArg(fmt.Errorf("result column %q: %w", r.schema.Field(j).Name, err))
				break
			}
		}
		if r.err == nil {
			columns[j] = b.NewArray()
		}
		b.Release()
		if r.err != nil {
			return false
		}
	}
	r.current = array.NewRecordBatch(r.schema, columns, end-r.offset)
	r.offset = end
	return true
}

func appendPhysicalResult(b array.Builder, dt arrow.DataType, v Value) error {
	if v.Null {
		b.AppendNull()
		return nil
	}
	switch b := b.(type) {
	case *array.LargeStringBuilder:
		b.Append(v.S)
	case *array.StringViewBuilder:
		b.Append(v.S)
	case *array.BinaryBuilder:
		b.Append([]byte(v.S))
	case *array.BinaryViewBuilder:
		b.Append([]byte(v.S))
	case *array.FixedSizeBinaryBuilder:
		if len(v.S) != dt.(*arrow.FixedSizeBinaryType).ByteWidth {
			return fmt.Errorf("fixed-size binary value has %d bytes; requires %d", len(v.S), dt.(*arrow.FixedSizeBinaryType).ByteWidth)
		}
		b.Append([]byte(v.S))
	case *array.BinaryDictionaryBuilder:
		if err := b.AppendString(v.S); err != nil {
			return err
		}
		if b.DictionarySize() > maxResultDictionaryEntries {
			return fmt.Errorf("per-batch dictionary exceeds %d entries; reduce stream_batch_rows", maxResultDictionaryEntries)
		}
	case *array.Decimal32Builder:
		b.Append(decimal.Decimal32(v.D.Int64()))
	case *array.Decimal64Builder:
		b.Append(decimal.Decimal64(v.D.Int64()))
	case *array.Date64Builder:
		b.Append(arrow.Date64(v.I * 86400000))
	case *array.TimestampBuilder:
		b.Append(arrow.Timestamp(v.I))
	case *array.MonthIntervalBuilder:
		if v.Days != 0 || v.I != 0 {
			return fmt.Errorf("month_interval cannot represent days or nanoseconds")
		}
		b.Append(arrow.MonthInterval(v.Months))
	case *array.DayTimeIntervalBuilder:
		ms := v.I / 1000000
		if v.Months != 0 || v.I%1000000 != 0 || ms < math.MinInt32 || ms > math.MaxInt32 {
			return fmt.Errorf("day_time_interval conversion would lose information")
		}
		b.Append(arrow.DayTimeInterval{Days: v.Days, Milliseconds: int32(ms)})
	case *array.DurationBuilder:
		if v.Months != 0 || v.Days != 0 {
			return fmt.Errorf("duration cannot represent calendar months or days")
		}
		factor := nsPerSecond / unitsPerSecond[dt.(*arrow.DurationType).Unit]
		if v.I%factor != 0 {
			return fmt.Errorf("duration conversion would lose precision")
		}
		b.Append(arrow.Duration(v.I / factor))
	default:
		return appendValue(b, v.T, v)
	}
	return nil
}
