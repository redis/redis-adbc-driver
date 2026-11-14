// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"context"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSnapshotRuntimeExactBigIntAggregates(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,g BIGINT,v BIGINT)")
	h.exec("INSERT INTO t VALUES(1,1,10),(2,1,-20),(3,2,30),(4,2,NULL),(5,NULL,NULL)")
	h.exec("UPDATE t SET v=25 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=3")
	h.exec("INSERT INTO t VALUES(6,2,5)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	query := "SELECT g,CAST(COUNT(*) AS BIGINT),CAST(SUM(v) AS BIGINT),MIN(v),MAX(v),AVG(v) FROM t GROUP BY g ORDER BY g NULLS LAST"
	result := snapshotExecuteTest(t, s, h.ctx, query)
	for _, row := range result.rows {
		if typ := row[5].T; typ.Kind != KindDecimal || typ.Precision != 76 || typ.Scale != 16 {
			t.Fatal("BIGINT AVG lost exact NUMERIC type", typ)
		}
	}
	got := snapshotResultStrings(result.rows)
	if want := []string{"1|2|5|-20|25|2.5000000000000000", "2|2|5|5|5|5.0000000000000000", "NULL|1|NULL|NULL|NULL|NULL"}; !slices.Equal(got, want) {
		t.Fatal("runtime-proven groups differ", got, want)
	}
	if guard.rows.Load() != 0 {
		t.Fatal("small exact BIGINT reducers fetched HASH images", guard.rows.Load())
	}
	if _, _, ok := h.planOf(query); !ok {
		t.Fatal("output casts suppressed eligible index reducers")
	}
	h.expectRows("SELECT CAST(COUNT(*) AS BIGINT),CAST(SUM(v) AS BIGINT) FROM t WHERE id<0", "0|NULL")
	h.expectRows("SELECT g,SUM(v) FROM t WHERE id<0 GROUP BY g")
}

func TestSnapshotRuntimeExactBigIntFallbacks(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(g BIGINT,v BIGINT)")
	h.exec("INSERT INTO t VALUES(9007199254740992,9007199254740992),(9007199254740993,-9007199254740993),(1,5),(1,NULL)")
	for _, query := range []string{
		"SELECT g,COUNT(*) FROM t GROUP BY g",
		"SELECT SUM(v) FROM t",
		"SELECT MIN(v),MAX(v) FROM t",
	} {
		if _, _, ok := h.planOf(query); ok {
			t.Fatal("rounded BIGINT escaped runtime fallback", query)
		}
	}
	h.expectRows("SELECT g,COUNT(*) FROM t GROUP BY g ORDER BY g", "1|2", "9007199254740992|1", "9007199254740993|1")
	h.expectRows("SELECT CAST(SUM(v) AS BIGINT),MIN(v),MAX(v) FROM t", "4|-9007199254740993|9007199254740992")
	// A zero final sum does not prove intermediate double accumulation exact.
	h.exec("TRUNCATE t")
	h.exec("INSERT INTO t VALUES(1,4503599627370496),(1,-4503599627370496)")
	if _, _, ok := h.planOf("SELECT SUM(v) FROM t"); ok {
		t.Fatal("cancellation bypassed conservative magnitude/count proof")
	}
	h.expectRows("SELECT CAST(SUM(v) AS BIGINT) FROM t", "0")
	h.exec("TRUNCATE t")
	h.exec("INSERT INTO t VALUES(1,9223372036854775807),(1,1)")
	h.expectRows("SELECT SUM(v) FROM t", "9223372036854775808")
	h.expectError("SELECT CAST(SUM(v) AS BIGINT) FROM t", "out of range")
}

// A typed deterministic Search group cap makes this optional new BIGINT
// optimization unavailable; unrelated errors must still escape unchanged.
type runtimeBigIntGroupCap struct {
	snapshotHook
	refused atomic.Bool
}

func (h *runtimeBigIntGroupCap) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		args := cmd.Args()
		if len(args) > 1 && strings.EqualFold(fmt.Sprint(args[0]), "FT.AGGREGATE") && h.refused.CompareAndSwap(false, true) {
			err := fmt.Errorf("SEARCH_LIMIT_OVER Aggregate GROUPBY exceeded MAX_AGGREGATE_GROUPS limit of 1 groups")
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}
func TestSnapshotRuntimeExactBigIntGroupCapFallback(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(g BIGINT,v BIGINT)")
	h.exec("INSERT INTO t VALUES(1,10),(2,20),(3,NULL)")
	s := snapshotTestStore(t, raw)
	guard := &runtimeBigIntGroupCap{}
	s.client.AddHook(guard)
	got := snapshotResultStrings(snapshotExecuteTest(t, s, h.ctx, "SELECT g,CAST(COUNT(*) AS BIGINT),CAST(SUM(v) AS BIGINT) FROM t GROUP BY g ORDER BY g").rows)
	if !guard.refused.Load() || !slices.Equal(got, []string{"1|1|10", "2|1|20", "3|1|NULL"}) {
		t.Fatal("optional reducer group cap changed supported SQL", got)
	}
}
