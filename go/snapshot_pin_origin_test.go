// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type snapshotPinTestOrigin struct {
	test, pool, operation string
	failedReply           bool
}

var snapshotPinTestOrigins sync.Map // token -> origin; tokens never appear in diagnostics

type snapshotPinOriginHook struct {
	test, pool string
}

func (*snapshotPinOriginHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }
func (*snapshotPinOriginHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

func pinOriginHook(t *testing.T, pool string) *snapshotPinOriginHook {
	return &snapshotPinOriginHook{test: strings.SplitN(t.Name(), "/", 2)[0], pool: pool}
}

func (h *snapshotPinOriginHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		a := snapshotObservedControlArgs(cmd)
		op, token := "", ""
		if snapshotObservedControlName(cmd) == "eval" && len(a) > 3 && a[1] == snapshotControl {
			keys, err := strconv.Atoi(fmt.Sprint(a[2]))
			if err == nil && keys > 0 && 5+keys < len(a) {
				op, token = fmt.Sprint(a[3+keys]), fmt.Sprint(a[4+keys])
				if op == "retain_partitions" {
					token = fmt.Sprint(a[5+keys])
				}
			}
		}
		err := next(ctx, cmd)
		if !snapshotMetadataID(token) {
			return err
		}
		switch op {
		case "acquire", "acquire_partition", "retain_partitions":
			snapshotPinTestOrigins.Store(token, snapshotPinTestOrigin{test: h.test, pool: h.pool, operation: op, failedReply: err != nil})
		case "release", "release_partitions":
			if err == nil {
				snapshotPinTestOrigins.Delete(token)
			}
		}
		return err
	}
}
