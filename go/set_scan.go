// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"context"
	goredis "github.com/redis/go-redis/v9"
)

type setScanner interface {
	SScan(context.Context, string, uint64, string, int64) *goredis.ScanCmd
}

// Metadata is read under its existing catalog pin or WATCH. SSCAN may repeat
// members, so keep set semantics while yielding between cursor pages. Cursors
// never outlive this call and a failed page never returns a partial inventory.
func scanSet(ctx context.Context, client setScanner, key string) *goredis.StringSliceCmd {
	result := goredis.NewStringSliceCmd(ctx)
	seen := make(map[string]bool)
	var members []string
	var cursor uint64
	for {
		if err := ctx.Err(); err != nil {
			result.SetErr(err)
			return result
		}
		page, next, err := client.SScan(ctx, key, cursor, "", 256).Result()
		if err != nil {
			result.SetErr(err)
			return result
		}
		for _, member := range page {
			if !seen[member] {
				seen[member] = true
				members = append(members, member)
			}
		}
		cursor = next
		if cursor == 0 {
			result.SetVal(members)
			return result
		}
	}
}
