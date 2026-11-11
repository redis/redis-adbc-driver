// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

//go:build driverlib

package main

// #include <stdlib.h>
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"unsafe"

	redis "github.com/adbc-drivers/redis/go"
)

// Private versioned JSON ABI for language bindings. Byte values use JSON's
// base64 encoding for []byte, preserving NULs and non-UTF8 data. Handles are
// registry IDs, never Go pointers. Callers own replies and free them once.
var nativeReaders = struct {
	sync.Mutex
	next    uint64
	readers map[uint64]*redis.NativeReader
}{readers: make(map[uint64]*redis.NativeReader)}

//export RedisNativeReadV1
func RedisNativeReadV1(input *C.char) (reply *C.char) {
	defer func() {
		if p := recover(); p != nil {
			globalPoison.Store(true)
			reply = nativeReply(nil, fmt.Errorf("native read panic: %v", p))
		}
	}()
	if globalPoison.Load() {
		return nativeReply(nil, fmt.Errorf("driver state is unavailable after an earlier panic; restart the process"))
	}
	var req struct {
		Action    string            `json:"action"`
		Handle    uint64            `json:"handle"`
		Options   map[string]string `json:"options"`
		Schema    string            `json:"schema"`
		Table     string            `json:"table"`
		RowID     int64             `json:"row_id"`
		Fields    []string          `json:"fields"`
		Retention int64             `json:"retention_ms"`
	}
	if input == nil {
		return nativeReply(nil, fmt.Errorf("missing request"))
	}
	if err := json.Unmarshal([]byte(C.GoString(input)), &req); err != nil {
		return nativeReply(nil, err)
	}
	ctx := context.Background()
	if req.Action == "open" {
		reader, err := redis.OpenNativeReader(ctx, req.Options)
		if err != nil {
			return nativeReply(nil, err)
		}
		nativeReaders.Lock()
		nativeReaders.next++
		id := nativeReaders.next
		nativeReaders.readers[id] = reader
		nativeReaders.Unlock()
		return nativeReply(id, nil)
	}
	nativeReaders.Lock()
	reader := nativeReaders.readers[req.Handle]
	if req.Action == "close" {
		delete(nativeReaders.readers, req.Handle)
	}
	nativeReaders.Unlock()
	if reader == nil {
		return nativeReply(nil, fmt.Errorf("unknown or closed native reader"))
	}
	switch req.Action {
	case "close":
		return nativeReply(nil, reader.Close())
	case "acquire":
		if req.Retention < 0 || req.Retention > 3600000 {
			return nativeReply(nil, fmt.Errorf("invalid retention_ms"))
		}
		return nativeReply(nil, reader.AcquireSnapshot(ctx, time.Duration(req.Retention)*time.Millisecond))
	case "release":
		return nativeReply(nil, reader.ReleaseSnapshot(ctx))
	case "read":
		row, exists, err := reader.ReadHash(ctx, req.Schema, req.Table, req.RowID, req.Fields)
		return nativeReply(struct {
			Row    map[string][]byte `json:"row"`
			Exists bool              `json:"exists"`
		}{row, exists}, err)
	default:
		return nativeReply(nil, fmt.Errorf("unsupported native reader action"))
	}
}

func nativeReply(result any, err error) *C.char {
	var message string
	if err != nil {
		message = err.Error()
		result = nil
	}
	data, marshalErr := json.Marshal(struct {
		Result any    `json:"result"`
		Error  string `json:"error,omitempty"`
	}{result, message})
	if marshalErr != nil {
		return C.CString(`{"error":"native reply encoding failed"}`)
	}
	return C.CString(string(data))
}

//export RedisNativeReadFreeV1
func RedisNativeReadFreeV1(reply *C.char) { C.free(unsafe.Pointer(reply)) }
