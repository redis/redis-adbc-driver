// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-adbc/go/adbc"
	goredis "github.com/redis/go-redis/v9"
)

type statisticsPresenceGuard struct {
	snapshotHook
	probes atomic.Int64
	deny   atomic.Bool
}

func (g *statisticsPresenceGuard) check(cmd goredis.Cmder) error {
	args := cmd.Args()
	if len(args) < 2 || snapshotRowPrefix(fmt.Sprint(args[1])) == "" {
		return nil
	}
	switch cmd.Name() {
	case "hexists":
		g.probes.Add(1)
		if g.deny.Load() {
			return errors.New("NOPERM injected field presence denial")
		}
	case "hgetall":
		return errors.New("statistics fetched complete payload")
	case "hmget", "hget":
		for _, field := range args[2:] {
			name := fmt.Sprint(field)
			if name != rowIDField && name != snapshotDeletedField {
				return errors.New("statistics fetched column payload")
			}
		}
	}
	return nil
}
func (g *statisticsPresenceGuard) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if err := g.check(cmd); err != nil {
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func (g *statisticsPresenceGuard) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		for _, cmd := range cmds {
			if err := g.check(cmd); err != nil {
				for _, c := range cmds {
					c.SetErr(err)
				}
				return err
			}
		}
		return next(ctx, cmds)
	}
}

func TestSnapshotStatisticsPresenceOnlyAndDenial(t *testing.T) {
	h := newSQLHarness(t)
	const table = "it_stat_presence"
	h.dropTables(table)
	h.exec("CREATE TABLE " + table + "(id BIGINT,v TEXT)")
	h.exec("INSERT INTO " + table + " VALUES(1,REPEAT('x',1048576)),(2,NULL)")
	h.exec("ALTER TABLE " + table + " ADD COLUMN later INTEGER DEFAULT 7")
	h.exec("INSERT INTO " + table + " VALUES(3,NULL,NULL)")
	guard := &statisticsPresenceGuard{}
	ctx, impl := statisticsHookedTransport(t, h, guard)
	filter := table
	result, err := impl.GetStatistics(ctx, nil, nil, &filter, false)
	if err != nil {
		t.Fatal(err)
	}
	rows, nulls := readStatisticValues(t, result)
	if guard.probes.Load() == 0 || !reflect.DeepEqual(rows, map[string]int64{table: 3}) || !reflect.DeepEqual(nulls, map[string]map[string]int64{table: {"id": 0, "v": 2, "later": 1}}) {
		t.Fatal("incorrect payload-independent statistics", rows, nulls, guard.probes.Load())
	}
	guard.deny.Store(true)
	result, err = impl.GetStatistics(ctx, nil, nil, &filter, false)
	if result != nil {
		result.Release()
		t.Fatal("denied field probes returned partial statistics")
	}
	if !isStatus(err, adbc.StatusIO) {
		t.Fatal("field denial was not surfaced", err)
	}
}
