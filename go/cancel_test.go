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

// Tests of the errors a cancelled statement fails with (cancel.go) that need
// no Redis server.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

func TestCancelled(t *testing.T) {
	live, stop := context.WithCancel(context.Background())
	defer stop()
	done, cancel := context.WithCancel(context.Background())
	cancel()
	past, cancelPast := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelPast()
	closed, closeWith := context.WithCancelCause(context.Background())
	closeWith(errorf(adbc.StatusInvalidState, "the connection was closed while its result was being read"))

	for _, c := range []struct {
		name  string
		ctx   context.Context
		code  adbc.Status // 0: no error
		msg   string
		state string // the SQLSTATE: query_canceled, as Postgres reports a cancel and a statement timeout
	}{
		{"nil", nil, 0, "", ""},
		{"live", live, 0, "", ""},
		{"cancelled", done, adbc.StatusCancelled, "[redis] the query was cancelled", "57014"},
		{"past its deadline", past, adbc.StatusTimeout, "[redis] the query timed out", "57014"},
		{"closed stream", closed, adbc.StatusInvalidState, "[redis] the connection was closed while its result was being read", ""},
	} {
		err := cancelled(c.ctx)
		if c.code == 0 {
			if err != nil {
				t.Errorf("%s: got %v, want nil", c.name, err)
			}
			continue
		}
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != c.code || ae.Msg != c.msg || sqlState(err) != c.state {
			t.Errorf("%s: got %#v, want %v %q (%s)", c.name, err, c.code, c.msg, c.state)
		}
	}
}

// wrapRedis and invalidArg report an error caused by a done context
// (go-redis returns ctx.Err()) as CANCELLED or TIMEOUT, not IO or
// INVALID_ARGUMENT.
func TestCancelStatus(t *testing.T) {
	socketTimeout := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}
	for _, c := range []struct {
		err     error
		redis   adbc.Status // wrapRedis
		arg     adbc.Status // invalidArg
		message string      // wrapRedis's
		state   string      // the SQLSTATE of both
	}{
		{context.Canceled, adbc.StatusCancelled, adbc.StatusCancelled, "[redis] failed to write rows: context canceled", "57014"},
		{fmt.Errorf("pipeline: %w", context.Canceled), adbc.StatusCancelled, adbc.StatusCancelled, "[redis] failed to write rows: pipeline: context canceled", "57014"},
		{context.DeadlineExceeded, adbc.StatusTimeout, adbc.StatusTimeout, "[redis] failed to write rows: context deadline exceeded", "57014"},
		// A socket timeout stays IO (see timeout.go).
		{socketTimeout, adbc.StatusIO, adbc.StatusInvalidArgument, "[redis] failed to write rows: read tcp: i/o timeout", ""},
		{errors.New("ERR unknown command"), adbc.StatusIO, adbc.StatusInvalidArgument, "[redis] failed to write rows: ERR unknown command", ""},
		// An ADBC error is kept.
		{stateErrorf(stateQueryCanceled, "the rename was cancelled"), adbc.StatusCancelled, adbc.StatusCancelled, "[redis] the rename was cancelled", "57014"},
		// A value that doesn't convert has its SQLSTATE's status.
		{stateErr(stateInvalidText, "invalid number %q", "x"), adbc.StatusInvalidData, adbc.StatusInvalidData, `[redis] failed to write rows: invalid number "x"`, "22P02"},
	} {
		var ae adbc.Error
		if err := wrapRedis(c.err, "failed to write rows"); !errors.As(err, &ae) || ae.Code != c.redis || ae.Msg != c.message || sqlState(err) != c.state {
			t.Errorf("wrapRedis(%v) = %#v, want %v %q (%s)", c.err, err, c.redis, c.message, c.state)
		}
		if err := invalidArg(c.err); !errors.As(err, &ae) || ae.Code != c.arg || sqlState(err) != c.state {
			t.Errorf("invalidArg(%v) = %#v, want %v (%s)", c.err, err, c.arg, c.state)
		}
	}
}
