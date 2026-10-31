// Copyright (c) 2026 ADBC Drivers Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//         http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redis

import (
	"errors"
	"fmt"
	"strings"

	"github.com/apache/arrow-adbc/go/adbc"
)

// SQLSTATEs. An error the driver knows Postgres's SQLSTATE for carries it
// (adbc.Error.SqlState, AdbcError.sqlstate in C, exc.sqlstate in Python):
// the code Postgres 16 reports for the same error, which psql shows with
// \set VERBOSITY verbose. The code sets the ADBC status, as it does in
// adbc-driver-postgresql (stateStatus): a data exception (class 22) is
// INVALID_DATA, an integrity violation (class 23) INTEGRITY.
//
// While a statement runs, such an error is a sqlError (sql.go) with the
// code in state (stateErr): wrapping it with %w keeps the code, and TRY_CAST
// only checks that there is an error. Where the error leaves the code that
// runs the statement, invalidArg, wrapErr and columnErr make it an
// adbc.Error with the code and its status; stateErrorf makes one directly.
const (
	stateFeatureNotSupported = "0A000" // feature_not_supported
	stateStringTooLong       = "22001" // string_data_right_truncation
	stateNumericRange        = "22003" // numeric_value_out_of_range
	stateNullNotAllowed      = "22004" // null_value_not_allowed
	stateDatetimeFormat      = "22007" // invalid_datetime_format
	stateDatetimeRange       = "22008" // datetime_field_overflow
	stateTZDisplacement      = "22009" // invalid_time_zone_displacement_value
	stateSubstring           = "22011" // substring_error
	stateDivisionByZero      = "22012" // division_by_zero
	stateFrameOffset         = "22013" // invalid_preceding_or_following_size
	stateNtile               = "22014" // invalid_argument_for_ntile_function
	stateIntervalField       = "22015" // interval_field_overflow
	stateNthValue            = "22016" // invalid_argument_for_nth_value_function
	stateInvalidParameter    = "22023" // invalid_parameter_value
	stateEscape              = "22025" // invalid_escape_sequence
	stateEscapeUse           = "2200C" // invalid_use_of_escape_character
	stateRegex               = "2201B" // invalid_regular_expression
	stateLogarithm           = "2201E" // invalid_argument_for_logarithm
	statePower               = "2201F" // invalid_argument_for_power_function
	stateArraySubscript      = "2202E" // array_subscript_error
	stateDuplicateJSONKey    = "22030" // duplicate_json_object_key_value
	stateJSONSubscript       = "22033" // invalid_sql_json_subscript
	stateJSONArrayNotFound   = "22039" // sql_json_array_not_found
	stateJSONMemberNotFound  = "2203A" // sql_json_member_not_found
	stateJSONObjectNotFound  = "2203C" // sql_json_object_not_found
	stateInvalidText         = "22P02" // invalid_text_representation
	stateUntranslatable      = "22P05" // untranslatable_character
	stateNotNull             = "23502" // not_null_violation
	stateUnique              = "23505" // unique_violation
	stateCheck               = "23514" // check_violation
	stateUndefinedSchema     = "3F000" // invalid_schema_name
	stateSyntax              = "42601" // syntax_error
	stateDuplicateColumn     = "42701" // duplicate_column
	stateAmbiguousColumn     = "42702" // ambiguous_column
	stateUndefinedColumn     = "42703" // undefined_column
	stateGrouping            = "42803" // grouping_error
	stateDatatypeMismatch    = "42804" // datatype_mismatch
	stateCannotCoerce        = "42846" // cannot_coerce
	stateUndefinedFunction   = "42883" // undefined_function
	stateAmbiguousFunction   = "42725" // ambiguous_function
	stateUndefinedObject     = "42704" // undefined_object
	stateWrongObjectType     = "42809" // wrong_object_type
	stateDuplicateObject     = "42710" // duplicate_object
	stateUndefinedTable      = "42P01" // undefined_table
	stateUndefinedParameter  = "42P02" // undefined_parameter
	stateDuplicateSchema     = "42P06" // duplicate_schema
	stateDuplicateTable      = "42P07" // duplicate_table
	stateTableDefinition     = "42P16" // invalid_table_definition
	stateWindowing           = "42P20" // windowing_error
	stateQueryCanceled       = "57014" // query_canceled
)

