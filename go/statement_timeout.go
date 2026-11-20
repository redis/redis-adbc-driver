// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"time"
)

type statementTimeoutKey struct{}

func statementTimeoutDefault(d *time.Duration) time.Duration {
	if d == nil {
		return defaultStatementTimeout
	}
	return *d
}

func (s *session) statementTimeout() time.Duration {
	if s == nil {
		return defaultStatementTimeout
	}
	return s.value("statement_timeout").timeout
}

func (s *statementImpl) statementTimeoutContext(ctx context.Context) context.Context {
	if s.statementTimeout != nil {
		return context.WithValue(ctx, statementTimeoutKey{}, *s.statementTimeout)
	}
	return ctx
}

// Only the operation deadline changes. Renewable leases and cleanup retain
// their independent lifetimes, and parent cancellation/deadlines always win.
func statementOperationContext(ctx context.Context, sess *session) (context.Context, context.CancelFunc) {
	d, ok := ctx.Value(statementTimeoutKey{}).(time.Duration)
	if !ok {
		d = sess.statementTimeout()
	}
	ctx = context.WithValue(ctx, statementTimeoutKey{}, d)
	if d == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}
