// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-adbc/go/adbc"
)

// ALTER acknowledges its schema change before background indexing completes.
// Do not publish the new metadata/return success while ordinary Search reads
// can still observe that incomplete backfill. The existing readiness check
// covers every OSS Cluster primary, or the Software/proxy endpoint.
func (s *store) awaitIndexReady(ctx context.Context, index string) error {
	return waitIndexReady(ctx, func(ctx context.Context) error { return s.checkRangeReadiness(ctx, index) })
}

func waitIndexReady(ctx context.Context, check func(context.Context) error) error {
	for {
		if err := cancelled(ctx); err != nil {
			return err
		}
		err := check(ctx)
		var ae adbc.Error
		if !errors.As(err, &ae) || ae.Code != adbc.StatusInvalidState {
			return err // nil or an actual capability/permission/transport failure
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return cancelled(ctx)
		case <-timer.C:
		}
	}
}

// Distributed FT.INFO compares shard schemas. Concurrent FT.ALTER propagation
// can temporarily make that comparison fail on OSS Cluster or Software.
// Match only known server replies, never similarly worded transport errors.
func inconsistentIndexState(err error) bool {
	var serverErr interface {
		error
		RedisError()
	}
	if !errors.As(err, &serverErr) {
		return false
	}
	switch serverErr.Error() {
	case "Inconsistent index state", "SEARCH_VALUE_BAD Inconsistent index state":
		return true
	}
	return false
}
