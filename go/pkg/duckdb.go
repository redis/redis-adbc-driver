// Copyright (c) 2026 ADBC Drivers Contributors
// SPDX-License-Identifier: Apache-2.0

//go:build driverlib && duckdb

package main

// #include "adbc.h"
import "C"

import redis "github.com/adbc-drivers/redis/go"

// RedisDuckDBValidateReadQuery uses the driver's own parser, before opening a
// connection or executing SQL. Only one SELECT (including SELECT CTEs and set
// operations) is accepted by the DuckDB scanner.
//
//export RedisDuckDBValidateReadQuery
func RedisDuckDBValidateReadQuery(query *C.char, err *C.struct_AdbcError) (code C.AdbcStatusCode) {
	defer func() {
		if e := recover(); e != nil {
			code = poison(err, "RedisDuckDBValidateReadQuery", e)
		}
	}()
	parsed, e := redis.ParseScript(C.GoString(query))
	if e != nil {
		setErr(err, "invalid read query: %s", e)
		return C.ADBC_STATUS_INVALID_ARGUMENT
	}
	if len(parsed) != 1 {
		setErr(err, "redis_adbc_scan requires exactly one SELECT statement")
		return C.ADBC_STATUS_INVALID_ARGUMENT
	}
	if _, ok := parsed[0].Stmt.(*redis.SelectStmt); !ok {
		setErr(err, "redis_adbc_scan requires exactly one SELECT statement")
		return C.ADBC_STATUS_INVALID_ARGUMENT
	}
	return C.ADBC_STATUS_OK
}
