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
	"context"
	"errors"

	"github.com/apache/arrow-adbc/go/adbc"
)

// Cancelling a statement (AdbcStatementCancel) cancels the context its
// execution runs with. go-redis notices at the statement's next command;
// the work the driver does in memory (filtering, joins, aggregates, window
// functions, sorts, generate_series) checks the context in its row loops,
// with cancelled, at every row. That costs one atomic load (cancelCtx.Err)
// per row, next to the map a row is. A sort under way can't be stopped:
// the loops check before and after it.

// cancelEvery is how many values the generate_series loops make between
// checks of the context, as they do little else.
const cancelEvery = 4096

// cancelled returns nil until ctx is done, then the error the statement
// fails with: CANCELLED, or TIMEOUT once a deadline the Go caller gave it
// has passed. A stream closed with an error (closeWith) fails with that.
// A nil ctx (an expression evaluated outside a statement) is never done.
func cancelled(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	var ae adbc.Error
	if errors.As(context.Cause(ctx), &ae) {
		return ae
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return codeStateErrorf(adbc.StatusTimeout, stateQueryCanceled, "the query timed out")
	}
	return stateErrorf(stateQueryCanceled, "the query was cancelled")
}

// ctxStatus is the status of an error caused by a done context, as go-redis
// returns one: CANCELLED or TIMEOUT. A socket timeout isn't one
// (os.ErrDeadlineExceeded): it stays an IO error (see timeout.go).
func ctxStatus(err error) (adbc.Status, bool) {
	switch {
	case errors.Is(err, context.Canceled):
		return adbc.StatusCancelled, true
	case errors.Is(err, context.DeadlineExceeded):
		return adbc.StatusTimeout, true
	}
	return 0, false
}
