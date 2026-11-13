//go:build cgo

// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

// Package upstreamalloc isolates Arrow's C boundary from Redis and row conversion.
package upstreamalloc

/*
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/cdata"
)

// Descriptors live in zeroed C memory; no Go pointer-containing struct is handed
// to a C callback. Go benchmark allocation counters do not measure these mallocs.
type descriptors struct {
	array  *cdata.CArrowArray
	schema *cdata.CArrowSchema
}

func newDescriptors() descriptors {
	return descriptors{
		array:  (*cdata.CArrowArray)(C.calloc(1, C.size_t(unsafe.Sizeof(cdata.CArrowArray{})))),
		schema: (*cdata.CArrowSchema)(C.calloc(1, C.size_t(unsafe.Sizeof(cdata.CArrowSchema{})))),
	}
}

func (d descriptors) close() {
	cdata.ReleaseCArrowArray(d.array)
	cdata.ReleaseCArrowSchema(d.schema)
	C.free(unsafe.Pointer(d.array))
	C.free(unsafe.Pointer(d.schema))
}

func (d descriptors) roundTrip(rec arrow.RecordBatch, schemaEach bool) (arrow.RecordBatch, error) {
	// Import moves ownership out of these descriptor headers. Reusing a header
	// does not reuse live children or payload buffers owned by a prior record.
	*d.array, *d.schema = cdata.CArrowArray{}, cdata.CArrowSchema{}
	if schemaEach {
		cdata.ExportArrowRecordBatch(rec, d.array, d.schema)
		return cdata.ImportCRecordBatch(d.array, d.schema)
	}
	cdata.ExportArrowRecordBatch(rec, d.array, nil)
	return cdata.ImportCRecordBatchWithSchema(d.array, rec.Schema())
}

func roundTripStream(records []arrow.RecordBatch) (array.RecordReader, error) {
	producer, err := array.NewRecordReader(records[0].Schema(), records)
	if err != nil {
		return nil, err
	}
	return roundTripReader(producer)
}

// roundTripReader consumes the caller's original reader reference. The C export
// retains its own reference, released by the imported consumer's Release path.
func roundTripReader(producer array.RecordReader) (array.RecordReader, error) {
	stream := (*cdata.CArrowArrayStream)(C.calloc(1, C.size_t(unsafe.Sizeof(cdata.CArrowArrayStream{}))))
	cdata.ExportRecordReader(producer, stream)              // v18.8.0 retains its own reference
	producer.Release()                                      // release the constructor's original reference
	consumer, err := cdata.ImportCRecordReader(stream, nil) // moves stream ownership
	cdata.ReleaseCArrowArrayStream(stream)                  // moved header, also safe on import failure
	C.free(unsafe.Pointer(stream))
	if err != nil {
		return nil, err
	}
	// The concrete imported reader implements explicit RecordReader lifetime
	// management. Use its standard borrowed RecordBatch/Retain contract, rather
	// than relying on finalizer timing through the narrower arrio.Reader type.
	r, ok := consumer.(array.RecordReader)
	if !ok {
		if release, ok := consumer.(interface{ Release() }); ok {
			release.Release()
		}
		return nil, fmt.Errorf("imported stream does not expose explicit record-reader ownership")
	}
	return r, nil
}