// stateStatus is the ADBC status of an error with SQLSTATE state, as
// adbc-driver-postgresql classifies it (ClassifySqlState in
// c/driver/postgresql/error.cc).
func stateStatus(state string) adbc.Status {
	switch state {
	case stateQueryCanceled:
		return adbc.StatusCancelled
	case stateFeatureNotSupported:
		return adbc.StatusNotImplemented
	case "42501":
		return adbc.StatusUnauthorized
	case "42P01", "42P02", "42703", "42883", "42704", "42602", "34000", "3D000", "3F000", "26000", "58P01":
		return adbc.StatusNotFound
	case "42701", "42P03", "42P04", "42P05", "42P06", "42P07", "42712", "42723", "42710", "58P02":
		return adbc.StatusAlreadyExists
	}
	switch state[:2] {
	case "22":
		return adbc.StatusInvalidData
	case "23":
		return adbc.StatusIntegrity
	case "28":
		return adbc.StatusUnauthenticated
	case "25", "2D", "3B", "55":
		return adbc.StatusInvalidState
	case "XX":
		return adbc.StatusInternal
	case "42":
		return adbc.StatusInvalidArgument
	}
	return adbc.StatusIO
}

// stateErr is an error with SQLSTATE state, for the code that runs a
// statement: it gets its "[redis] " prefix and its status where it leaves
// that code (invalidArg).
func stateErr(state, format string, args ...any) error {
	return &sqlError{msg: fmt.Sprintf(format, args...), state: state}
}

// stateErrorf is errorf with SQLSTATE state, and the status it has
// (stateStatus).
func stateErrorf(state, format string, args ...any) error {
	return codeStateErrorf(stateStatus(state), state, format, args...)
}

// codeStateErrorf is errorf with SQLSTATE state.
func codeStateErrorf(code adbc.Status, state, format string, args ...any) error {
	e := adbc.Error{Code: code, Msg: "[redis] " + fmt.Sprintf(format, args...)}
	copy(e.SqlState[:], state)
	return e
}

// sqlState is the SQLSTATE of err: of the sqlError or adbc.Error it is or
// wraps, or "" if it has none.
func sqlState(err error) string {
	var se *sqlError
	if errors.As(err, &se) && se.state != "" {
		return se.state
	}
	var ae adbc.Error
	if errors.As(err, &ae) && ae.SqlState[0] != 0 {
		return string(ae.SqlState[:])
	}
	return ""
}

// parseErr is err, an error in a literal or a type that the parser read,
// as a sqlError that keeps its SQLSTATE.
func parseErr(err error) error {
	return &sqlError{msg: err.Error(), state: sqlState(err)}
}

// wrapErr is err as an adbc.Error, "[redis] prefix: message" (without the
// prefix if it is ""). An adbc.Error keeps its status and SQLSTATE; an
// error a done context caused is CANCELLED or TIMEOUT (ctxStatus), with
// SQLSTATE 57014 (query_canceled, which Postgres also reports for a
// statement timeout); one with a SQLSTATE gets its status (stateStatus),
// and any other gets code.
func wrapErr(code adbc.Status, err error, prefix string) error {
	if err == nil {
		return nil
	}
	var ae adbc.Error
	if errors.As(err, &ae) {
		if prefix == "" {
			return err
		}
		ae.Msg = "[redis] " + prefix + ": " + strings.TrimPrefix(ae.Msg, "[redis] ")
		return ae
	}
	msg := err.Error()
	if prefix != "" {
		msg = prefix + ": " + msg
	}
	if c, ok := ctxStatus(err); ok {
		return codeStateErrorf(c, stateQueryCanceled, "%s", msg)
	}
	if state := sqlState(err); state != "" {
		return stateErrorf(state, "%s", msg)
	}
	return errorf(code, "%s", msg)
}

// columnErr is err, the error of a value written to column name, as an
// adbc.Error: `column "c": message`, with its status and SQLSTATE
// (wrapErr). A value whose type never converts to the column's is
// datatype_mismatch (42804), as in Postgres's INSERT and UPDATE, rather
// than a cast's cannot_coerce (42846).
func columnErr(name string, err error) error {
	if sqlState(err) == stateCannotCoerce {
		err = &sqlError{msg: err.Error(), state: stateDatatypeMismatch}
	}
	return wrapErr(adbc.StatusInvalidArgument, err, fmt.Sprintf("column %q", name))
}
