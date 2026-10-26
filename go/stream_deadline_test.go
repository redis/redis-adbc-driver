// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0

package redis

import (
	"context"
	"github.com/apache/arrow-adbc/go/adbc"
	"testing"
	"time"
)

func TestStreamPageDeadlinePreservesTimeout(t *testing.T) {
	ctx, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	reader := &streamReader{ctx: ctx, cancel: func(error) { stop() }, it: &scanIter{}}
	reader.fail(context.DeadlineExceeded)
	if !isStatus(reader.err, adbc.StatusTimeout) || sqlState(reader.err) != stateQueryCanceled || !reader.closed {
		t.Fatalf("page deadline status/lifecycle: %v, closed=%t", reader.err, reader.closed)
	}
}
