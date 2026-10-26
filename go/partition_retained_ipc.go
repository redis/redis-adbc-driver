// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"encoding/binary"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Retained chunks are deliberately a restricted IPC stream: uncompressed SQL
// output, one schema, one record and EOS. Preflight runs before the general
// Arrow decoder; that decoder's allocator cannot bound Go metadata allocations
// or stop compressed buffers from declaring enormous uncompressed lengths.
const (
	retainedIPCFields   = 4096
	retainedIPCRows     = 1 << 20
	retainedIPCElements = 16 << 20
	retainedIPCMetadata = 1 << 20
)

type retainedFB struct {
	data         []byte
	pos, vt, end int
}

func retainedIPCBad() { panic("invalid retained IPC") }
func retainedSpan(b []byte, p, n int) []byte {
	if p < 0 || n < 0 || p > len(b) || n > len(b)-p {
		retainedIPCBad()
	}
	return b[p : p+n]
}
func retainedU32(b []byte, p int) int { return int(binary.LittleEndian.Uint32(retainedSpan(b, p, 4))) }
func retainedIndirect(b []byte, p int) int {
	n := retainedU32(b, p)
	if n < 4 || p > len(b)-n {
		retainedIPCBad()
	}
	return p + n
}
func retainedTable(b []byte, p int) retainedFB {
	offset := int(int32(binary.LittleEndian.Uint32(retainedSpan(b, p, 4))))
	vt := p - offset
	h := retainedSpan(b, vt, 4)
	n := int(binary.LittleEndian.Uint16(h))
	size := int(binary.LittleEndian.Uint16(h[2:]))
	if n < 4 || n > 64 || n%2 != 0 || size < 4 {
		retainedIPCBad()
	}
	retainedSpan(b, vt, n)
	retainedSpan(b, p, size)
	return retainedFB{b, p, vt, p + size}
}
func (t retainedFB) field(slot, size int) int {
	h := retainedSpan(t.data, t.vt, 4)
	if 4+slot*2+2 > int(binary.LittleEndian.Uint16(h)) {
		return 0
	}
	off := int(binary.LittleEndian.Uint16(t.data[t.vt+4+slot*2:]))
	if off == 0 {
		return 0
	}
	p := t.pos + off
	if off < 4 || p > t.end-size {
		retainedIPCBad()
	}
	return p
}
func (t retainedFB) scalar(slot, size int, def uint64) uint64 {
	p := t.field(slot, size)
	if p == 0 {
		return def
	}
	b := retainedSpan(t.data, p, size)
	switch size {
	case 1:
		return uint64(b[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(b))
	case 4:
		return uint64(binary.LittleEndian.Uint32(b))
	case 8:
		return binary.LittleEndian.Uint64(b)
	}
	retainedIPCBad()
	return 0
}
func (t retainedFB) table(slot int) retainedFB {
	p := t.field(slot, 4)
	if p == 0 {
		retainedIPCBad()
	}
	return retainedTable(t.data, retainedIndirect(t.data, p))
}
func (t retainedFB) vector(slot, width, limit int) (int, int) {
	p := t.field(slot, 4)
	if p == 0 {
		return 0, 0
	}
	p = retainedIndirect(t.data, p)
	n := retainedU32(t.data, p)
	if n > limit {
		retainedIPCBad()
	}
	retainedSpan(t.data, p+4, n*width)
	return p + 4, n
}

type retainedIPCShape struct {
	fields, buffers, text, items int
	nulls                        []bool
}

func (s *retainedIPCShape) textAt(t retainedFB, slot int) string {
	p, n := t.vector(slot, 1, retainedIPCMetadata)
	s.text += n
	if s.text > retainedIPCMetadata {
		retainedIPCBad()
	}
	b := retainedSpan(t.data, p, n)
	if !utf8.Valid(b) {
		retainedIPCBad()
	}
	return string(b)
}
func (s *retainedIPCShape) metadata(t retainedFB, slot int) {
	p, n := t.vector(slot, 4, retainedIPCFields)
	s.items += n
	if s.items > retainedIPCFields {
		retainedIPCBad()
	}
	for i := 0; i < n; i++ {
		kv := retainedTable(t.data, retainedIndirect(t.data, p+4*i))
		key := s.textAt(kv, 0)
		s.textAt(kv, 1)
		if strings.HasPrefix(key, "ARROW:extension:") {
			retainedIPCBad()
		}
	}
}
func (s *retainedIPCShape) field(t retainedFB, depth int) {
	s.fields++
	if depth > 32 || s.fields > retainedIPCFields {
		retainedIPCBad()
	}
	s.textAt(t, 0)
	t.scalar(1, 1, 0)
	kind := t.scalar(2, 1, 0)
	s.nulls = append(s.nulls, kind == 1)
	typ := t.table(3)
	if t.field(4, 4) != 0 {
		retainedIPCBad()
	} // dictionaries are never produced
	p, n := t.vector(5, 4, retainedIPCFields)
	switch kind {
	case 1: // Null
		if n != 0 {
			retainedIPCBad()
		}
	case 2: // Int
		width := typ.scalar(0, 4, 0)
		signed := typ.scalar(1, 1, 0)
		if (width != 8 && width != 16 && width != 32 && width != 64) || signed > 1 || n != 0 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 3: // FloatingPoint
		if typ.scalar(0, 2, 0) > 2 || n != 0 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 4, 5: // Binary / Utf8
		if n != 0 {
			retainedIPCBad()
		}
		s.buffers += 3
	case 6: // Bool
		if n != 0 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 7: // Decimal
		precision := typ.scalar(0, 4, 0)
		typ.scalar(1, 4, 0)
		width := typ.scalar(2, 4, 128)
		if n != 0 || precision == 0 || (width != 128 && width != 256) || precision > 76 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 8, 11: // Date / Interval
		unit := typ.scalar(0, 2, 0)
		if n != 0 || unit > 2 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 9: // Time
		unit := typ.scalar(0, 2, 1)
		width := typ.scalar(1, 4, 32)
		if n != 0 || unit > 3 || (unit < 2 && width != 32) || (unit >= 2 && width != 64) {
			retainedIPCBad()
		}
		s.buffers += 2
	case 10: // Timestamp
		if n != 0 || typ.scalar(0, 2, 0) > 3 {
			retainedIPCBad()
		}
		s.textAt(typ, 1)
		s.buffers += 2
	case 12: // List
		if n != 1 {
			retainedIPCBad()
		}
		s.buffers += 2
	case 13: // Struct
		s.buffers++
	default:
		retainedIPCBad()
	}
	s.metadata(t, 6)
	for i := 0; i < n; i++ {
		s.field(retainedTable(t.data, retainedIndirect(t.data, p+4*i)), depth+1)
	}
}

type retainedIPCPart struct{ meta, body []byte }
type retainedIPCFrame struct {
	parts  [2]retainedIPCPart
	schema string
	rows   int64
}

func preflightRetainedIPC(data []byte) (frame retainedIPCFrame, err error) {
	defer func() {
		if recover() != nil {
			frame = retainedIPCFrame{}
			err = errorf(adbc.StatusInvalidData, "invalid or excessive retained Arrow IPC")
		}
	}()
	if len(data) == 0 || len(data) > 16<<20 {
		retainedIPCBad()
	}
	offset := 0
	shape := retainedIPCShape{}
	for index := 0; index < 2; index++ {
		if retainedU32(data, offset) != int(^uint32(0)) {
			retainedIPCBad()
		}
		size := retainedU32(data, offset+4)
		if size < 8 || size > retainedIPCMetadata || size%8 != 0 {
			retainedIPCBad()
		}
		meta := retainedSpan(data, offset+8, size)
		msg := retainedTable(meta, retainedU32(meta, 0))
		if msg.scalar(0, 2, 0) != 4 {
			retainedIPCBad()
		} // MetadataV5
		kind := msg.scalar(1, 1, 0)
		header := msg.table(2)
		bodySize := int64(msg.scalar(3, 8, 0))
		if bodySize < 0 || bodySize > int64(len(data)-offset-8-size) || bodySize%8 != 0 {
			retainedIPCBad()
		}
		shape.metadata(msg, 4)
		if index == 0 {
			if kind != 1 || bodySize != 0 || header.scalar(0, 2, 0) != 0 {
				retainedIPCBad()
			}
			shape.metadata(header, 2)
			_, features := header.vector(3, 8, 16)
			if features != 0 {
				retainedIPCBad()
			}
			p, n := header.vector(1, 4, retainedIPCFields)
			for i := 0; i < n; i++ {
				shape.field(retainedTable(meta, retainedIndirect(meta, p+4*i)), 0)
			}
			frame.schema = retainedSHA(data[:offset+8+size])
		} else {
			if kind != 3 || header.field(3, 4) != 0 {
				retainedIPCBad()
			} // no compression
			_, variadic := header.vector(4, 8, retainedIPCFields)
			if variadic != 0 {
				retainedIPCBad()
			}
			rows := int64(header.scalar(0, 8, 0))
			if rows < 0 || rows > retainedIPCRows {
				retainedIPCBad()
			}
			frame.rows = rows
			p, n := header.vector(1, 16, retainedIPCFields)
			if n != shape.fields {
				retainedIPCBad()
			}
			var elements int64
			for i := 0; i < n; i++ {
				length := int64(binary.LittleEndian.Uint64(meta[p+16*i:]))
				nulls := int64(binary.LittleEndian.Uint64(meta[p+16*i+8:]))
				if length < 0 || length > retainedIPCRows || nulls < 0 || nulls > length || shape.nulls[i] && nulls != length {
					retainedIPCBad()
				}
				elements += length
				if elements > retainedIPCElements {
					retainedIPCBad()
				}
			}
			p, n = header.vector(2, 16, retainedIPCFields*3)
			if n != shape.buffers {
				retainedIPCBad()
			}
			for i := 0; i < n; i++ {
				start := int64(binary.LittleEndian.Uint64(meta[p+16*i:]))
				length := int64(binary.LittleEndian.Uint64(meta[p+16*i+8:]))
				if start < 0 || start > bodySize || length < 0 || length > bodySize-start {
					retainedIPCBad()
				}
			}
		}
		offset += 8 + size
		frame.parts[index] = retainedIPCPart{meta, retainedSpan(data, offset, int(bodySize))}
		offset += int(bodySize)
	}
	eos := retainedSpan(data, offset, 8)
	if len(data) != offset+8 || binary.LittleEndian.Uint32(eos) != ^uint32(0) || binary.LittleEndian.Uint32(eos[4:]) != 0 {
		retainedIPCBad()
	}
	return frame, nil
}

// Message buffers alias the already bounded and checksummed chunk bytes. Arrow
// takes its own references to these buffers; no encoded lengths allocate body
// memory and compression was rejected before its decoder can run.
type retainedMessages struct {
	parts   [2]retainedIPCPart
	index   int
	current *ipc.Message
}

func (r *retainedMessages) Message() (*ipc.Message, error) {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	if r.index == len(r.parts) {
		return nil, io.EOF
	}
	p := r.parts[r.index]
	r.index++
	meta, body := memory.NewBufferBytes(p.meta), memory.NewBufferBytes(p.body)
	r.current = ipc.NewMessage(meta, body)
	meta.Release()
	body.Release()
	return r.current, nil
}
func (r *retainedMessages) Retain() {}
func (r *retainedMessages) Release() {
	if r.current != nil {
		r.current.Release()
		r.current = nil
	}
	r.parts = [2]retainedIPCPart{}
}

func decodeRetainedIPC(data []byte, schema string, rows int64) (record arrow.RecordBatch, err error) {
	defer func() {
		if recover() != nil {
			if record != nil {
				record.Release()
				record = nil
			}
			err = errorf(adbc.StatusInvalidData, "invalid retained Arrow record")
		}
	}()
	frame, err := preflightRetainedIPC(data)
	if err != nil {
		return nil, err
	}
	if frame.schema != schema || frame.rows != rows {
		return nil, errorf(adbc.StatusInvalidData, "retained Arrow schema or row count mismatch")
	}
	reader, err := ipc.NewReaderFromMessageReader(&retainedMessages{parts: frame.parts}, ipc.WithEnsureNativeEndian(false))
	if err != nil {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained Arrow schema")
	}
	defer reader.Release()
	if !reader.Next() || reader.RecordBatch().NumRows() != rows {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained Arrow record")
	}
	candidate := reader.RecordBatch()
	for _, col := range candidate.Columns() {
		retainedCanonicalNullBuffers(col.Data())
	}
	if err := array.ValidateRecordFull(candidate); err != nil {
		return nil, errorf(adbc.StatusInvalidData, "invalid retained Arrow array data")
	}
	candidate.Retain()
	record = candidate
	if reader.Next() || reader.Err() != nil {
		record.Release()
		record = nil
		return nil, errorf(adbc.StatusInvalidData, "invalid retained Arrow stream ending")
	}
	return record, nil
}

// Arrow Go's IPC Null decoder supplies zero buffers; its full validator expects
// the single nil placeholder used by array.NewNull. Normalize only that private
// decoded data before validation, including nested NULL output. Null counts are
// checked in preflight before Arrow's Null constructor normalizes them.
func retainedCanonicalNullBuffers(data arrow.ArrayData) {
	if data.DataType().ID() == arrow.NULL && len(data.Buffers()) == 0 {
		data.(*array.Data).Reset(data.DataType(), data.Len(), []*memory.Buffer{nil}, nil, data.NullN(), data.Offset())
	}
	for _, child := range data.Children() {
		retainedCanonicalNullBuffers(child)
	}
}
