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
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
)

// stateStatus classifies SQLSTATEs as adbc-driver-postgresql does: exact
// codes first, then the class.
func TestStateStatus(t *testing.T) {
	for state, want := range map[string]adbc.Status{
		"22P02": adbc.StatusInvalidData,
		"22012": adbc.StatusInvalidData,
		"23502": adbc.StatusIntegrity,
		"23514": adbc.StatusIntegrity,
		"42P01": adbc.StatusNotFound,
		"42703": adbc.StatusNotFound,
		"42883": adbc.StatusNotFound,
		"3F000": adbc.StatusNotFound,
		"42P07": adbc.StatusAlreadyExists,
		"42701": adbc.StatusAlreadyExists,
		"42601": adbc.StatusInvalidArgument,
		"42846": adbc.StatusInvalidArgument,
		"42725": adbc.StatusInvalidArgument,
		"0A000": adbc.StatusNotImplemented,
		"57014": adbc.StatusCancelled,
		"21000": adbc.StatusIO,
	} {
		if got := stateStatus(state); got != want {
			t.Errorf("stateStatus(%s) = %v, want %v", state, got, want)
		}
	}
}

// The SQLSTATE survives wrapping (with %w), the parser, and the boundaries
// that make the error an adbc.Error.
func TestSQLStateKept(t *testing.T) {
	inner := stateErr(stateInvalidText, "invalid decimal %q", "x")
	for _, c := range []struct {
		name  string
		err   error
		code  adbc.Status
		state string
		msg   string
	}{
		{"invalidArg", invalidArg(inner), adbc.StatusInvalidData, "22P02", `[redis] invalid decimal "x"`},
		{"wrapped", invalidArg(fmt.Errorf("ABS: %w", inner)), adbc.StatusInvalidData, "22P02", `[redis] ABS: invalid decimal "x"`},
		{"parser", invalidArg(parseErr(inner)), adbc.StatusInvalidData, "22P02", `[redis] invalid decimal "x"`},
		{"column", columnErr("n", inner), adbc.StatusInvalidData, "22P02", `[redis] column "n": invalid decimal "x"`},
		{"column of an adbc.Error", columnErr("v", stateErrorf(stateStringTooLong, "value too long for type character varying(3)")),
			adbc.StatusInvalidData, "22001", `[redis] column "v": value too long for type character varying(3)`},
		// Written to a column, a type that never converts is datatype_mismatch.
		{"column of cannot_coerce", columnErr("b", stateErr(stateCannotCoerce, "cannot convert date value")),
			adbc.StatusInvalidArgument, "42804", `[redis] column "b": cannot convert date value`},
		{"no SQLSTATE", invalidArg(errors.New("unsupported")), adbc.StatusInvalidArgument, "", "[redis] unsupported"},
	} {
		var ae adbc.Error
		if !errors.As(c.err, &ae) || ae.Code != c.code || sqlState(c.err) != c.state || ae.Msg != c.msg {
			t.Errorf("%s: got %#v, want %v (%s) %q", c.name, c.err, c.code, c.state, c.msg)
		}
	}
	// Conversions return a plain error with the SQLSTATE (TRY_CAST only
	// checks that there is one).
	if _, err := Coerce(stringValue("x"), typeInt32); sqlState(err) != stateInvalidText {
		t.Errorf("Coerce('x', INTEGER): %v (%s), want 22P02", err, sqlState(err))
	}
}
