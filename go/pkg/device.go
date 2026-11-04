//go:build driverlib

// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

package main

/*
#include "device.h"
#include "../internal/cuda/runtime.h"
#include <stdlib.h>
#include <errno.h>
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/apache/arrow-adbc/go/adbc"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/cdata"
)

func deviceError(message *C.char, code C.int) error {
	status := adbc.StatusIO
	if code == C.ENOTSUP {
		status = adbc.StatusNotImplemented
	}
	if code == C.EINVAL || code == C.EOVERFLOW {
		status = adbc.StatusInvalidArgument
	}
	return adbc.Error{Code: status, Msg: fmt.Sprintf("Arrow C Device: %s", C.GoString(message))}
}

//export RedisStatementExecuteDevice
func RedisStatementExecuteDevice(stmt *C.struct_AdbcStatement, device C.int64_t, out *C.struct_ArrowDeviceArrayStream, affected *C.int64_t, err *C.struct_AdbcError) (code C.AdbcStatusCode) {
	defer func() {
		if e := recover(); e != nil {
			code = poison(err, "RedisStatementExecuteDevice", e)
		}
	}()
	st := checkStmtInit(stmt, err, "RedisStatementExecuteDevice")
	if st == nil {
		return C.ADBC_STATUS_INVALID_STATE
	}
	if out == nil || out.release != nil || device < 0 || device > 65535 {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusInvalidArgument, Msg: "device output must be zero initialized; CUDA device ID must be 0 through 65535"}))
	}
	var message [256]C.char
	ctx := st.executionContext.NewContext()
	defer st.executionContext.FinishContext(ctx)
	if rc := C.redis_cuda_probe(C.int(device), &message[0], 256); rc != 0 {
		return C.AdbcStatusCode(errToAdbcErr(err, deviceError(&message[0], rc)))
	}
	if ctx.Err() != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusCancelled, Msg: "device execution cancelled during CUDA initialization"}))
	}
	var cpu C.struct_ArrowArrayStream
	reader, n, e := st.stmt.ExecuteQuery(ctx)
	if e != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, e))
	}
	defer reader.Release()
	if affected != nil {
		*affected = C.int64_t(n)
	}
	exportRecordReader(reader, &cpu)
	if rc := C.RedisDeviceStreamInit(&cpu, device, out, &message[0], 256); rc != 0 {
		// The CPU stream still owns its reader on initialization failure.
		C.RedisReleaseCPUStream(&cpu)
		return C.AdbcStatusCode(errToAdbcErr(err, deviceError(&message[0], rc)))
	}
	return C.ADBC_STATUS_OK
}

//export RedisStatementBindDevice
func RedisStatementBindDevice(stmt *C.struct_AdbcStatement, input *C.struct_ArrowDeviceArray, schema *C.struct_ArrowSchema, err *C.struct_AdbcError) (code C.AdbcStatusCode) {
	defer func() {
		if e := recover(); e != nil {
			code = poison(err, "RedisStatementBindDevice", e)
		}
	}()
	st := checkStmtInit(stmt, err, "RedisStatementBindDevice")
	if st == nil {
		return C.ADBC_STATUS_INVALID_STATE
	}
	if input == nil || schema == nil {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusInvalidArgument, Msg: "device array and schema are required"}))
	}
	var host C.struct_ArrowArray
	var message [256]C.char
	var view C.struct_ArrowSchema
	if rc := C.RedisDeviceSchemaBorrow(schema, &view, &message[0], 256); rc != 0 {
		return C.AdbcStatusCode(errToAdbcErr(err, deviceError(&message[0], rc)))
	}
	defer C.RedisReleaseBorrowedSchema(&view)
	ctx := st.executionContext.NewContext()
	defer st.executionContext.FinishContext(ctx)
	borrowed := *input
	if borrowed.device_type == C.ARROW_DEVICE_CUDA && borrowed.sync_event != nil {
		if borrowed.device_id < 0 || borrowed.device_id > 65535 {
			return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusInvalidArgument, Msg: "invalid CUDA device ID"}))
		}
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusCancelled, Msg: "device bind cancelled while synchronizing producer event"}))
			}
			rc := C.redis_cuda_event_ready(C.int(borrowed.device_id), borrowed.sync_event, &message[0], 256)
			if rc == 0 {
				break
			}
			if rc != C.EAGAIN {
				return C.AdbcStatusCode(errToAdbcErr(err, deviceError(&message[0], rc)))
			}
			select {
			case <-ctx.Done():
			case <-ticker.C:
			}
		}
		borrowed.sync_event = nil
	}
	if ctx.Err() != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusCancelled, Msg: "device bind cancelled"}))
	}
	if rc := C.RedisDeviceArrayToHost(&borrowed, &view, &host, &message[0], 256); rc != 0 {
		return C.AdbcStatusCode(errToAdbcErr(err, deviceError(&message[0], rc)))
	}
	defer C.RedisReleaseHostArray(&host)
	if ctx.Err() != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusCancelled, Msg: "device bind cancelled during staging"}))
	}
	field, e := cdata.ImportCArrowField((*cdata.CArrowSchema)(unsafe.Pointer(&view)))
	if e != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, e))
	}
	typ, ok := field.Type.(*arrow.StructType)
	if !ok {
		return C.AdbcStatusCode(errToAdbcErr(err, adbc.Error{Code: adbc.StatusInvalidArgument, Msg: "BindDevice requires a record batch struct schema"}))
	}
	record, e := cdata.ImportCRecordBatchWithSchema((*cdata.CArrowArray)(unsafe.Pointer(&host)), arrow.NewSchema(typ.Fields(), &field.Metadata))
	if e != nil {
		return C.AdbcStatusCode(errToAdbcErr(err, e))
	}
	defer record.Release()
	return C.AdbcStatusCode(errToAdbcErr(err, st.stmt.Bind(ctx, record)))
}
