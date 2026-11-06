// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func snapshotResultStrings(rows [][]Value) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		values := make([]string, len(row))
		for j, value := range row {
			if value.Null {
				values[j] = "NULL"
			} else {
				values[j] = value.Text()
			}
		}
		out[i] = strings.Join(values, "|")
	}
	return out
}
func TestSnapshotIndexAggregatesMergeVisibleGroups(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,g VARCHAR,v INTEGER,p BOOLEAN)")
	h.exec("INSERT INTO t VALUES (1,'a',10,true),(2,'a',NULL,NULL),(3,'b',20,true),(4,'b',30,false),(5,NULL,NULL,NULL),(6,'c',40,true)")
	h.exec("UPDATE t SET g='b',v=100 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=3")
	h.exec("INSERT INTO t VALUES (7,'a',15,false),(8,'c',NULL,NULL)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	res := snapshotExecuteTest(t, s, h.ctx, "SELECT g,COUNT(*),COUNT(v),SUM(v),AVG(v),MIN(v),MAX(v),BOOL_OR(p),BOOL_AND(p) FROM t GROUP BY g ORDER BY g NULLS LAST")
	want := []string{"a|2|1|15|15|15|15|false|false", "b|2|2|130|65|30|100|true|false", "c|2|1|40|40|40|40|true|true", "NULL|1|0|NULL|NULL|NULL|NULL|NULL|NULL"}
	if got := snapshotResultStrings(res.rows); !slices.Equal(got, want) {
		t.Fatalf("visible groups: %v", got)
	}
	if guard.rows.Load() != 0 {
		t.Fatalf("index aggregates fetched %d HASH row images", guard.rows.Load())
	}
	h.expectRows("SELECT COUNT(*),COUNT(v),SUM(v),AVG(v),MIN(v),MAX(v) FROM t", "7|4|185|46.25|15|100")
	h.expectRows("SELECT g,COUNT(*),SUM(v) FROM t WHERE v<=30 GROUP BY g ORDER BY g", "a|1|15", "b|1|30")
	h.expectRows("SELECT COUNT(*),COUNT(v),SUM(v),AVG(v),MIN(v),MAX(v) FROM t WHERE v>999", "0|0|NULL|NULL|NULL|NULL")
}

func TestSnapshotIndexMaskBudgetFallsBackWithoutResurrection(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,1025) AS g(i)")
	h.exec("UPDATE t SET v=2 WHERE id%2=0")
	h.expectRows("SELECT v,COUNT(*),SUM(id) FROM t GROUP BY v ORDER BY v", "1|513|263169", "2|512|262656")
	h.exec("DELETE FROM t WHERE v=2")
	h.expectRows("SELECT COUNT(*),SUM(id) FROM t", "513|263169")
}

func TestSnapshotContiguousMasksAvoidIDEnumeration(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,1 FROM generate_series(1,6001) AS g(i)")
	h.exec("DELETE FROM t WHERE id<=5000")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	ctx, pin, err := s.acquireSnapshot(h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.release()
	meta, err := s.getTable(ctx, defaultSchema, "t")
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{store: s}
	queries, ok, err := e.snapshotIndexQueries(ctx, meta)
	if err != nil || !ok || len(queries) != 2 {
		t.Fatalf("large contiguous tombstones lost bounded index masks: %d %v %v", len(queries), ok, err)
	}
	res := snapshotExecuteTest(t, s, ctx, "SELECT COUNT(*),SUM(id) FROM t")
	if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"1001|5506501"}) {
		t.Fatal("contiguous tombstones resurrected old rows", got)
	}
	if guard.rows.Load() != 0 || guard.masks.Load() != 0 {
		t.Fatalf("contiguous proof enumerated IDs or fetched HASHes: %d %d", guard.masks.Load(), guard.rows.Load())
	}
	// Adjacent/overlapping masks from later replacements and tombstones must
	// merge exactly, while the old pin keeps the first deletion's generation.
	h.exec("UPDATE t SET v=2 WHERE id>=5001 AND id<=5100")
	h.exec("DELETE FROM t WHERE id>=5901")
	h.exec("DELETE FROM t WHERE id=5501")
	counts, sums := [3]int64{}, [3]int64{}
	for id := int64(5001); id < 5901; id++ {
		if id == 5501 {
			continue
		}
		v := 1
		if id <= 5100 {
			v = 2
		}
		counts[v]++
		sums[v] += id
	}
	h.expectRows("SELECT v,COUNT(*),SUM(id) FROM t GROUP BY v ORDER BY v",
		fmt.Sprintf("1|%d|%d", counts[1], sums[1]), fmt.Sprintf("2|%d|%d", counts[2], sums[2]))
	res = snapshotExecuteTest(t, s, ctx, "SELECT COUNT(*),SUM(id) FROM t")
	if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"1001|5506501"}) {
		t.Fatal("later masks changed pinned aggregates", got)
	}
	if guard.rows.Load() != 0 || guard.masks.Load() != 0 {
		t.Fatal("pinned contiguous proof lost its bounded index path")
	}
}

