// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"bytes"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func TestUnmanagedArrowInputActualNUL(t *testing.T) {
	value := string([]byte{'a', 0, 'b'}) + "雪"
	if !bytes.Contains([]byte(value), []byte{0}) {
		t.Fatal("fixture contains no zero byte")
	}
	it, cols, raw := scopedInputProjection(64, true)
	raw["c1"] = value
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	r := &streamReader{it: it, cols: cols, zone: utcZone, batchRows: 1, left: -1,
		arrow: &scanArrow{scratch: make([]Value, 64), builder: array.NewRecordBuilder(mem, resultSchema(cols))}}
	defer r.arrow.release()
	if err := r.appendArrow(raw); err != nil {
		t.Fatal(err)
	}
	if len(r.arrow.input) != 64 || r.arrow.input[1] == nil {
		t.Fatal("eligible unmanaged string binding absent")
	}
	retained := r.arrow.ready[0]
	retained.Retain()
	defer retained.Release()
	raw["c1"] = "later input"
	if err := r.appendArrow(raw); err != nil {
		t.Fatal(err)
	}
	r.arrow.release()
	got := retained.Column(1).(*array.String).Value(0)
	if got != value || !bytes.Contains([]byte(got), []byte{0}) {
		t.Fatal("retained zero byte/string changed")
	}
}

func TestUnmanagedArrowInputActualNULSoftware(t *testing.T) {
	value := string([]byte{'a', 0, 'b'}) + "雪"
	if !bytes.Contains([]byte(value), []byte{0}) {
		t.Fatal("fixture contains no zero byte")
	}
	h, query, _ := unmanagedWideProjectionFixture(t, 257)
	raw := h.rawClient()
	if err := raw.HSet(h.ctx, unmanagedWideProjectionKey(1), "c104", value).Err(); err != nil {
		t.Fatal(err)
	}
	_, reader, count := h.openStmtStream(h.ctx, h.conn, query, nil, map[string]string{OptionIntStreamBatchRows: "1"})
	released := false
	defer func() {
		if !released {
			reader.Release()
		}
	}()
	if count != -1 || !reader.Next() {
		t.Fatal("expected streaming wide source", count, reader.Err())
	}
	stream, ok := inspectionReader(reader).(*streamReader)
	if !ok || stream.it == nil || stream.it.meta == nil || !stream.it.meta.Unmanaged || stream.arrow == nil || len(stream.arrow.input) != 105 || stream.arrow.input[104] == nil {
		t.Fatal("eligible unmanaged string binding absent")
	}
	var retainedString *array.String
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	seen := make([]bool, 257)
	for {
		rec := reader.RecordBatch()
		ids := rec.Column(0).(*array.Int64)
		for row := 0; row < int(rec.NumRows()); row++ {
			id := ids.Value(row)
			if id < 1 || id > 257 || seen[id-1] {
				t.Fatal("invalid/duplicate source ID", id)
			}
			seen[id-1] = true
			if id == 1 {
				if rec.NumRows() != 1 {
					t.Fatal("unexpected one-row record")
				}
				rec.Retain()
				release = rec.Release
				retainedString = rec.Column(104).(*array.String)
				if retainedString.Value(0) != value {
					t.Fatal("zero-byte source value changed")
				}
			}
		}
		if !reader.Next() {
			break
		}
	}
	if reader.Err() != nil {
		t.Fatal(reader.Err())
	}
	for _, yes := range seen {
		if !yes {
			t.Fatal("incomplete source rows")
		}
	}
	reader.Release()
	released = true
	if retainedString == nil || retainedString.Value(0) != value || !bytes.Contains([]byte(retainedString.Value(0)), []byte{0}) {
		t.Fatal("retained zero byte/string changed after release")
	}
	if got := raw.HGet(h.ctx, unmanagedWideProjectionKey(1), "c104").Val(); got != value {
		t.Fatal("read modified application source")
	}
}
