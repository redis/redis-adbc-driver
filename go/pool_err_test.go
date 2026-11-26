// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// A worker pool runs under context.WithCancelCause of the statement's
// context. Once the statement's deadline passes, the pool's cause is the
// bare context.DeadlineExceeded, which reached the C boundary as UNKNOWN
// without SQLSTATE 57014 (TPC-DS SF1 Q75 on Flex, #696).
func TestPoolErrMapsStatementDeadline(t *testing.T) {
	parent, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	pool, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	<-pool.Done()
	if cause := context.Cause(pool); cause != context.DeadlineExceeded {
		t.Fatalf("precondition: pool cause = %v", cause)
	}
	err := poolErr(parent, pool)
	if !isStatus(err, adbc.StatusTimeout) || sqlState(err) != stateQueryCanceled {
		t.Fatalf("deadline: got %v (status timeout=%t, SQLSTATE %q)", err, isStatus(err, adbc.StatusTimeout), sqlState(err))
	}
	var ae adbc.Error
	if !errors.As(err, &ae) {
		t.Fatalf("deadline: %T is not an adbc.Error", err)
	}
}

func TestPoolErrCases(t *testing.T) {
	t.Run("not done", func(t *testing.T) {
		pool, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		if err := poolErr(context.Background(), pool); err != nil {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("statement cancelled", func(t *testing.T) {
		parent, stop := context.WithCancel(context.Background())
		pool, cancel := context.WithCancelCause(parent)
		defer cancel(nil)
		stop()
		err := poolErr(parent, pool)
		if !isStatus(err, adbc.StatusCancelled) || sqlState(err) != stateQueryCanceled {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("worker error wins", func(t *testing.T) {
		parent, stop := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
		defer stop()
		pool, cancel := context.WithCancelCause(parent)
		defer cancel(nil)
		workerErr := errorf(adbc.StatusIO, "worker failed")
		cancel(workerErr)
		stop() // the statement ending afterwards does not replace the first cause
		if err := poolErr(parent, pool); !isStatus(err, adbc.StatusIO) || err.Error() != workerErr.Error() {
			t.Fatalf("got %v, want the worker's error", err)
		}
	})
	t.Run("statement deadline decides over a pool cancel", func(t *testing.T) {
		// A worker that saw a bare context.Canceled cancels the pool with it
		// (the pool's cause and Err are then Canceled). When the statement's
		// own deadline has passed by the time the workers return, the
		// statement timed out: the old snowflake code checked the statement's
		// context here, not the pool's.
		parent, stop := context.WithDeadline(context.Background(), time.Now().Add(20*time.Millisecond))
		defer stop()
		pool, cancel := context.WithCancelCause(parent)
		cancel(context.Canceled)
		<-parent.Done()
		if err := poolErr(parent, pool); !isStatus(err, adbc.StatusTimeout) || sqlState(err) != stateQueryCanceled {
			t.Fatalf("got %v, want the statement's TIMEOUT", err)
		}
	})
	t.Run("internal stop sentinel kept", func(t *testing.T) {
		pool, cancel := context.WithCancelCause(context.Background())
		cancel(errStarProbeStopped)
		if err := poolErr(context.Background(), pool); !errors.Is(err, errStarProbeStopped) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("closeWith cause kept", func(t *testing.T) {
		closeErr := errorf(adbc.StatusInvalidState, "connection closed")
		parent, stop := context.WithCancelCause(context.Background())
		pool, cancel := context.WithCancelCause(parent)
		defer cancel(nil)
		stop(closeErr)
		if err := poolErr(parent, pool); !isStatus(err, adbc.StatusInvalidState) {
			t.Fatalf("got %v, want the close cause", err)
		}
	})
}