func TestSnapshotIndexDistinctUnionsVisibleTuples(t *testing.T) {
	h, raw, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,g VARCHAR,v INTEGER)")
	h.exec("INSERT INTO t VALUES(1,'a',10),(2,'a',20),(3,'b',10),(4,'b',NULL),(5,'c',30)")
	h.exec("UPDATE t SET g='b',v=20 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=3")
	h.exec("INSERT INTO t VALUES(6,'a',20),(7,'c',30),(8,'c',40)")
	s := snapshotTestStore(t, raw)
	guard := &snapshotCandidateReads{}
	s.client.AddHook(guard)
	res := snapshotExecuteTest(t, s, h.ctx, "SELECT g,COUNT(DISTINCT v) FROM t GROUP BY g ORDER BY g")
	if got := snapshotResultStrings(res.rows); !slices.Equal(got, []string{"a|1", "b|1", "c|2"}) {
		t.Fatal("cross-segment DISTINCT duplicated values", got)
	}
	if guard.rows.Load() != 0 {
		t.Fatal("server DISTINCT transferred row images", guard.rows.Load())
	}
	h.expectRows("SELECT COUNT(DISTINCT g),COUNT(DISTINCT v) FROM t", "3|3")
	h.expectRows("SELECT g,COUNT(DISTINCT v) FILTER(WHERE v>30) FROM t GROUP BY g ORDER BY g", "a|0", "b|0", "c|1")
	h.expectRows("SELECT g,COUNT(*),SUM(v) FROM t GROUP BY GROUPING SETS ((g),()) ORDER BY g NULLS LAST", "a|2|40", "b|2|20", "c|3|100", "NULL|7|160")
}

func TestSnapshotPartitionedTupleAggregatesResolveVisibility(t *testing.T) {
	h, _, _ := newSnapshotHarnessMode(t, false, false)
	h.exec("CREATE TABLE t(id INTEGER,g VARCHAR,v INTEGER)")
	h.exec("INSERT INTO t SELECT i,'g'||(i%32),i%10 FROM generate_series(1,70001) AS g(i)")
	h.exec("UPDATE t SET g='g0',v=99 WHERE id=1")
	h.exec("DELETE FROM t WHERE id=2")
	type group struct {
		count, sum int64
		distinct   map[int]bool
	}
	expected := map[string]*group{}
	for id := 1; id <= 70001; id++ {
		if id == 2 {
			continue
		}
		key, v := "g"+strconv.Itoa(id%32), id%10
		if id == 1 {
			key, v = "g0", 99
		}
		g := expected[key]
		if g == nil {
			g = &group{distinct: map[int]bool{}}
			expected[key] = g
		}
		g.count++
		g.sum += int64(v)
		g.distinct[v] = true
	}
	var keys []string
	for key := range expected {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var want []string
	for _, key := range keys {
		g := expected[key]
		want = append(want, fmt.Sprintf("%s|%d|%d|%d", key, g.count, len(g.distinct), g.sum))
	}
	got, _ := h.query("SELECT g,COUNT(*),COUNT(DISTINCT v),SUM(v) FROM t GROUP BY g ORDER BY g")
	if !slices.Equal(got, want) {
		t.Fatalf("parallel tuple visibility: got %v want %v", got, want)
	}
}
