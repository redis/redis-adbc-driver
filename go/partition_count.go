// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"strconv"

	"github.com/apache/arrow-adbc/go/adbc"
)

func effectivePartitionCount(n int) int {
	if n == 0 {
		return 8
	}
	return n
}
func parsePartitionCount(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > maxPartitionCount {
		return 0, errorf(adbc.StatusInvalidArgument, "%s must be between 1 and %d", OptionIntPartitionCount, maxPartitionCount)
	}
	return n, nil
}
func (d *databaseImpl) SetOptionInt(ctx context.Context, key string, value int64) error {
	if key == OptionIntPartitionCount {
		return d.SetOption(ctx, key, strconv.FormatInt(value, 10))
	}
	return d.DatabaseImplBase.SetOptionInt(ctx, key, value)
}
func (d *databaseImpl) GetOptionInt(ctx context.Context, key string) (int64, error) {
	if key == OptionIntPartitionCount {
		return int64(effectivePartitionCount(d.partitionCount)), nil
	}
	return d.DatabaseImplBase.GetOptionInt(ctx, key)
}
